//go:build windows

package winsel

import (
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	cfText        = 1
	cfBitmap      = 2
	cfMetafile    = 3
	cfPalette     = 9
	cfUnicodeText = 13
	cfEnhMetafile = 14
	cfDspFirst    = 0x0080
	cfDspLast     = 0x008E
	cfGdiFirst    = 0x0300
	cfGdiLast     = 0x03FF

	gmemMoveable = 0x0002
	gmemZeroInit = 0x0040

	// maxSnapshot is the most clipboard data kept to put back afterwards.
	maxSnapshot = 64 << 20
)

// clipMu serialises everything that borrows the clipboard (reading a
// selection by copying, pasting a translation, putting the old content back).
var clipMu sync.Mutex

// ClipboardSeq is the clipboard change counter.
func ClipboardSeq() uint32 {
	r, _, _ := pGetClipboardSeq.Call()
	return uint32(r)
}

// txns counts finished clipboard transactions that Tolk made on its own
// account, so that a watcher polling slower than a transaction lasts can still
// tell that the changes it sees are not the user's.
var txns atomic.Uint64

// Transactions is how many clipboard transactions Tolk has finished.
func Transactions() uint64 { return txns.Load() }

var (
	ownerOnce sync.Once
	ownerWnd  uintptr
)

// clipboardOwner is a window of ours to own what is put on the clipboard:
// SetClipboardData fails after EmptyClipboard when the clipboard has no owner.
// It lives on a thread of its own that keeps pumping its messages.
func clipboardOwner() uintptr {
	ownerOnce.Do(func() {
		ready := make(chan uintptr, 1)
		go func() {
			runtime.LockOSThread()
			const hwndMessage = ^uintptr(2) // (HWND)-3: a window that is only there for messages
			h, _, _ := pCreateWindowEx.Call(0, ptr(u16("STATIC")), ptr(u16("Tolk clipboard")), 0, 0, 0, 0, 0, hwndMessage, 0, 0, 0)
			ready <- h
			if h == 0 {
				return
			}
			var msg [8]uintptr
			for {
				if r, _, _ := pGetMessage.Call(ptr(&msg[0]), 0, 0, 0); int32(r) <= 0 {
					return
				}
				pDispatchMessage.Call(ptr(&msg[0]))
			}
		}()
		ownerWnd = <-ready
	})
	return ownerWnd
}

// openClipboard opens the clipboard and pins the goroutine to its OS thread
// until closeClipboard: the clipboard is open for a thread, and the Go
// scheduler would otherwise move the goroutine to another one in between.
func openClipboard() error {
	runtime.LockOSThread()
	owner := clipboardOwner()
	for i := 0; i < 20; i++ {
		if r, _, _ := pOpenClipboard.Call(owner); r != 0 {
			return nil
		}
		time.Sleep(15 * time.Millisecond) // another app (or a clipboard manager) has it
	}
	runtime.UnlockOSThread()
	return errors.New("剪贴板正被其它应用占用")
}

func closeClipboard() {
	pCloseClipboard.Call()
	runtime.UnlockOSThread()
}

// ClipboardText reads the text on the clipboard.
func clipboardText() (string, bool) {
	if r, _, _ := pIsClipboardFormatAvail.Call(cfUnicodeText); r == 0 {
		return "", false
	}
	if openClipboard() != nil {
		return "", false
	}
	defer closeClipboard()
	h, _, _ := pGetClipboardData.Call(cfUnicodeText)
	if h == 0 {
		return "", false
	}
	p, _, _ := pGlobalLock.Call(h)
	if p == 0 {
		return "", false
	}
	defer pGlobalUnlock.Call(h)
	size, _, _ := pGlobalSize.Call(h)
	n := int(size) / 2
	if n == 0 {
		return "", true
	}
	buf := unsafe.Slice((*uint16)(at(p)), n)
	end := 0
	for end < n && buf[end] != 0 {
		end++
	}
	return windows.UTF16ToString(buf[:end]), true
}

// snapshotItem is one clipboard format with its data.
type snapshotItem struct {
	format uint32
	data   []byte
}

// snapshot holds the clipboard content so it can be put back. Memory formats
// are copied; an enhanced metafile (a copied vector drawing) is a GDI handle
// and is copied with CopyEnhMetaFile. The other GDI formats (bitmaps, palettes)
// are not: Windows builds those again from the DIB that is among the memory
// formats. A clipboard that holds something this cannot keep is not borrowed.
type snapshot struct {
	items []snapshotItem
	emf   uintptr // owned until it is handed to the clipboard
}

var errSuperseded = errors.New("剪贴板已被其它程序更新")

func takeSnapshot() (*snapshot, error) {
	if err := openClipboard(); err != nil {
		return nil, err
	}
	defer closeClipboard()
	s := &snapshot{}
	total := 0
	lossy := false
	var f uintptr
	for {
		f, _, _ = pEnumClipboardFormats.Call(f)
		if f == 0 {
			break
		}
		fmtID := uint32(f)
		switch fmtID {
		case cfEnhMetafile:
			h, _, _ := pGetClipboardData.Call(f)
			if h != 0 {
				s.emf, _, _ = pCopyEnhMetaFile.Call(h, 0)
				if s.emf == 0 {
					lossy = true
				}
			}
			continue
		case cfMetafile:
			lossy = true // an old style metafile; fine only if an enhanced one came with it
			continue
		}
		if skipFormat(fmtID) {
			continue
		}
		h, _, _ := pGetClipboardData.Call(f)
		if h == 0 {
			continue
		}
		size, _, _ := pGlobalSize.Call(h)
		if size == 0 {
			continue
		}
		total += int(size)
		if total > maxSnapshot {
			s.free()
			return nil, errors.New("剪贴板里的内容太大，无法在读取选区后原样还原")
		}
		p, _, _ := pGlobalLock.Call(h)
		if p == 0 {
			continue
		}
		data := make([]byte, size)
		copy(data, unsafe.Slice((*byte)(at(p)), int(size)))
		pGlobalUnlock.Call(h)
		s.items = append(s.items, snapshotItem{fmtID, data})
	}
	if lossy && s.emf == 0 {
		s.free()
		return nil, errors.New("剪贴板里的矢量图形无法在读取选区后原样还原")
	}
	return s, nil
}

// free drops what the snapshot still owns, when it is not put back.
func (s *snapshot) free() {
	if s != nil && s.emf != 0 {
		pDeleteEnhMetaFile.Call(s.emf)
		s.emf = 0
	}
}

func skipFormat(f uint32) bool {
	switch {
	case f == cfBitmap, f == cfMetafile, f == cfPalette, f == cfEnhMetafile:
		return true
	case f >= cfDspFirst && f <= cfDspLast, f >= cfGdiFirst && f <= cfGdiLast:
		return true
	}
	return false
}

func globalCopy(data []byte) uintptr {
	h, _, _ := pGlobalAlloc.Call(gmemMoveable|gmemZeroInit, uintptr(len(data))+2)
	if h == 0 {
		return 0
	}
	p, _, _ := pGlobalLock.Call(h)
	if p == 0 {
		pGlobalFree.Call(h)
		return 0
	}
	if len(data) > 0 {
		copy(unsafe.Slice((*byte)(at(p)), len(data)), data)
	}
	pGlobalUnlock.Call(h)
	return h
}

// restore puts the snapshot back, provided the clipboard still has the change
// number expect (what Tolk itself left there): the check is made with the
// clipboard open, so nobody can slip a newer content in between it and the
// emptying.
func (s *snapshot) restore(expect uint32) error {
	defer txns.Add(1)
	defer s.free()
	if err := openClipboard(); err != nil {
		return err
	}
	defer closeClipboard()
	if ClipboardSeq() != expect {
		return errSuperseded
	}
	pEmptyClipboard.Call()
	failed := 0
	for _, it := range s.items {
		h := globalCopy(it.data)
		if h == 0 {
			failed++
			continue
		}
		if r, _, _ := pSetClipboardData.Call(uintptr(it.format), h); r == 0 {
			pGlobalFree.Call(h) // the clipboard did not take it
			failed++
		}
	}
	if s.emf != 0 {
		if r, _, _ := pSetClipboardData.Call(cfEnhMetafile, s.emf); r != 0 {
			s.emf = 0 // the clipboard owns it now
		} else {
			failed++
		}
	}
	if failed > 0 {
		return errors.New("剪贴板里的部分内容没能还原")
	}
	return nil
}

// registered clipboard formats that keep a temporary entry out of the
// clipboard history, the cloud clipboard and clipboard monitors.
var privateFormats = func() []uint32 {
	var out []uint32
	for _, n := range []string{"ExcludeClipboardContentFromMonitorProcessing", "CanIncludeInClipboardHistory", "CanUploadToCloudClipboard"} {
		if id, _, _ := pRegisterClipboardFormat.Call(ptr(u16(n))); id != 0 {
			out = append(out, uint32(id))
		}
	}
	return out
}()

// setClipboardText replaces the clipboard with text. private keeps it out of
// the clipboard history and cloud sync, for the temporary content of a paste.
func setClipboardText(text string, private bool) error {
	u, err := windows.UTF16FromString(text)
	if err != nil {
		return err
	}
	b := unsafe.Slice((*byte)(unsafe.Pointer(&u[0])), len(u)*2)
	if err := openClipboard(); err != nil {
		return err
	}
	defer closeClipboard()
	pEmptyClipboard.Call()
	h := globalCopy(b)
	if h == 0 {
		return errors.New("内存不足")
	}
	if r, _, _ := pSetClipboardData.Call(cfUnicodeText, h); r == 0 {
		pGlobalFree.Call(h)
		return errors.New("无法写入剪贴板")
	}
	if private {
		for i, id := range privateFormats {
			// ExcludeClipboardContentFromMonitorProcessing has no value; the
			// two Can… formats are DWORD 0.
			data := []byte{0, 0, 0, 0}
			if i == 0 {
				data = []byte{1, 0, 0, 0}
			}
			if hh := globalCopy(data); hh != 0 {
				if r, _, _ := pSetClipboardData.Call(uintptr(id), hh); r == 0 {
					pGlobalFree.Call(hh)
				}
			}
		}
	}
	return nil
}

// SetClipboardText puts text on the clipboard (the "译文已复制" fallback).
func SetClipboardText(text string) error {
	clipMu.Lock()
	defer clipMu.Unlock()
	defer txns.Add(1)
	return setClipboardText(text, false)
}

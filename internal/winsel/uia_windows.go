//go:build windows

package winsel

import (
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"tolk/internal/inputassist"
)

// UI Automation, called through its COM vtables. Only what is needed to read
// the selection of the focused control: the text, where it is on screen, and
// whether the control is a password box.

var (
	clsidCUIAutomation = windows.GUID{Data1: 0xff48dba4, Data2: 0x60ef, Data3: 0x4201, Data4: [8]byte{0xaa, 0x87, 0x54, 0x10, 0x3e, 0xef, 0x59, 0x4e}}
	iidIUIAutomation   = windows.GUID{Data1: 0x30cbe57d, Data2: 0xd9d0, Data3: 0x452a, Data4: [8]byte{0xab, 0x13, 0x7a, 0xc5, 0xac, 0x48, 0x25, 0xee}}
	iidTextPattern     = windows.GUID{Data1: 0x32eba289, Data2: 0x3583, Data3: 0x42c9, Data4: [8]byte{0x9c, 0x59, 0x3b, 0x6d, 0x9a, 0x1e, 0x9b, 0x6a}}
)

const (
	uiaTextPatternID  = 10014
	uiaIsPasswordProp = 30019
	vtBool            = 11

	// Vtable slots (IUnknown takes 0 to 2).
	vtQueryInterface = 0
	vtRelease        = 2
	// IUIAutomation
	vtGetFocusedElement = 8
	// IUIAutomationElement
	vtGetCurrentPropertyValue = 10
	vtGetCurrentPattern       = 16
	vtCurrentProcessID        = 20
	// IUIAutomationTextPattern
	vtGetSelection = 5
	// IUIAutomationTextRangeArray
	vtArrayLength     = 3
	vtArrayGetElement = 4
	// IUIAutomationTextRange
	vtRangeGetBoundingRects = 10
	vtRangeGetText          = 12
)

// com calls method slot of a COM object.
func com(obj uintptr, slot int, args ...uintptr) uintptr {
	vtbl := *(*uintptr)(at(obj))
	fn := *(*uintptr)(at(vtbl + uintptr(slot)*unsafe.Sizeof(uintptr(0))))
	r, _, _ := syscall.SyscallN(fn, append([]uintptr{obj}, args...)...)
	return r
}

func release(obj uintptr) {
	if obj != 0 {
		com(obj, vtRelease)
	}
}

// ErrNoSelection means the control has no selected text.
var ErrNoSelection = errors.New("没有选中的文字")

// ErrPassword means the control is a password box; nothing is read from it.
var ErrPassword = errors.New("密码框")

// uiaResult is what UI Automation found in the focused control.
type uiaResult struct {
	supported bool // the control has a text pattern at all
	text      string
	rect      inputassist.Rect
	haveRect  bool
	// truncated: the text is longer than GetText was asked for, so only the
	// start of it was read.
	truncated bool
	why       string // when not supported or empty: where it stopped (for tests and the log)
}

// readUIA reads the selection of the focused control, which must belong to
// process pid. It runs on a thread of its own with a time limit: a hung app
// can make UI Automation wait for seconds, and the hook must not.
func readUIA(pid int, limit time.Duration) (uiaResult, error) {
	type out struct {
		r   uiaResult
		err error
	}
	ch := make(chan out, 1)
	go func() {
		runtime.LockOSThread() // COM state belongs to the thread
		r, err := readUIAOnThread(pid)
		ch <- out{r, err}
		runtime.UnlockOSThread()
	}()
	select {
	case o := <-ch:
		return o.r, o.err
	case <-time.After(limit):
		return uiaResult{}, errors.New("应用没有及时回应")
	}
}

func readUIAOnThread(pid int) (res uiaResult, err error) {
	const coinitMultithreaded = 0x0
	if hr, _, _ := pCoInitializeEx.Call(0, coinitMultithreaded); failed(hr) {
		return res, fmt.Errorf("COM 初始化失败（0x%x）", uint32(hr))
	}
	defer pCoUninitialize.Call()

	var auto uintptr
	const clsctxInprocServer = 0x1
	if hr, _, _ := pCoCreateInstance.Call(ptr(&clsidCUIAutomation), 0, clsctxInprocServer, ptr(&iidIUIAutomation), ptr(&auto)); failed(hr) || auto == 0 {
		return res, fmt.Errorf("无法使用 UI Automation（0x%x）", uint32(hr))
	}
	defer release(auto)

	var elem uintptr
	if hr := com(auto, vtGetFocusedElement, ptr(&elem)); failed(hr) || elem == 0 {
		res.why = "GetFocusedElement 没有返回元素"
		return res, nil // nothing focused: not supported, the caller falls back
	}
	defer release(elem)

	// The control must belong to the app that was approved, not to whatever
	// took the focus meanwhile.
	var owner int32
	if hr := com(elem, vtCurrentProcessID, ptr(&owner)); failed(hr) || int(owner) != pid {
		res.why = fmt.Sprintf("焦点元素属于进程 %d（hr=0x%x），要的是 %d", owner, uint32(hr), pid)
		return res, nil
	}

	var v [4]uint64 // VARIANT (24 bytes on 64-bit), with room to spare
	if hr := com(elem, vtGetCurrentPropertyValue, uiaIsPasswordProp, ptr(&v[0])); !failed(hr) {
		isBool := uint16(v[0]) == vtBool
		on := int16(v[1]) != 0
		pVariantClear.Call(ptr(&v[0]))
		if isBool && on {
			return res, ErrPassword
		}
	}

	var pattern uintptr
	if hr := com(elem, vtGetCurrentPattern, uiaTextPatternID, ptr(&pattern)); failed(hr) || pattern == 0 {
		res.why = fmt.Sprintf("GetCurrentPattern(Text) hr=0x%x pattern=%x", uint32(hr), pattern)
		return res, nil
	}
	defer release(pattern)
	// What comes back is an IUnknown: ask it for the text pattern interface.
	var textPattern uintptr
	if hr := com(pattern, vtQueryInterface, ptr(&iidTextPattern), ptr(&textPattern)); failed(hr) || textPattern == 0 {
		res.why = fmt.Sprintf("QueryInterface(TextPattern) hr=0x%x", uint32(hr))
		return res, nil
	}
	defer release(textPattern)
	pattern = textPattern
	res.supported = true

	var ranges uintptr
	if hr := com(pattern, vtGetSelection, ptr(&ranges)); failed(hr) || ranges == 0 {
		return res, nil
	}
	defer release(ranges)
	var n int32
	if hr := com(ranges, vtArrayLength, ptr(&n)); failed(hr) || n <= 0 {
		return res, nil
	}

	var text []uint16
	var union rect
	have := false
	for i := int32(0); i < n && i < 32; i++ {
		var rg uintptr
		if hr := com(ranges, vtArrayGetElement, uintptr(i), ptr(&rg)); failed(hr) || rg == 0 {
			continue
		}
		var bstr uintptr
		if hr := com(rg, vtRangeGetText, MaxChars+1, ptr(&bstr)); !failed(hr) && bstr != 0 {
			part := bstrToUTF16(bstr)
			text = append(text, part...)
			if len(part) > MaxChars {
				res.truncated = true // GetText stops at MaxChars+1 units
			}
			pSysFreeString.Call(bstr)
		}
		if r, ok := rangeRect(rg); ok {
			if !have {
				union, have = r, true
			} else {
				union.Left, union.Top = min(union.Left, r.Left), min(union.Top, r.Top)
				union.Right, union.Bottom = max(union.Right, r.Right), max(union.Bottom, r.Bottom)
			}
		}
		release(rg)
	}
	if len(text) > MaxChars {
		res.truncated = true
	}
	res.text = windows.UTF16ToString(text)
	if have {
		res.rect = inputassist.Rect{X: int(union.Left), Y: int(union.Top), W: int(union.Right - union.Left), H: int(union.Bottom - union.Top)}
		res.haveRect = true
	}
	return res, nil
}

// bstrToUTF16 copies a BSTR (without its terminator).
func bstrToUTF16(b uintptr) []uint16 {
	n := int(*(*uint32)(at(b - 4))) / 2 // the byte length precedes the characters
	if n <= 0 {
		return nil
	}
	return append([]uint16(nil), unsafe.Slice((*uint16)(at(b)), n)...)
}

// rangeRect is the union of the bounding rectangles of a text range, in
// screen pixels.
func rangeRect(rg uintptr) (rect, bool) {
	var sa uintptr
	if hr := com(rg, vtRangeGetBoundingRects, ptr(&sa)); failed(hr) || sa == 0 {
		return rect{}, false
	}
	defer pSafeArrayDestroy.Call(sa)
	var lo, hi int32
	if hr, _, _ := pSafeArrayGetLBound.Call(sa, 1, ptr(&lo)); failed(hr) {
		return rect{}, false
	}
	if hr, _, _ := pSafeArrayGetUBound.Call(sa, 1, ptr(&hi)); failed(hr) {
		return rect{}, false
	}
	count := int(hi-lo) + 1 // doubles: left, top, width, height for each rectangle
	if count < 4 {
		return rect{}, false
	}
	var data uintptr
	if hr, _, _ := pSafeArrayAccessData.Call(sa, ptr(&data)); failed(hr) || data == 0 {
		return rect{}, false
	}
	defer pSafeArrayUnaccessDat.Call(sa)
	vals := unsafe.Slice((*float64)(at(data)), count)
	var u rect
	have := false
	for i := 0; i+3 < count; i += 4 {
		l, t, w, h := vals[i], vals[i+1], vals[i+2], vals[i+3]
		if w <= 0 && h <= 0 {
			continue
		}
		r := rect{int32(l), int32(t), int32(l + w), int32(t + h)}
		if !have {
			u, have = r, true
		} else {
			u.Left, u.Top = min(u.Left, r.Left), min(u.Top, r.Top)
			u.Right, u.Bottom = max(u.Right, r.Right), max(u.Bottom, r.Bottom)
		}
	}
	return u, have
}

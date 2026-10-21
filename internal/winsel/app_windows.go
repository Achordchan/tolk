//go:build windows

package winsel

import (
	"fmt"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"

	"tolk/internal/inputassist"
)

// Window is a window handle.
type Window uintptr

// Alive reports whether the window still exists.
func (w Window) Alive() bool {
	r, _, _ := pIsWindow.Call(uintptr(w))
	return r != 0
}

// Foreground is the window the user is typing in, and who owns it.
type Foreground struct {
	Window Window
	App    inputassist.App
}

// GetForeground describes the foreground window. ok is false when there is
// none (the lock screen, a window being switched) or its process cannot be
// identified.
func GetForeground() (f Foreground, ok bool) {
	h, _, _ := pGetForegroundWindow.Call()
	if h == 0 {
		return f, false
	}
	var pid uint32
	pGetWindowThreadPID.Call(h, ptr(&pid))
	if pid == 0 {
		return f, false
	}
	f.Window = Window(h)
	f.App = inputassist.App{PID: int(pid)}
	path := processImage(pid)
	if path == "" {
		return f, false
	}
	f.App.Exe = filepath.Base(path)
	f.App.Name = productName(path)
	return f, true
}

func processImage(pid uint32) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, 1024)
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
		return ""
	}
	return windows.UTF16ToString(buf[:n])
}

// productName reads the ProductName (or FileDescription) of an executable,
// which is what people call an app ("微信", "Notepad"); "" when it has none.
func productName(path string) string {
	p := u16(path)
	size, _, _ := pGetFileVersionInfoSize.Call(ptr(p), 0)
	if size == 0 {
		return ""
	}
	data := make([]byte, size)
	if r, _, _ := pGetFileVersionInfo.Call(ptr(p), 0, size, uintptr(unsafe.Pointer(&data[0]))); r == 0 {
		return ""
	}
	query := func(sub string) string {
		var v uintptr
		var n uint32
		r, _, _ := pVerQueryValue.Call(uintptr(unsafe.Pointer(&data[0])), ptr(u16(sub)), ptr(&v), ptr(&n))
		if r == 0 || n == 0 || v == 0 {
			return ""
		}
		return windows.UTF16PtrToString((*uint16)(at(v)))
	}
	var langs []string
	{
		var v uintptr
		var n uint32
		r, _, _ := pVerQueryValue.Call(uintptr(unsafe.Pointer(&data[0])), ptr(u16(`\VarFileInfo\Translation`)), ptr(&v), ptr(&n))
		if r != 0 && n >= 4 {
			lang := *(*uint16)(at(v))
			cp := *(*uint16)(at(v + 2))
			langs = append(langs, fmt.Sprintf("%04x%04x", lang, cp))
		}
	}
	langs = append(langs, "080404b0", "040904b0", "040904e4")
	for _, l := range langs {
		for _, field := range []string{"ProductName", "FileDescription"} {
			if s := strings.TrimSpace(query(`\StringFileInfo\` + l + `\` + field)); s != "" {
				return s
			}
		}
	}
	return ""
}

// higherIntegrity reports whether the process runs with more privileges than
// Tolk, or cannot be inspected at all (which for a process we could open the
// image of means it is more privileged). Input and UI Automation calls from
// an ordinary process to an administrator's one are blocked by Windows.
func higherIntegrity(pid int) bool {
	self := integrity(windows.GetCurrentProcessId())
	other := integrity(uint32(pid))
	if other < 0 { // could not be opened: treat as more privileged
		return true
	}
	return self >= 0 && other > self
}

// integrity is the integrity level of a process (the RID of its label), or -1
// when the token cannot be read.
func integrity(pid uint32) int {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return -1
	}
	defer windows.CloseHandle(h)
	var tok windows.Token
	if err := windows.OpenProcessToken(h, windows.TOKEN_QUERY, &tok); err != nil {
		return -1
	}
	defer tok.Close()
	var n uint32
	_ = windows.GetTokenInformation(tok, windows.TokenIntegrityLevel, nil, 0, &n)
	if n == 0 {
		return -1
	}
	buf := make([]byte, n)
	if err := windows.GetTokenInformation(tok, windows.TokenIntegrityLevel, &buf[0], n, &n); err != nil {
		return -1
	}
	label := (*windows.Tokenmandatorylabel)(unsafe.Pointer(&buf[0]))
	sid := label.Label.Sid
	cnt := int(sid.SubAuthorityCount())
	if cnt == 0 {
		return -1
	}
	return int(sid.SubAuthority(uint32(cnt - 1)))
}

// HigherIntegrity reports whether the process pid runs with more privileges
// than Tolk.
func HigherIntegrity(pid int) bool { return higherIntegrity(pid) }

// CursorPoint is the mouse position in screen pixels.
func CursorPoint() (x, y int) {
	var p point
	pGetCursorPos.Call(ptr(&p))
	return int(p.X), int(p.Y)
}

// CursorRect is a zero-size rectangle at the mouse, the anchor of last resort.
func CursorRect() inputassist.Rect {
	x, y := CursorPoint()
	return inputassist.Rect{X: x, Y: y}
}

// WorkArea is the work area (the screen minus the taskbar) of the monitor
// nearest to the point.
func WorkArea(x, y int) inputassist.Rect {
	const nearest = 2
	mon, _, _ := pMonitorFromPoint.Call(uintptr(uint64(uint32(int32(x)))|uint64(uint32(int32(y)))<<32), nearest)
	var mi struct {
		Size    uint32
		Monitor rect
		Work    rect
		Flags   uint32
	}
	mi.Size = uint32(unsafe.Sizeof(mi))
	if r, _, _ := pGetMonitorInfo.Call(mon, ptr(&mi)); r == 0 {
		return inputassist.Rect{X: 0, Y: 0, W: 1920, H: 1040}
	}
	return inputassist.Rect{X: int(mi.Work.Left), Y: int(mi.Work.Top), W: int(mi.Work.Right - mi.Work.Left), H: int(mi.Work.Bottom - mi.Work.Top)}
}

// DPIAt is the DPI of the monitor at a point (96 = 100 %).
func DPIAt(x, y int) int {
	const nearest = 2
	mon, _, _ := pMonitorFromPoint.Call(uintptr(uint64(uint32(int32(x)))|uint64(uint32(int32(y)))<<32), nearest)
	var dx, dy uint32
	if hr, _, _ := pGetDpiForMonitor.Call(mon, 0, ptr(&dx), ptr(&dy)); failed(hr) || dx == 0 {
		return 96
	}
	return int(dx)
}

// CaretRect is the text caret of the window's focused control in screen
// pixels, for apps that draw a classic caret (and tell Windows about it).
func CaretRect(w Window) (inputassist.Rect, bool) {
	tid, _, _ := pGetWindowThreadPID.Call(uintptr(w), 0)
	info := guiThreadInfo{}
	info.Size = uint32(unsafe.Sizeof(info))
	if r, _, _ := pGetGUIThreadInfo.Call(tid, ptr(&info)); r == 0 {
		return inputassist.Rect{}, false
	}
	if info.HwndCaret == 0 || (info.Caret.Right == info.Caret.Left && info.Caret.Bottom == info.Caret.Top) {
		return inputassist.Rect{}, false
	}
	tl := point{info.Caret.Left, info.Caret.Top}
	br := point{info.Caret.Right, info.Caret.Bottom}
	pClientToScreen.Call(info.HwndCaret, ptr(&tl))
	pClientToScreen.Call(info.HwndCaret, ptr(&br))
	return inputassist.Rect{X: int(tl.X), Y: int(tl.Y), W: max(1, int(br.X-tl.X)), H: max(1, int(br.Y-tl.Y))}, true
}

type guiThreadInfo struct {
	Size        uint32
	Flags       uint32
	HwndActive  uintptr
	HwndFocus   uintptr
	HwndCapture uintptr
	HwndMenu    uintptr
	HwndMove    uintptr
	HwndCaret   uintptr
	Caret       rect
}

// FocusWindow is the window handle of the control with the keyboard focus in
// the foreground window's thread; it changes when the user clicks elsewhere.
func FocusWindow(w Window) uintptr {
	tid, _, _ := pGetWindowThreadPID.Call(uintptr(w), 0)
	info := guiThreadInfo{}
	info.Size = uint32(unsafe.Sizeof(info))
	if r, _, _ := pGetGUIThreadInfo.Call(tid, ptr(&info)); r == 0 {
		return 0
	}
	return info.HwndFocus
}

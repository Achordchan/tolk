//go:build windows

package winsel

import (
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
)

const (
	whKeyboardLL = 13
	whMouseLL    = 14
	wmQuit       = 0x0012

	llkhfInjected = 0x10
	llmhfInjected = 0x01
)

// Mods are the modifier keys held down.
type Mods struct{ Ctrl, Alt, Shift, Win bool }

func currentMods() Mods {
	return Mods{Ctrl: keyDown(vkControl), Alt: keyDown(vkMenu), Shift: keyDown(vkShift), Win: keyDown(vkLWin) || keyDown(vkRWin)}
}

// KeyHandler sees every key press (not releases, and not keys Tolk itself
// sends) while a keyboard hook is installed. Returning true keeps the key from
// the application. It runs inside the hook: Windows removes a hook that is
// slow to answer, so it must only decide, never wait.
type KeyHandler func(vk int, mods Mods) (swallow bool)

// MouseHandler sees the mouse buttons, pressed (down true) and released.
type MouseHandler func(button int, down bool, x, y int) (swallow bool)

// Mouse buttons.
const (
	ButtonLeft = iota + 1
	ButtonRight
	ButtonMiddle
	ButtonX
)

// Hook is an installed low level input hook on a thread of its own.
type Hook struct {
	tid   uint32
	done  chan struct{}
	once  sync.Once
	clear func()
}

// Remove uninstalls the hook.
func (h *Hook) Remove() {
	h.once.Do(func() {
		pPostThreadMessage.Call(uintptr(h.tid), wmQuit, 0, 0)
		<-h.done
		h.clear()
	})
}

var (
	kbdHandler   atomic.Pointer[KeyHandler]
	mouseHandler atomic.Pointer[MouseHandler]

	kbdSwallowed   [256]atomic.Bool
	mouseSwallowed [5]atomic.Bool
)

// The callbacks are made once: Go never frees them.
var (
	kbdCallback   = syscall.NewCallback(kbdProc)
	mouseCallback = syscall.NewCallback(mouseProc)
)

type kbdHookStruct struct {
	VK, Scan, Flags, Time uint32
	Extra                 uintptr
}

type mouseHookStruct struct {
	Pt    point
	Data  uint32
	Flags uint32
	Time  uint32
	Extra uintptr
}

func kbdProc(code, wparam, lparam uintptr) uintptr {
	if int32(code) == 0 {
		ks := (*kbdHookStruct)(at(lparam))
		if h := kbdHandler.Load(); h != nil && ks.Flags&llkhfInjected == 0 && ks.VK < 256 {
			down := wparam == 0x0100 || wparam == 0x0104 // WM_KEYDOWN, WM_SYSKEYDOWN
			if down {
				if (*h)(int(ks.VK), currentMods()) {
					kbdSwallowed[ks.VK].Store(true)
					return 1
				}
			} else if kbdSwallowed[ks.VK].Swap(false) {
				return 1 // the release of a key the application never saw pressed
			}
		}
	}
	r, _, _ := pCallNextHookEx.Call(0, code, wparam, lparam)
	return r
}

func mouseProc(code, wparam, lparam uintptr) uintptr {
	if int32(code) == 0 {
		button, down := 0, false
		switch wparam {
		case 0x0201:
			button, down = ButtonLeft, true
		case 0x0202:
			button = ButtonLeft
		case 0x0204:
			button, down = ButtonRight, true
		case 0x0205:
			button = ButtonRight
		case 0x0207:
			button, down = ButtonMiddle, true
		case 0x0208:
			button = ButtonMiddle
		case 0x020B:
			button, down = ButtonX, true
		case 0x020C:
			button = ButtonX
		}
		ms := (*mouseHookStruct)(at(lparam))
		if h := mouseHandler.Load(); h != nil && button != 0 && ms.Flags&llmhfInjected == 0 {
			if down {
				if (*h)(button, true, int(ms.Pt.X), int(ms.Pt.Y)) {
					mouseSwallowed[button].Store(true)
					return 1
				}
			} else if mouseSwallowed[button].Swap(false) {
				(*h)(button, false, int(ms.Pt.X), int(ms.Pt.Y))
				return 1
			} else {
				(*h)(button, false, int(ms.Pt.X), int(ms.Pt.Y))
			}
		}
	}
	r, _, _ := pCallNextHookEx.Call(0, code, wparam, lparam)
	return r
}

func install(kind uintptr, callback uintptr) (*Hook, error) {
	h := &Hook{done: make(chan struct{})}
	ready := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(h.done)
		tid, _, _ := pGetThreadID.Call()
		h.tid = uint32(tid)
		mod, _, _ := kernel32.NewProc("GetModuleHandleW").Call(0)
		hook, _, err := pSetWindowsHookEx.Call(kind, callback, mod, 0)
		if hook == 0 {
			ready <- errors.New("无法安装输入钩子：" + err.Error())
			return
		}
		ready <- nil
		var msg [8]uintptr // MSG is 48 bytes on 64-bit
		for {
			r, _, _ := pGetMessage.Call(ptr(&msg[0]), 0, 0, 0)
			if int32(r) <= 0 { // WM_QUIT or an error
				break
			}
		}
		pUnhookWindowsHookEx.Call(hook)
	}()
	if err := <-ready; err != nil {
		return nil, err
	}
	return h, nil
}

// InstallKeyboard starts watching the keyboard system-wide.
func InstallKeyboard(fn KeyHandler) (*Hook, error) {
	for i := range kbdSwallowed {
		kbdSwallowed[i].Store(false)
	}
	mine := &fn
	kbdHandler.Store(mine)
	h, err := install(whKeyboardLL, kbdCallback)
	if err != nil {
		kbdHandler.CompareAndSwap(mine, nil)
		return nil, err
	}
	// Only the handler this hook installed is dropped with it.
	h.clear = func() { kbdHandler.CompareAndSwap(mine, nil) }
	return h, nil
}

// InstallMouse starts watching the mouse buttons system-wide.
func InstallMouse(fn MouseHandler) (*Hook, error) {
	for i := range mouseSwallowed {
		mouseSwallowed[i].Store(false)
	}
	mine := &fn
	mouseHandler.Store(mine)
	h, err := install(whMouseLL, mouseCallback)
	if err != nil {
		mouseHandler.CompareAndSwap(mine, nil)
		return nil, err
	}
	h.clear = func() { mouseHandler.CompareAndSwap(mine, nil) }
	return h, nil
}

// DoubleClickTime is the system's double click interval.
func DoubleClickTime() int {
	r, _, _ := pGetDoubleClickTime.Call()
	return int(r)
}

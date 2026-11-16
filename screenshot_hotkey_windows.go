//go:build windows

package main

import (
	"github.com/egoist/mygo"
	"golang.org/x/sys/windows"
	"runtime"
	"sync/atomic"
	"syscall"
	"time"
	"tolk/internal/doublekey"
	"unsafe"
)

var shotEnabled atomic.Bool
var shotUser = windows.NewLazySystemDLL("user32.dll")
var shotNext = shotUser.NewProc("CallNextHookEx")
var shotKey = shotUser.NewProc("GetAsyncKeyState")
var shotDetector doublekey.Detector
var shotSwallowed bool
var shotCallback = syscall.NewCallback(func(code int32, w, l uintptr) uintptr {
	if code >= 0 {
		event := (*struct {
			Key, Scan, Flags, Time uint32
			Extra                  uintptr
		})(unsafe.Pointer(l))
		if event.Flags&0x10 == 0 {
			down := w == 0x100 || w == 0x104
			x := event.Key == 0x58
			held := func(key uintptr) bool { v, _, _ := shotKey.Call(key); return v&0x8000 != 0 }
			ctrlOnly := held(0x11) && !held(0x12) && !held(0x10) && !held(0x5B) && !held(0x5C)
			observeCopyKey(event.Key, down, ctrlOnly) // Ctrl+C+C, doublecopy_windows.go; never swallowed
			if x && !down && shotSwallowed {
				shotSwallowed = false
				shotDetector.Observe(true, false, false, time.Now())
				return 1
			}
			if x && down && shotSwallowed {
				return 1
			}
			if !shotEnabled.Load() {
				shotDetector = doublekey.Detector{}
			} else if shotDetector.Observe(x, down, ctrlOnly, time.Now()) {
				shotSwallowed = true
				go func() {
					if err := (Tolk{}).ScreenshotWindows(); err != nil {
						Logs.Warnf("截图翻译：%v", err)
						_ = MacNotice.Broadcast(err.Error())
					}
				}()
				return 1
			}
		}
	}
	r, _, _ := shotNext.Call(0, uintptr(code), w, l)
	return r
})

// setupScreenshotShortcut installs the keyboard hook behind Ctrl+X+X and,
// since it sees every key anyway, Ctrl+C+C.
func setupScreenshotShortcut() {
	shotEnabled.Store(loadPrefs().ScreenshotHotkey)
	if shotEnabled.Load() {
		// Late enough not to slow Tolk's own start.
		time.AfterFunc(5*time.Second, func() { prewarmScreenshot(true) })
	}
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		mod, _, _ := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetModuleHandleW").Call(0)
		hook, _, err := shotUser.NewProc("SetWindowsHookExW").Call(13, shotCallback, mod, 0)
		keyHookOK.Store(hook != 0)
		close(keyHookDone)
		if hook == 0 {
			Logs.Errorf("键盘钩子安装失败，Ctrl+X、X 截图不可用：%v", err)
			return
		}
		defer shotUser.NewProc("UnhookWindowsHookEx").Call(hook)
		defer watchForeground()() // same thread: it pumps the messages both hooks need
		tid := windows.GetCurrentThreadId()
		var msg [8]uintptr
		shotUser.NewProc("PeekMessageW").Call(uintptr(unsafe.Pointer(&msg[0])), 0, 0, 0, 0)
		off := mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { shotUser.NewProc("PostThreadMessageW").Call(uintptr(tid), 0x12, 0, 0) })
		defer off()
		for {
			r, _, _ := shotUser.NewProc("GetMessageW").Call(uintptr(unsafe.Pointer(&msg[0])), 0, 0, 0)
			if int32(r) <= 0 {
				return
			}
		}
	}()
}
func (Tolk) SetWindowsScreenshotHotkey(on bool) error {
	if err := setTolkPref(func(m map[string]any) { m["screenshot_hotkey"] = on }); err != nil {
		return err
	}
	shotEnabled.Store(on)
	if on {
		go prewarmScreenshot(true)
	} else {
		discardScreenshotSpare()
	}
	return nil
}

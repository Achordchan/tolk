//go:build windows

package main

import (
	"github.com/egoist/mygo"
	"golang.org/x/sys/windows"
)

var (
	frontUser32                   = windows.NewLazySystemDLL("user32.dll")
	frontGetForegroundWindow      = frontUser32.NewProc("GetForegroundWindow")
	frontGetWindowThreadProcess   = frontUser32.NewProc("GetWindowThreadProcessId")
	frontAttachThreadInput        = frontUser32.NewProc("AttachThreadInput")
	frontSetForegroundWindow      = frontUser32.NewProc("SetForegroundWindow")
	frontBringWindowToTop         = frontUser32.NewProc("BringWindowToTop")
	frontIsIconic                 = frontUser32.NewProc("IsIconic")
	frontShowWindow               = frontUser32.NewProc("ShowWindow")
	frontAllowSetForegroundWindow = frontUser32.NewProc("AllowSetForegroundWindow")
)

const (
	swRestore = 9
	asfwAny   = ^uintptr(0) // ASFW_ANY = (DWORD)-1
)

// bringToFront shows win, restores it when minimized and puts it in front of
// every other window.
//
// Windows only lets the process that owns the foreground window (or got the
// last input event) take the foreground. Tolk is usually a background
// process when it is asked to show the window: double copy only watches the
// clipboard, and a second launch is another process. SetForegroundWindow is
// then refused, and a minimized window stays minimized with only its taskbar
// button flashing, or comes back behind the window the user is in. Joining
// the foreground thread's input queue for the duration lends Tolk that
// thread's right to change the foreground.
func bringToFront(win *mygo.Window) {
	mygo.RunOnMain(func() {
		hwnd := win.NativeHandle()
		if hwnd == 0 {
			return
		}
		// Join before Show: its own SetForegroundWindow would otherwise be
		// refused and flash the taskbar button.
		fg, _, _ := frontGetForegroundWindow.Call()
		if fg != hwnd && fg != 0 {
			self := uintptr(windows.GetCurrentThreadId())
			other, _, _ := frontGetWindowThreadProcess.Call(fg, 0)
			if other != 0 && other != self {
				if r, _, _ := frontAttachThreadInput.Call(self, other, 1); r != 0 {
					defer frontAttachThreadInput.Call(self, other, 0)
				}
			}
		}
		// Shows a hidden window (closing only hides it), but SW_SHOW leaves a
		// minimized one minimized.
		win.Show()
		if r, _, _ := frontIsIconic.Call(hwnd); r != 0 {
			frontShowWindow.Call(hwnd, swRestore)
		}
		frontBringWindowToTop.Call(hwnd)
		frontSetForegroundWindow.Call(hwnd)
		win.Focus() // keyboard focus into the page
	})
}

// allowForegroundHandoff lets the instance already running take the
// foreground when this one, started by the user and so in front, hands its
// command line over and exits. Without it the window it shows comes up behind.
func allowForegroundHandoff() { frontAllowSetForegroundWindow.Call(asfwAny) }

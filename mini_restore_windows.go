//go:build windows

package main

import (
	"github.com/egoist/mygo"
	"golang.org/x/sys/windows"
)

var miniShowWindow = windows.NewLazySystemDLL("user32.dll").NewProc("ShowWindow")

// SW_SHOWNOACTIVATE restores size and position without taking keyboard focus.
func restoreMiniInactive(w *mygo.Window) {
	if w.IsMinimized() {
		miniShowWindow.Call(w.NativeHandle(), 4)
	}
}

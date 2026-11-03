//go:build windows

package main

import (
	"unsafe"

	"github.com/egoist/mygo"
	"golang.org/x/sys/windows"
)

var dwmSetWindowAttribute = windows.NewLazySystemDLL("dwmapi.dll").NewProc("DwmSetWindowAttribute")

// nativeTitleBar colours the title bar and its text (Windows 11; earlier
// versions ignore the attributes). off gives both back to the system.
func nativeTitleBar(w *mygo.Window, on bool, bg, fg uint32) {
	const (
		dwmwaCaptionColor = 35
		dwmwaTextColor    = 36
		dwmwaColorDefault = 0xFFFFFFFF
	)
	caption, text := uint32(dwmwaColorDefault), uint32(dwmwaColorDefault)
	if on {
		caption, text = colorref(bg), colorref(fg)
	}
	mygo.RunOnMain(func() {
		hwnd := w.NativeHandle()
		if hwnd == 0 || dwmSetWindowAttribute.Find() != nil {
			return
		}
		dwmSetWindowAttribute.Call(hwnd, dwmwaCaptionColor, uintptr(unsafe.Pointer(&caption)), 4)
		dwmSetWindowAttribute.Call(hwnd, dwmwaTextColor, uintptr(unsafe.Pointer(&text)), 4)
	})
}

// colorref turns 0xRRGGBB into a COLORREF (0x00BBGGRR).
func colorref(rgb uint32) uint32 {
	return (rgb&0xff)<<16 | rgb&0xff00 | rgb>>16&0xff
}

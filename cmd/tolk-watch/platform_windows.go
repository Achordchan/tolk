package main

import (
	"os/exec"
	"syscall"
	"unsafe"
)

// Tolk is a GUI executable: it needs normal startup visibility, not SW_HIDE.
// The launcher's windowsgui build already prevents a console window.
func prepare(c *exec.Cmd) { c.SysProcAttr = nil }

// Transfer foreground activation permission from the user-launched process
// to its GUI child. Windows may refuse background/autostart activation.
func allowForeground(c *exec.Cmd) {
	if c.Process != nil {
		_, _, _ = syscall.NewLazyDLL("user32.dll").NewProc("AllowSetForegroundWindow").Call(uintptr(c.Process.Pid))
	}
}

// coreMissing shows the problem in a message box: the windowsgui launcher has
// no console, so without it the user sees nothing happen.
func coreMissing(child string) {
	text, err := syscall.UTF16PtrFromString(coreMissingMessage(child))
	if err != nil {
		return
	}
	title, _ := syscall.UTF16PtrFromString("Tolk")
	const mbIconError, mbSetForeground = 0x10, 0x10000
	_, _, _ = syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), mbIconError|mbSetForeground)
}

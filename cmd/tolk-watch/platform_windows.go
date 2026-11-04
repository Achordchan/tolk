package main

import (
	"os/exec"
	"syscall"
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

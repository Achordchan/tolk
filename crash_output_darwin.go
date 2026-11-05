//go:build darwin

package main

import (
	"os"
	"runtime/debug"
	"syscall"
)

// redirectCrashOutput points fd 2 at f. debug.SetCrashOutput alone is not
// enough: it only copies what is printed once the crash has begun, which is
// after a fatal error's own "fatal error: …" line. The log view's copy of each
// line moves to a duplicate of the old stderr, so it stays out of the crash log.
func redirectCrashOutput(f *os.File) error {
	fd, err := syscall.Dup(2)
	if err != nil {
		return err
	}
	syscall.CloseOnExec(fd)
	terminal := os.NewFile(uintptr(fd), "stderr")
	if err := syscall.Dup2(int(f.Fd()), 2); err != nil {
		terminal.Close()
		return err
	}
	Logs.SetTee(terminal)
	// A terminal, if there is one, still gets the dump.
	_ = debug.SetCrashOutput(terminal, debug.CrashOptions{})
	return nil
}

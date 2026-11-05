//go:build windows

package main

import (
	"os"
	"runtime/debug"

	"golang.org/x/sys/windows"
)

// redirectCrashOutput points the runtime's stderr at f. debug.SetCrashOutput
// alone is not enough: it only copies what is printed once the crash has begun,
// which is after a fatal error's own "fatal error: …" line. The runtime asks
// Windows for the handle on every write, while os.Stderr keeps the original, so
// the log view's copy of each line and any console stay as they were.
func redirectCrashOutput(f *os.File) error {
	// The console, if there is one, still gets the dump. A GUI build has none
	// and this fails harmlessly.
	_ = debug.SetCrashOutput(os.Stderr, debug.CrashOptions{})
	return windows.SetStdHandle(windows.STD_ERROR_HANDLE, windows.Handle(f.Fd()))
}

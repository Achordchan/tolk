//go:build !windows && !darwin

package main

import (
	"os"
	"runtime/debug"
)

// redirectCrashOutput: Tolk ships for Windows and macOS only, so other systems
// keep the runtime's own option, which misses a fatal error's first line.
func redirectCrashOutput(f *os.File) error {
	return debug.SetCrashOutput(f, debug.CrashOptions{})
}

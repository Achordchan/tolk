//go:build !windows

package report

import (
	"golang.org/x/sys/unix"
	"os"
)

func lockState(f *os.File) error { return unix.Flock(int(f.Fd()), unix.LOCK_EX) }
func unlockState(f *os.File)     { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN) }

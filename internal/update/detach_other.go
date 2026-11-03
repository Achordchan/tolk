//go:build !windows

package update

import (
	"os/exec"
	"syscall"
)

func startDetached(cmd *exec.Cmd) error {
	if err := startBackground(cmd); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func startBackground(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}

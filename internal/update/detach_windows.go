//go:build windows

package update

import (
	"os/exec"
	"syscall"
)

func startDetached(cmd *exec.Cmd) error {
	const detachedProcess, createNoWindow = 0x00000008, 0x08000000
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | detachedProcess | createNoWindow}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

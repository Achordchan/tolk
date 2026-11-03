//go:build windows

package update

import (
	"os/exec"
	"syscall"
)

// startDetached starts the swap script so that it outlives the app. Windows
// does not end a child with its parent, so no "detach" flag is needed — and
// DETACHED_PROCESS must not be used: PowerShell started without a console
// exits at once without running the script, which is how 2.2.1 and 2.2.2
// quit for an update and then never updated (Windows CI reproduced it).
func startDetached(cmd *exec.Cmd) error {
	if err := startBackground(cmd); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// startBackground starts cmd the same way but keeps the process handle, so
// the caller can still stop it.
func startBackground(cmd *exec.Cmd) error {
	const createNoWindow = 0x08000000
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | createNoWindow}
	return cmd.Start()
}

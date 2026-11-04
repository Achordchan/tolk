//go:build !windows

package dlx

import "os/exec"

func prepare(cmd *exec.Cmd) {}

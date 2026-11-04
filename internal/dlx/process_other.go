//go:build !windows

package dlx

import "os/exec"

func prepare(cmd *exec.Cmd) {}

func supervise(cmd *exec.Cmd) (func(), error) { return func() {}, nil }

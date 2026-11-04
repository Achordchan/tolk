//go:build !windows

package main

import "os/exec"

func prepare(c *exec.Cmd)         {}
func allowForeground(c *exec.Cmd) {}

//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
)

func prepare(c *exec.Cmd)         {}
func allowForeground(c *exec.Cmd) {}

func coreMissing(child string) { fmt.Fprintln(os.Stderr, coreMissingMessage(child)) }

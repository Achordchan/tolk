package main

import (
	"os/exec"
	"testing"
)

func TestMainWindowIsNotHidden(t *testing.T) {
	cmd := exec.Command("Tolk.exe")
	prepare(cmd)
	if cmd.SysProcAttr != nil && cmd.SysProcAttr.HideWindow {
		t.Fatal("launcher must not hide the main application window")
	}
}

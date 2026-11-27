package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestMainWindowIsNotHidden(t *testing.T) {
	cmd := exec.Command("Tolk.exe")
	prepare(cmd)
	if cmd.SysProcAttr != nil && cmd.SysProcAttr.HideWindow {
		t.Fatal("launcher must not hide the main application window")
	}
}

// A core ended by Windows at shutdown or sign-out (#58) leaves no startup log
// and no crash report behind.
func TestSessionEndLeavesNoReport(t *testing.T) {
	if os.Getenv("TOLK_SUPERVISED") == "1" {
		os.Exit(dbgTerminateProcess) // the core, as Windows ends it
	}
	if os.Getenv("TOLK_WATCH_MAIN") == "1" {
		main()
		return
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	launcher := filepath.Join(dir, "Tolk.exe")
	for _, p := range []string{launcher, corePath(launcher)} {
		if err = os.WriteFile(p, b, 0755); err != nil {
			t.Fatal(err)
		}
	}
	home := filepath.Join(dir, "home")
	// Reports stay at their default (off), so a regression here cannot upload.
	cmd := exec.Command(launcher, "-test.run=^TestSessionEndLeavesNoReport$")
	cmd.Env = append(os.Environ(), "TOLK_WATCH_MAIN=1", "TOLK_HOME="+home)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	left, _ := filepath.Glob(filepath.Join(home, "update_state", "startup-*.log"))
	crashes, _ := filepath.Glob(filepath.Join(home, "update_state", "crash-source-*.json"))
	if len(left) > 0 || len(crashes) > 0 {
		t.Fatalf("session end left %v %v", left, crashes)
	}
}

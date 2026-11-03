//go:build windows

package main

import (
	"os"
	"os/exec"
	"testing"
)

// The script runs hidden and its errors only reach the console, so at least
// make sure PowerShell can parse it.
func TestTidyShortcutsScriptParses(t *testing.T) {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command",
		`[void][ScriptBlock]::Create($env:TOLK_SCRIPT); 'ok'`)
	cmd.Env = append(os.Environ(), "TOLK_SCRIPT="+tidyShortcutsScript)
	out, err := cmd.CombinedOutput()
	if err != nil || string(out) != "ok\r\n" {
		t.Fatalf("%v: %s", err, out)
	}
}

//go:build windows

package update

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Runs the real swap script under Windows PowerShell against fake install dirs.
func runWindowsScript(t *testing.T, sourceVersion string) (target string, result string) {
	t.Helper()
	root := t.TempDir()
	source, target := filepath.Join(root, "source"), filepath.Join(root, "target")
	for _, d := range []string{source, target} {
		os.MkdirAll(d, 0o755)
		os.WriteFile(filepath.Join(d, "app.cmd"), []byte("@echo started> \"%~dp0started.txt\"\r\n"), 0o644)
	}
	os.WriteFile(filepath.Join(target, "old.txt"), []byte("old"), 0o644)
	os.WriteFile(filepath.Join(target, "unins000.exe"), []byte("uninstaller"), 0o644)
	os.WriteFile(filepath.Join(source, "new.txt"), []byte("new"), 0o644)
	os.WriteFile(filepath.Join(source, "update_manifest.json"), []byte(`{"app_version":"`+sourceVersion+`"}`), 0o644)
	script := filepath.Join(root, "apply.ps1")
	os.WriteFile(script, []byte("\xef\xbb\xbf"+strings.ReplaceAll(windowsApplyScript, "\n", "\r\n")), 0o600)
	plan := &WindowsPlan{SourceDir: source, ExeName: "app.cmd", Version: "9.9.9"}
	cmd := exec.Command("powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", script)
	cmd.Env = append(os.Environ(), plan.Env(target, filepath.Join(root, "backup"), filepath.Join(root, "log.txt"), filepath.Join(root, "result.json"), 999999)...)
	out, _ := cmd.CombinedOutput()
	t.Logf("script output: %s", out)
	time.Sleep(2 * time.Second) // Start-Process is asynchronous
	b, _ := os.ReadFile(filepath.Join(root, "result.json"))
	return target, string(b)
}

func TestWindowsScriptSwapsInstallDir(t *testing.T) {
	target, result := runWindowsScript(t, "9.9.9")
	if !strings.Contains(result, "success") {
		t.Fatalf("result: %s", result)
	}
	if _, err := os.Stat(filepath.Join(target, "new.txt")); err != nil {
		t.Error("new file missing")
	}
	if _, err := os.Stat(filepath.Join(target, "old.txt")); err == nil {
		t.Error("old file must be mirrored away")
	}
	if _, err := os.Stat(filepath.Join(target, "unins000.exe")); err != nil {
		t.Error("uninstaller must be preserved")
	}
	if _, err := os.Stat(filepath.Join(target, "started.txt")); err != nil {
		t.Error("app was not restarted")
	}
}

func TestWindowsScriptLeavesInstallUntouchedOnBadPackage(t *testing.T) {
	target, result := runWindowsScript(t, "1.0.0")
	if !strings.Contains(result, "failed") {
		t.Fatalf("result: %s", result)
	}
	if _, err := os.Stat(filepath.Join(target, "old.txt")); err != nil {
		t.Error("old install must stay when the package is rejected")
	}
}

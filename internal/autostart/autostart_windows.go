//go:build windows

package autostart

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

const shortcutName = "大佐翻译官.lnk"

func shortcutPath() (string, error) {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		return "", fmt.Errorf("找不到 APPDATA")
	}
	return filepath.Join(appData, "Microsoft", "Windows", "Start Menu", "Programs", "Startup", shortcutName), nil
}

func run(script string, env ...string) (string, error) {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", script)
	cmd.Env = append(os.Environ(), env...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// Enabled reports whether the shortcut exists and points at this program.
func Enabled() bool {
	lnk, err := shortcutPath()
	if err != nil {
		return false
	}
	if _, err := os.Stat(lnk); err != nil {
		return false
	}
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	target, err := run(`(New-Object -ComObject WScript.Shell).CreateShortcut($env:TOLK_LNK).TargetPath`, "TOLK_LNK="+lnk)
	if err != nil {
		return true // present but unreadable: do not claim it is off
	}
	return strings.EqualFold(filepath.Clean(target), filepath.Clean(exe))
}

// Set creates or removes the shortcut.
func Set(on bool) error {
	lnk, err := shortcutPath()
	if err != nil {
		return err
	}
	if !on {
		if err := os.Remove(lnk); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(lnk), 0o755); err != nil {
		return err
	}
	out, err := run(`$s=(New-Object -ComObject WScript.Shell).CreateShortcut($env:TOLK_LNK);`+
		`$s.TargetPath=$env:TOLK_EXE;$s.WorkingDirectory=Split-Path -Parent $env:TOLK_EXE;`+
		`$s.IconLocation=$env:TOLK_EXE;$s.Description='Tolk';$s.Save()`,
		"TOLK_LNK="+lnk, "TOLK_EXE="+exe)
	if err != nil {
		return fmt.Errorf("创建开机自启快捷方式失败：%v %s", err, out)
	}
	return nil
}

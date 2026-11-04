//go:build windows

package autostart

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"golang.org/x/sys/windows/registry"
)

// shortcutName is the Startup-folder entry; Windows lists it by this name
// under 启动应用. Versions up to 2.2.2 named it oldShortcutName.
const (
	shortcutName    = "Tolk.lnk"
	oldShortcutName = "大佐翻译官.lnk"
)

func startupDir() (string, error) {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		return "", fmt.Errorf("找不到 APPDATA")
	}
	return filepath.Join(appData, "Microsoft", "Windows", "Start Menu", "Programs", "Startup"), nil
}

// mu keeps the startup rename and the settings switch from interleaving.
var mu sync.Mutex

func shortcutPath() (string, error) {
	dir, err := startupDir()
	return filepath.Join(dir, shortcutName), err
}

// oldExeName is the program name of installs up to 2.2.2; 2.2.3 moved them to
// Tolk.exe in a Tolk folder.
const oldExeName = "大佐翻译官.exe"

// Migrate brings an entry made by 2.2.2 or older up to date: renamed to Tolk,
// so Windows lists Tolk among the startup apps, and pointed at this program
// when it still points at the old one, which the 2.2.3 installer removed.
func Migrate() error {
	mu.Lock()
	defer mu.Unlock()
	if err := rename(); err != nil {
		return err
	}
	lnk, err := shortcutPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(lnk); err != nil {
		return nil
	}
	target, err := run(`(New-Object -ComObject WScript.Shell).CreateShortcut($env:TOLK_LNK).TargetPath`, "TOLK_LNK="+lnk)
	if err != nil || !strings.EqualFold(filepath.Base(target), oldExeName) {
		return nil
	}
	if _, err := os.Stat(target); err == nil {
		return nil // that old program is still there
	}
	return set(true)
}

// rename moves an entry made by 2.2.2 or older to the Tolk name. The shortcut
// file itself is renamed, so its target and properties stay as they were. It
// does nothing when there is no old entry.
func rename() error {
	dir, err := startupDir()
	if err != nil {
		return err
	}
	old := filepath.Join(dir, oldShortcutName)
	if _, err := os.Stat(old); err != nil {
		return nil
	}
	lnk := filepath.Join(dir, shortcutName)
	if _, err := os.Stat(lnk); err != nil {
		// Turning a startup app off in Settings or Task Manager keeps the
		// shortcut and records the choice by file name; carry it over first so
		// a switched-off app stays off.
		if err := carryApproval(oldShortcutName, shortcutName); err != nil {
			return err
		}
		if err := os.Rename(old, lnk); err != nil {
			return err
		}
	} else if err := os.Remove(old); err != nil {
		return err
	}
	dropApproval(oldShortcutName)
	return nil
}

// approvedKey holds the on/off switch Windows shows for each Startup-folder
// entry, as a binary value named after the shortcut file.
const approvedKey = `Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\StartupFolder`

// carryApproval copies the recorded switch of one entry to another; the old
// record stays until its shortcut is gone. Nothing
// recorded (never switched) is not an error.
func carryApproval(from, to string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, approvedKey, registry.QUERY_VALUE|registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer k.Close()
	v, _, err := k.GetBinaryValue(from)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return k.SetBinaryValue(to, v)
}

// dropApproval forgets the switch of an entry that is gone.
func dropApproval(name string) {
	k, err := registry.OpenKey(registry.CURRENT_USER, approvedKey, registry.SET_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	_ = k.DeleteValue(name)
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
	exe, err := launcherPath()
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
	mu.Lock()
	defer mu.Unlock()
	return set(on)
}

func set(on bool) error {
	lnk, err := shortcutPath()
	if err != nil {
		return err
	}
	if !on {
		for _, p := range []string{lnk, filepath.Join(filepath.Dir(lnk), oldShortcutName)} {
			if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		return nil
	}
	exe, err := launcherPath()
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

// launcherPath keeps auto-start on the supervised entry point.
func launcherPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if strings.EqualFold(filepath.Base(exe), "Tolk-core.exe") {
		if launcher := os.Getenv("TOLK_LAUNCHER_PATH"); launcher != "" && strings.EqualFold(filepath.Dir(launcher), filepath.Dir(exe)) {
			if _, err := os.Stat(launcher); err == nil {
				return launcher, nil
			}
		}
		return filepath.Join(filepath.Dir(exe), "Tolk.exe"), nil
	}
	return exe, nil
}

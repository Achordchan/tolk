package update

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"
)

// Dirs are where an install keeps its working files.
type Dirs struct {
	Cache  string // downloads and extracted packages
	Backup string // copy of the install being replaced (Windows)
	State  string // log and last result
}

// DefaultDirs match dazuo-win: everything lives under ~/.dzfyq (or
// $DZFYQ_HOME), so old logs and the pending-update guard stay in one place.
func DefaultDirs() (Dirs, error) {
	root := os.Getenv("DZFYQ_HOME")
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Dirs{}, err
		}
		root = filepath.Join(home, ".dzfyq")
	}
	return Dirs{
		Cache:  filepath.Join(root, "update_cache"),
		Backup: filepath.Join(root, "update_backup"),
		State:  filepath.Join(root, "update_state"),
	}, nil
}

// Install downloads, verifies and starts the swap of the running app. It
// returns once the swap script runs; the caller must then quit the app so the
// script can replace it.
func (c *Checker) Install(ctx context.Context, info *Info, dirs Dirs, progress func(done, total int64)) error {
	if info == nil {
		return errors.New("没有可安装的更新")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return err
	}
	path, err := c.Download(ctx, info, dirs.Cache, progress)
	if err != nil {
		return err
	}
	if err := Verify(info, path); err != nil {
		os.Remove(path)
		return err
	}
	if err := os.MkdirAll(dirs.State, 0o700); err != nil {
		return err
	}
	switch c.GOOS {
	case "windows":
		return installWindows(path, info, dirs, exe)
	case "darwin":
		return installMac(path, info, dirs, exe)
	}
	return errors.New("这个系统暂不支持自动更新")
}

func installWindows(zipPath string, info *Info, dirs Dirs, exe string) error {
	target := filepath.Dir(exe)
	if err := writable(target); err != nil {
		return errors.New("当前安装目录没有写入权限，无法静默替换更新。请先用安装包安装到用户目录，之后即可使用静默更新")
	}
	plan, err := PrepareWindows(zipPath, dirs.Cache, info.Version, filepath.Base(exe))
	if err != nil {
		return err
	}
	script := filepath.Join(dirs.Cache, "apply_update.ps1")
	// PowerShell 5 reads a BOM-less script in the ANSI code page, which would
	// garble the Chinese executable name.
	body := "\xef\xbb\xbf" + strings.ReplaceAll(windowsApplyScript, "\n", "\r\n")
	if err := os.WriteFile(script, []byte(body), 0o600); err != nil {
		return err
	}
	env := append(os.Environ(), plan.Env(target,
		filepath.Join(dirs.Backup, "v"+plan.Version),
		filepath.Join(dirs.State, "update.log"),
		filepath.Join(dirs.State, "last_update_result.json"),
		os.Getpid())...)
	cmd := exec.Command("powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-WindowStyle", "Hidden", "-File", script)
	cmd.Env = env
	return startDetached(cmd)
}

func writable(dir string) error {
	f, err := os.CreateTemp(dir, ".update_write_test_*")
	if err != nil {
		return err
	}
	name := f.Name()
	f.Close()
	return os.Remove(name)
}

// bundlePath returns the .app that contains exe.
func bundlePath(exe string) (string, error) {
	parts := strings.Split(exe, string(os.PathSeparator))
	for i, p := range parts {
		if strings.HasSuffix(p, ".app") {
			return strings.Join(parts[:i+1], string(os.PathSeparator)), nil
		}
	}
	return "", errors.New("当前程序不在 .app 包里，无法自动更新")
}

func installMac(zipPath string, info *Info, dirs Dirs, exe string) error {
	app, err := bundlePath(exe)
	if err != nil {
		return err
	}
	if err := writable(filepath.Dir(app)); err != nil {
		return fmt.Errorf("应用所在的文件夹不可写：%s。请把应用放到「应用程序」文件夹后再更新", filepath.Dir(app))
	}
	dest := filepath.Join(dirs.Cache, "mac_update_"+sanitize(info.Version))
	if err := os.RemoveAll(dest); err != nil {
		return err
	}
	if err := os.MkdirAll(dest, 0o700); err != nil {
		return err
	}
	// ditto keeps resource forks, symlinks and permissions inside the bundle.
	if out, err := exec.Command("/usr/bin/ditto", "-x", "-k", zipPath, dest).CombinedOutput(); err != nil {
		return fmt.Errorf("解压更新包失败：%v %s", err, strings.TrimSpace(string(out)))
	}
	newApp, err := findApp(dest)
	if err != nil {
		return err
	}
	if err := sameRequirement(app, newApp); err != nil {
		return err
	}
	script := filepath.Join(dirs.Cache, "apply_update.sh")
	if err := os.WriteFile(script, []byte(macApplyScript), 0o700); err != nil {
		return err
	}
	cmd := exec.Command("/bin/sh", script)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("TOLK_UPDATE_PID=%d", os.Getpid()),
		"TOLK_UPDATE_NEW="+newApp,
		"TOLK_UPDATE_TARGET="+app,
		"TOLK_UPDATE_BACKUP="+filepath.Join(dirs.Backup, filepath.Base(app)),
		"TOLK_UPDATE_LOG="+filepath.Join(dirs.State, "update.log"),
		"TOLK_UPDATE_RESULT="+filepath.Join(dirs.State, "last_update_result.json"),
		"TOLK_UPDATE_VERSION="+info.Version,
	)
	if err := os.MkdirAll(dirs.Backup, 0o700); err != nil {
		return err
	}
	return startDetached(cmd)
}

func findApp(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if e.IsDir() && strings.HasSuffix(e.Name(), ".app") {
			return filepath.Join(dir, e.Name()), nil
		}
	}
	return "", errors.New("更新包内没有应用")
}

// requirement returns the designated requirement of an app.
func requirement(app string) (string, error) {
	out, err := exec.Command("/usr/bin/codesign", "-d", "-r-", app).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("读取签名身份失败：%v", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if i := strings.Index(line, "designated => "); i >= 0 && utf8.ValidString(line) {
			return strings.TrimSpace(line[i+len("designated => "):]), nil
		}
	}
	return "", errors.New("读不到签名身份要求")
}

// sameRequirement refuses an update signed by a different identity: the
// user's Accessibility, Screen Recording and Keychain grants follow the
// signing identity, so a different one would silently reset them. A running
// build without a stable identity (ad hoc, cdhash) has nothing to protect.
func sameRequirement(running, update string) error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	have, err := requirement(running)
	if err != nil || strings.Contains(have, "cdhash") {
		return nil
	}
	want, err := requirement(update)
	if err != nil {
		return err
	}
	if have != want {
		return errors.New("新版本的签名身份与当前版本不同，为了保住已有的系统授权，已取消自动更新。请到发布页手动下载安装")
	}
	return nil
}

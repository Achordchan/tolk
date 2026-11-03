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
	"time"
	"unicode/utf8"

	"tolk/internal/legacy"
)

// Dirs are where an install keeps its working files.
type Dirs struct {
	Cache  string // downloads and extracted packages
	Backup string // copy of the install being replaced (Windows)
	State  string // log and last result
}

// DefaultDirs live in the settings folder (~/.tolk, or $TOLK_HOME).
func DefaultDirs() (Dirs, error) {
	root, err := legacy.Home()
	if err != nil {
		return Dirs{}, err
	}
	return Dirs{
		Cache:  filepath.Join(root, "update_cache"),
		Backup: filepath.Join(root, "update_backup"),
		State:  filepath.Join(root, "update_state"),
	}, nil
}

// Pending is an update that has been downloaded, verified and unpacked, and
// only waits for the app to make way.
type Pending struct {
	Info   *Info
	goos   string
	dirs   Dirs
	target string       // Windows: install dir; macOS: the running .app
	plan   *WindowsPlan // Windows
	newApp string       // macOS
}

// Install downloads, verifies and starts the swap of the running app, which
// starts the new version when it is done. It returns once the swap script
// runs; the caller must then quit the app so the script can replace it.
func (c *Checker) Install(ctx context.Context, info *Info, dirs Dirs, progress func(done, total int64)) error {
	p, err := c.Prepare(ctx, info, dirs, progress)
	if err != nil {
		return err
	}
	return p.Apply(true)
}

// ErrCannotInstall marks a copy of Tolk that cannot update itself where it
// is; trying again later will not change that.
var ErrCannotInstall = errors.New("无法自动更新")

// Prepare downloads, verifies and unpacks an update without touching the
// running app.
func (c *Checker) Prepare(ctx context.Context, info *Info, dirs Dirs, progress func(done, total int64)) (*Pending, error) {
	if info == nil {
		return nil, errors.New("没有可安装的更新")
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return nil, err
	}
	if c.GOOS != "windows" && c.GOOS != "darwin" {
		return nil, errors.New("这个系统暂不支持自动更新")
	}
	// Whether this copy can update itself at all is known before anything is
	// downloaded; such a refusal is lasting (ErrCannotInstall), not worth
	// retrying.
	p := &Pending{Info: info, goos: c.GOOS, dirs: dirs}
	if c.GOOS == "windows" {
		p.target = filepath.Dir(exe)
		if err := writable(p.target); err != nil {
			return nil, fmt.Errorf("%w：当前安装目录没有写入权限，无法静默替换更新。请先用安装包安装到用户目录，之后即可使用静默更新", ErrCannotInstall)
		}
		// The swap makes the folder exactly the package (plus the
		// uninstaller), so it must be a folder the installer made for Tolk,
		// not wherever a copy of the exe happens to sit (Downloads…).
		if !installedDir(p.target) {
			return nil, fmt.Errorf("%w：Tolk 没有用安装包安装（所在文件夹里没有卸载程序），不能自动更新。请用安装包安装一次，之后就能自动更新", ErrCannotInstall)
		}
	} else {
		if p.target, err = bundlePath(exe); err != nil {
			return nil, fmt.Errorf("%w：%v", ErrCannotInstall, err)
		}
		if err := writable(filepath.Dir(p.target)); err != nil {
			return nil, fmt.Errorf("%w：应用所在的文件夹不可写：%s。请把应用放到「应用程序」文件夹后再更新", ErrCannotInstall, filepath.Dir(p.target))
		}
	}
	path, err := c.Download(ctx, info, dirs.Cache, progress)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dirs.State, 0o700); err != nil {
		return nil, err
	}
	if c.GOOS == "windows" {
		if p.plan, err = PrepareWindows(path, dirs.Cache, info.Version, filepath.Base(exe)); err != nil {
			return nil, err
		}
		return p, nil
	}
	dest := filepath.Join(dirs.Cache, "mac_update_"+sanitize(info.Version))
	if err := os.RemoveAll(dest); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dest, 0o700); err != nil {
		return nil, err
	}
	// ditto keeps resource forks, symlinks and permissions inside the bundle.
	if out, err := exec.Command("/usr/bin/ditto", "-x", "-k", path, dest).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("解压更新包失败：%v %s", err, strings.TrimSpace(string(out)))
	}
	if p.newApp, err = findApp(dest); err != nil {
		return nil, err
	}
	if err := sameRequirement(p.target, p.newApp); err != nil {
		return nil, err
	}
	// The archive's signature says nothing about which version it is: an
	// appcast from a mirror could pass off an older signed build as new.
	if err := bundleVersion(p.newApp, info.Version); err != nil {
		return nil, err
	}
	return p, nil
}

// bundleVersion checks that the app bundle is the version it was offered as.
func bundleVersion(app, want string) error {
	out, err := exec.Command("/usr/bin/plutil", "-extract", "CFBundleShortVersionString", "raw", "-o", "-",
		filepath.Join(app, "Contents", "Info.plist")).Output()
	if err != nil {
		return fmt.Errorf("读不到新版本的版本号：%v", err)
	}
	if got := NormalizeVersion(string(out)); got != NormalizeVersion(want) {
		return fmt.Errorf("更新包里是 %s，不是发布信息说的 %s，已终止更新", got, want)
	}
	return nil
}

// Apply starts the script that swaps in the update once this process has
// exited. relaunch starts the new version afterwards; it is false when the
// user is quitting. The caller must exit soon after.
func (p *Pending) Apply(relaunch bool) error {
	if p.goos == "windows" {
		return p.applyWindows(relaunch)
	}
	return p.applyMac(relaunch)
}

func (p *Pending) applyWindows(relaunch bool) error {
	dirs, plan := p.dirs, p.plan
	script := filepath.Join(dirs.Cache, "apply_update.ps1")
	// PowerShell 5 reads a BOM-less script in the ANSI code page, which would
	// garble a non-ASCII path.
	body := "\xef\xbb\xbf" + strings.ReplaceAll(windowsApplyScript, "\n", "\r\n")
	if err := os.WriteFile(script, []byte(body), 0o600); err != nil {
		return err
	}
	logPath := filepath.Join(dirs.State, "update.log")
	env := append(os.Environ(), plan.Env(p.target,
		filepath.Join(dirs.Backup, "v"+plan.Version),
		logPath,
		filepath.Join(dirs.State, "last_update_result.json"),
		os.Getpid(), relaunch)...)
	before := fileSize(logPath)
	cmd := exec.Command("powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-WindowStyle", "Hidden", "-File", script)
	cmd.Env = env
	if err := startBackground(cmd); err != nil {
		return err
	}
	// The app quits as soon as this returns, so make sure the script is
	// really running: its first act is a line in update.log. Without this
	// check a script that never ran looked like an update that just did
	// not happen.
	if !waitGrow(logPath, before, scriptStartTimeout) {
		// Stop it, so a script that starts late cannot still kill the app
		// and swap files after the update was reported as failed, or race
		// a retry. Until the app exits it only waits, so this is safe even
		// if it got going at the last moment.
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		return errors.New("更新脚本没有启动起来，可能被安全软件拦截了。请到 GitHub 下载安装包覆盖安装")
	}
	return cmd.Process.Release()
}

func (p *Pending) applyMac(relaunch bool) error {
	dirs := p.dirs
	script := filepath.Join(dirs.Cache, "apply_update.sh")
	if err := os.WriteFile(script, []byte(macApplyScript), 0o700); err != nil {
		return err
	}
	cmd := exec.Command("/bin/sh", script)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("TOLK_UPDATE_PID=%d", os.Getpid()),
		"TOLK_UPDATE_NEW="+p.newApp,
		"TOLK_UPDATE_TARGET="+p.target,
		"TOLK_UPDATE_BACKUP="+filepath.Join(dirs.Backup, filepath.Base(p.target)),
		"TOLK_UPDATE_LOG="+filepath.Join(dirs.State, "update.log"),
		"TOLK_UPDATE_RESULT="+filepath.Join(dirs.State, "last_update_result.json"),
		"TOLK_UPDATE_VERSION="+p.Info.Version,
		"TOLK_UPDATE_RELAUNCH="+map[bool]string{true: "1", false: "0"}[relaunch],
	)
	if err := os.MkdirAll(dirs.Backup, 0o700); err != nil {
		return err
	}
	return startDetached(cmd)
}

// waitGrow waits until the file at path is larger than before.
func waitGrow(path string, before int64, timeout time.Duration) bool {
	for deadline := time.Now().Add(timeout); fileSize(path) <= before; time.Sleep(100 * time.Millisecond) {
		if time.Now().After(deadline) {
			return false
		}
	}
	return true
}

// scriptStartTimeout is how long installWindows waits for the swap script to
// write its first log line; PowerShell can take a few seconds on a busy PC.
var scriptStartTimeout = 20 * time.Second

func fileSize(path string) int64 {
	fi, err := os.Stat(path)
	if err != nil {
		return -1
	}
	return fi.Size()
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

// installedDir reports whether dir is where the installer put Tolk: Tolk's
// own uninstall entry names it, and the uninstaller sits there. Any other
// folder holding Tolk.exe (Downloads, a tools folder shared with other
// programs) is not swapped wholesale.
func installedDir(dir string) bool {
	return ownedBy(dir, installLocation())
}

// uninstallKey is Tolk's entry under HKCU\...\Uninstall (the AppId kept from
// 大佐翻译官).
const uninstallKey = `Software\Microsoft\Windows\CurrentVersion\Uninstall\{5E76B529-0B3B-4E93-AE4D-DCF0EA7174CE}_is1`

func ownedBy(dir, location string) bool {
	if location == "" || !strings.EqualFold(resolved(dir), resolved(location)) {
		return false
	}
	m, _ := filepath.Glob(filepath.Join(dir, "unins*.exe"))
	return len(m) > 0
}

// resolved is path with links (a junction the installer went through)
// followed, so the recorded and the running path compare alike.
func resolved(path string) string {
	path = filepath.Clean(path)
	if r, err := filepath.EvalSymlinks(path); err == nil {
		return r
	}
	return path
}

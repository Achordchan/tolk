package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"

	"tolk/internal/autostart"
	"tolk/internal/legacy"
	"tolk/internal/logbuf"
	"tolk/internal/update"
)

// PrefsChanged tells every window to re-read the look (theme, text sizes).
var PrefsChanged = mygo.NewEvent[Prefs]("prefsChanged")

// UpdateState is what the update row of the About pane shows.
type UpdateState struct {
	// idle, checking, latest, available (found, download about to start),
	// downloading, ready (downloaded and verified; installs on restart or
	// quit), installing, manual (found, but this copy cannot update itself:
	// Error says why, and the new version is installed by hand), error
	State   string `json:"state"`
	Version string `json:"version"`
	Notes   string `json:"notes"`
	Force   bool   `json:"force"`
	Percent int    `json:"percent"`
	Error   string `json:"error"`
	Source  string `json:"source"`
	// Download is where to get the installer by hand (state manual).
	Download string `json:"download"`
}

// installerURL is the download of the latest installer for goos on the
// website, which always points at the newest release (Windows installer,
// macOS disk image) through the faster mirror.
func installerURL(goos string) string {
	if goos == "darwin" {
		return "https://tolk.achord.cn/download/mac?via=mirror"
	}
	return "https://tolk.achord.cn/download/win?via=mirror"
}

// UpdateChanged is broadcast whenever the update state changes.
var UpdateChanged = mygo.NewEvent[UpdateState]("updateChanged")

// Logs is the console's buffer; everything the app logs lands here.
var Logs = logbuf.New(2000, os.Stderr)

// About is the About pane's data.
type About struct {
	Version    string `json:"version"`
	OS         string `json:"os"`
	Arch       string `json:"arch"`
	ConfigPath string `json:"configPath"`
	Helper     string `json:"helper"` // "" when this system has no helper
}

// About returns version and environment details for the About pane.
func (Tolk) About() About {
	path, _ := legacy.DefaultPath()
	return About{Version: Version, OS: runtime.GOOS, Arch: runtime.GOARCH, ConfigPath: path, Helper: helperVersion()}
}

// Autostart reports whether Tolk starts at login.
func (Tolk) Autostart() bool { return autostart.Enabled() }

// SetAutostart turns start-at-login on or off.
func (Tolk) SetAutostart(on bool) error {
	if err := autostart.Set(on); err != nil {
		Logs.Errorf("开机自启设置失败：%v", err)
		return err
	}
	Logs.Infof("开机自启已%s", map[bool]string{true: "开启", false: "关闭"}[on])
	return nil
}

// ConsoleLines returns the log lines newer than after.
func (Tolk) ConsoleLines(after int64) []logbuf.Entry { return Logs.Since(after) }

// ClearConsole empties the console.
func (Tolk) ClearConsole() { Logs.Clear() }

// ---- updates ----
//
// A new version downloads by itself in the background. Once it has verified,
// the page offers 重启更新; otherwise it goes in when the user quits Tolk.

var upd struct {
	mu          sync.Mutex
	state       UpdateState
	info        *update.Info    // newest version found
	pending     *update.Pending // downloaded, verified, waiting to go in
	checking    bool            // a check is under way
	downloading bool
	installNow  bool // the user asked to install while it was downloading
	applied     bool // a swap script is running; nothing more to do
	reopens     bool // the running swap opens Tolk again when it is done
	closed      bool // the quit handler has decided; no swap starts after it
	// launching is closed once the swap claimed in applied has started or
	// failed to; until then whether it reopens Tolk is not settled.
	launching chan struct{}
	// reopen is the app bundle to open again once Tolk has quit (relaunchApp).
	// Only the quit handler acts on it, so one place decides who reopens.
	reopen string
}

func setUpdate(s UpdateState) {
	upd.mu.Lock()
	upd.state = s
	upd.mu.Unlock()
	_ = UpdateChanged.Broadcast(s)
}

func newChecker() *update.Checker {
	c := update.NewChecker(runtime.GOOS, Version)
	c.Logf = Logs.Infof
	return c
}

// UpdateStatus returns the last known update state.
func (Tolk) UpdateStatus() UpdateState {
	upd.mu.Lock()
	defer upd.mu.Unlock()
	if upd.state.State == "" {
		return UpdateState{State: "idle"}
	}
	return upd.state
}

// CheckUpdate asks GitHub whether a newer version exists, and starts
// downloading it if so.
func (Tolk) CheckUpdate() (UpdateState, error) { return checkUpdate() }

func checkUpdate() (UpdateState, error) {
	upd.mu.Lock()
	// One check at a time: two at once (startup and 检查更新) could each pick
	// a release, and the second would lose to the first one's download.
	if upd.checking || upd.downloading || upd.applied {
		s := upd.state
		upd.mu.Unlock()
		return s, nil
	}
	upd.checking = true
	upd.mu.Unlock()
	defer func() {
		upd.mu.Lock()
		upd.checking = false
		upd.mu.Unlock()
	}()
	setUpdate(UpdateState{State: "checking"})
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	info, err := newChecker().Check(ctx, Version)
	upd.mu.Lock()
	pending := upd.pending
	upd.mu.Unlock()
	if pending != nil && (err != nil || info == nil || update.CompareVersions(pending.Info.Version, info.Version) >= 0) {
		// The downloaded one is still the newest we know of; 重启更新 must
		// install it, not go after a release that failed to download.
		// The same release may since have been made mandatory (GitHub's own
		// answer only; Force is never taken from a mirror).
		force := err == nil && info != nil && info.Force && info.Version == pending.Info.Version
		upd.mu.Lock()
		if force {
			pending.Info.Force = true
		}
		upd.info = pending.Info
		upd.mu.Unlock()
		s := UpdateState{State: "ready", Version: pending.Info.Version, Notes: pending.Info.Notes, Force: pending.Info.Force, Source: pending.Info.Source}
		setUpdate(s)
		if force {
			go applyUpdate(true)
		}
		return s, nil
	}
	if err != nil {
		Logs.Warnf("检查更新失败：%v", err)
		s := UpdateState{State: "error", Error: err.Error()}
		setUpdate(s)
		return s, nil
	}
	if info == nil {
		Logs.Infof("已是最新版本 v%s", Version)
		s := UpdateState{State: "latest"}
		setUpdate(s)
		return s, nil
	}
	upd.mu.Lock()
	upd.info = info
	upd.mu.Unlock()
	Logs.Infof("发现新版本 v%s（来源 %s）", info.Version, info.Source)
	s := UpdateState{State: "available", Version: info.Version, Notes: info.Notes, Force: info.Force, Source: info.Source}
	setUpdate(s)
	startDownload(info, info.Force)
	return s, nil
}

// startDownload fetches and verifies info in the background. install swaps
// it in right away (the user asked, or the release is forced); otherwise the
// update waits as ready.
func startDownload(info *update.Info, install bool) {
	upd.mu.Lock()
	if upd.downloading || upd.applied {
		// One is already on its way: an install request rides along.
		if install && upd.downloading {
			upd.installNow = true
		}
		upd.mu.Unlock()
		return
	}
	upd.downloading = true
	upd.mu.Unlock()
	go downloadUpdate(info, install)
}

func downloadUpdate(info *update.Info, install bool) {
	pending, err := prepareUpdate(info)
	upd.mu.Lock()
	upd.downloading = false
	install = install || upd.installNow
	upd.installNow = false
	if err == nil {
		upd.pending = pending
	}
	// A newer release turned up while this one downloaded: get that instead.
	newer := upd.info
	if newer == nil || update.CompareVersions(newer.Version, info.Version) <= 0 {
		newer = nil
	}
	upd.mu.Unlock()
	if newer != nil {
		Logs.Infof("下载期间发现更新的 v%s，改下这个", newer.Version)
		startDownload(newer, install || newer.Force)
		return
	}
	if errors.Is(err, update.ErrCannotInstall) {
		needsManualInstall(info, err)
		return
	}
	if err != nil {
		Logs.Errorf("下载更新失败：%v", err)
		setUpdate(UpdateState{State: "error", Version: info.Version, Notes: info.Notes, Force: info.Force, Error: err.Error()})
		if !install {
			// A background download that broke tries again later by itself.
			time.AfterFunc(30*time.Minute, func() { _, _ = checkUpdate() })
		}
		return
	}
	if install {
		applyUpdate(true)
		return
	}
	Logs.Infof("v%s 已下载并校验，重启或退出时安装", info.Version)
	setUpdate(UpdateState{State: "ready", Version: info.Version, Notes: info.Notes, Force: info.Force, Source: info.Source})
}

// needsManualInstall reports a new version this copy cannot put in by itself
// (not installed by the installer, or in a folder it cannot write). It is how
// this copy was installed, not a fault, so it is a warning and never reported
// automatically; the user is pointed at the installer instead of 重试.
func needsManualInstall(info *update.Info, err error) {
	Logs.Warnf("v%s 需要手动安装：%v", info.Version, err)
	setUpdate(UpdateState{State: "manual", Version: info.Version, Notes: info.Notes, Force: info.Force, Error: err.Error(), Source: info.Source, Download: installerURL(runtime.GOOS)})
}

func prepareUpdate(info *update.Info) (*update.Pending, error) {
	c := newChecker()
	// Settled before the download starts, so the state never says
	// "downloading" for an update that cannot be installed.
	if err := c.Installable(); err != nil {
		return nil, err
	}
	dirs, err := update.DefaultDirs()
	if err != nil {
		return nil, err
	}
	state := UpdateState{State: "downloading", Version: info.Version, Notes: info.Notes, Force: info.Force, Source: info.Source}
	setUpdate(state)
	Logs.Infof("开始下载 v%s", info.Version)
	last := -1
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	return c.Prepare(ctx, info, dirs, func(done, total int64) {
		if total <= 0 {
			return
		}
		if pct := int(done * 100 / total); pct != last {
			last = pct
			state.Percent = pct
			setUpdate(state)
		}
	})
}

// InstallUpdate puts the update in now and restarts Tolk: at once when it has
// already downloaded, otherwise after the download (progress arrives as
// UpdateChanged events).
func (Tolk) InstallUpdate() error {
	upd.mu.Lock()
	info, pending, applied := upd.info, upd.pending, upd.applied
	if upd.downloading {
		upd.installNow = true
	}
	downloading := upd.downloading
	upd.mu.Unlock()
	switch {
	case applied, downloading:
	case pending != nil && (info == nil || pending.Info.Version == info.Version):
		go applyUpdate(true)
	case info == nil:
		return fmt.Errorf("还没有检查到新版本")
	default:
		startDownload(info, true)
	}
	return nil
}

// applyUpdate swaps in the pending update now: it quits Tolk so the swap can
// run, and the new version opens afterwards. It reports whether a swap
// started.
func applyUpdate(relaunch bool) bool {
	if out, _ := startSwap(relaunch, false); out != swapStarted {
		return false
	}
	if relaunch {
		time.Sleep(600 * time.Millisecond)
		mygo.App.Quit()
	}
	return true
}

// swapOutcome is what startSwap did.
type swapOutcome int

const (
	swapNone    swapOutcome = iota // nothing downloaded, or Tolk is past its quit decision
	swapStarted                    // the swap script is on its way
	swapFailed                     // there was an update, but it could not start
	swapBusy                       // a swap had already started
)

// startSwap starts the script that swaps in the pending update once this
// process exits; relaunch opens the new version afterwards. It also reports
// whether the swap now under way (this one or an earlier one) opens Tolk
// again. Checking for and claiming the swap happen under one lock, so two
// callers never both start one, and each learns which case it is in.
//
// quitting says it is the quit handler asking: the screen is not updated, a
// failure is left for the next launch since no swap script will report it,
// and nothing may start a swap after it (closed).
func startSwap(relaunch, quitting bool) (swapOutcome, bool) {
	upd.mu.Lock()
	// Another caller is still starting its swap: wait for it to have started
	// or failed, since only then is it known who reopens Tolk. Its failure
	// releases the claim, and this call goes on as if it had not been there.
	for upd.applied && upd.launching != nil {
		ch := upd.launching
		upd.mu.Unlock()
		<-ch
		upd.mu.Lock()
	}
	p := upd.pending
	switch {
	case upd.applied:
		reopens := upd.reopens
		upd.mu.Unlock()
		return swapBusy, reopens
	case upd.closed || p == nil:
		upd.closed = upd.closed || quitting
		upd.mu.Unlock()
		return swapNone, false
	}
	launched := make(chan struct{})
	upd.applied, upd.reopens, upd.launching = true, relaunch, launched
	if quitting {
		upd.closed = true
	}
	upd.mu.Unlock()
	defer func() {
		upd.mu.Lock()
		upd.launching = nil
		upd.mu.Unlock()
		close(launched)
	}()
	if !quitting {
		setUpdate(UpdateState{State: "installing", Version: p.Info.Version})
	}
	if err := p.Apply(relaunch); err != nil {
		if errors.Is(err, update.ErrCannotInstall) {
			Logs.Warnf("安装更新失败：%v", err)
		} else {
			Logs.Errorf("安装更新失败：%v", err)
		}
		if quitting {
			writeUpdateFailure(p.Info.Version, err)
		}
		upd.mu.Lock()
		upd.applied, upd.reopens = false, false
		upd.mu.Unlock()
		if !quitting {
			s := UpdateState{State: "error", Version: p.Info.Version, Notes: p.Info.Notes, Error: err.Error()}
			if errors.Is(err, update.ErrCannotInstall) {
				s.State, s.Download = "manual", installerURL(runtime.GOOS)
			}
			setUpdate(s)
		}
		return swapFailed, false
	}
	Logs.Infof("正在安装 v%s", p.Info.Version)
	return swapStarted, relaunch
}

// reopenAfterQuit asks for Tolk to be opened again once it has quit (a newly
// granted macOS permission only applies to a fresh process).
func reopenAfterQuit(app string) {
	upd.mu.Lock()
	upd.reopen = app
	upd.mu.Unlock()
}

// installOnQuit puts a downloaded update in when Tolk quits. It is also the
// one place that reopens Tolk after a quit (reopenAfterQuit): a swap replaces
// the bundle after this process exits, so opening it separately would race
// the swap and could open the old version, or nothing. Deciding here, at
// quit, nothing can turn up in between: with an update to put in, the swap
// script opens the new version itself; without, a wait-and-open starts.
func installOnQuit() {
	mygo.App.OnQuit(func() { quitHandler(startSwap, startReopen) })
}

func quitHandler(swap func(relaunch, quitting bool) (swapOutcome, bool), reopen func(app string) error) {
	upd.mu.Lock()
	app := upd.reopen
	upd.reopen = ""
	upd.mu.Unlock()
	// One call both claims the swap and says who owns reopening; after it no
	// other swap can start (closed), so the answer stays true until exit.
	out, reopens := swap(app != "", true)
	switch {
	case app == "" || out == swapStarted:
	case out == swapBusy:
		// 重启更新 started a swap just before: it replaces the bundle after
		// this process exits, and opening it as well would race that.
		if !reopens {
			Logs.Warnf("更新正在安装，装好后请手动打开 Tolk")
		}
	default: // nothing to put in, or it could not start: open it ourselves
		if err := reopen(app); err != nil {
			Logs.Errorf("%v", err)
		}
	}
}

// writeUpdateFailure records, in the file the swap scripts write, that the
// update promised for quitting did not start. A copy that needs the
// installer (ErrCannotInstall) is not a failure, and the next launch would
// report it automatically: nothing is written. The reason is not lost: a
// downloaded update lives only in memory, so the next launch downloads it
// again, and update.Checker.Prepare runs the same foreignFiles check that
// refused it here, which ends in the manual prompt.
func writeUpdateFailure(version string, err error) {
	if errors.Is(err, update.ErrCannotInstall) {
		return
	}
	dirs, derr := update.DefaultDirs()
	if derr != nil {
		return
	}
	b, _ := json.Marshal(map[string]string{"status": "failed", "message": err.Error(), "expected_version": version})
	_ = os.MkdirAll(dirs.State, 0o700)
	_ = os.WriteFile(filepath.Join(dirs.State, "last_update_result.json"), b, 0o600)
}

// UpdateNotice reports, once, how the update that just ran went: "" when
// there was none.
func (Tolk) UpdateNotice() string {
	dirs, err := update.DefaultDirs()
	if err != nil {
		return ""
	}
	path := filepath.Join(dirs.State, "last_update_result.json")
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	_ = os.Remove(path)
	var r struct {
		Status   string `json:"status"`
		Message  string `json:"message"`
		Expected string `json:"expected_version"`
	}
	if json.Unmarshal([]byte(strings.TrimPrefix(string(b), "\xef\xbb\xbf")), &r) != nil {
		return ""
	}
	if r.Status == "success" && update.CompareVersions(r.Expected, Version) == 0 {
		return "已更新到 v" + Version
	}
	if r.Status == "failed" {
		return "更新没装上：" + r.Message
	}
	return ""
}

// watchUpdates checks right after start and then daily; a failed check (no
// network yet at login) tries again sooner. A new version downloads in the
// background; a release marked update=1 installs by itself once its signature
// has verified.
func watchUpdates() {
	time.Sleep(3 * time.Second)
	for failures := 0; ; {
		s, _ := checkUpdate()
		if s.State == "error" && s.Version == "" {
			// Right after launch the network is often not ready yet; try
			// again soon so the About pane does not keep showing the error.
			failures++
			if failures <= 3 {
				time.Sleep(time.Duration(failures) * time.Minute)
			} else {
				time.Sleep(time.Hour)
			}
			continue
		}
		failures = 0
		time.Sleep(24 * time.Hour)
	}
}

func init() {
	log.SetOutput(Logs.Writer())
	log.SetFlags(0)
}

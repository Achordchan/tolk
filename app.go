package main

import (
	"context"
	"encoding/json"
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
	// quit), installing, error
	State   string `json:"state"`
	Version string `json:"version"`
	Notes   string `json:"notes"`
	Force   bool   `json:"force"`
	Percent int    `json:"percent"`
	Error   string `json:"error"`
	Source  string `json:"source"`
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
		upd.mu.Lock()
		upd.info = pending.Info
		upd.mu.Unlock()
		s := UpdateState{State: "ready", Version: pending.Info.Version, Notes: pending.Info.Notes, Force: pending.Info.Force, Source: pending.Info.Source}
		setUpdate(s)
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

func prepareUpdate(info *update.Info) (*update.Pending, error) {
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
	return newChecker().Prepare(ctx, info, dirs, func(done, total int64) {
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

// applyUpdate starts the swap of the pending update. relaunch is false when
// Tolk is quitting anyway; otherwise this quits Tolk so the swap can run.
func applyUpdate(relaunch bool) {
	upd.mu.Lock()
	p := upd.pending
	if p == nil || upd.applied {
		upd.mu.Unlock()
		return
	}
	upd.applied = true
	upd.mu.Unlock()
	if relaunch {
		setUpdate(UpdateState{State: "installing", Version: p.Info.Version})
	}
	if err := p.Apply(relaunch); err != nil {
		Logs.Errorf("安装更新失败：%v", err)
		upd.mu.Lock()
		upd.applied = false
		upd.mu.Unlock()
		if relaunch {
			setUpdate(UpdateState{State: "error", Version: p.Info.Version, Error: err.Error()})
		}
		return
	}
	Logs.Infof("正在安装 v%s", p.Info.Version)
	if relaunch {
		time.Sleep(600 * time.Millisecond)
		mygo.App.Quit()
	}
}

// installOnQuit puts a downloaded update in when the user quits, without
// starting Tolk again.
func installOnQuit() {
	mygo.App.OnQuit(func() { applyUpdate(false) })
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
	installOnQuit()
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

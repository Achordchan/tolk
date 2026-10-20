package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"runtime"
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
	State   string `json:"state"` // idle, checking, latest, available, downloading, installing, error
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

var upd struct {
	mu      sync.Mutex
	state   UpdateState
	info    *update.Info
	running bool
}

func setUpdate(s UpdateState) {
	upd.mu.Lock()
	upd.state = s
	upd.mu.Unlock()
	_ = UpdateChanged.Broadcast(s)
}

func newChecker() *update.Checker { return update.NewChecker(runtime.GOOS, Version) }

// UpdateStatus returns the last known update state.
func (Tolk) UpdateStatus() UpdateState {
	upd.mu.Lock()
	defer upd.mu.Unlock()
	if upd.state.State == "" {
		return UpdateState{State: "idle"}
	}
	return upd.state
}

// CheckUpdate asks the release sources whether a newer version exists.
func (Tolk) CheckUpdate() (UpdateState, error) { return checkUpdate() }

func checkUpdate() (UpdateState, error) {
	upd.mu.Lock()
	if upd.running {
		s := upd.state
		upd.mu.Unlock()
		return s, nil
	}
	upd.mu.Unlock()
	setUpdate(UpdateState{State: "checking"})
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	info, err := newChecker().Check(ctx, Version)
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
	Logs.Infof("发现新版本 v%s（来源 %s）", info.Version, info.Source)
	upd.mu.Lock()
	upd.info = info
	upd.mu.Unlock()
	s := UpdateState{State: "available", Version: info.Version, Notes: info.Notes, Force: info.Force, Source: info.Source}
	setUpdate(s)
	return s, nil
}

// InstallUpdate downloads the update found by CheckUpdate and swaps it in. It
// returns at once; progress arrives as UpdateChanged events, and the app quits
// when the swap script is running.
func (Tolk) InstallUpdate() error {
	upd.mu.Lock()
	info := upd.info
	if info == nil {
		upd.mu.Unlock()
		return fmt.Errorf("还没有检查到新版本")
	}
	if upd.running {
		upd.mu.Unlock()
		return nil
	}
	upd.running = true
	upd.mu.Unlock()
	go installUpdate(info)
	return nil
}

func installUpdate(info *update.Info) {
	defer func() {
		upd.mu.Lock()
		upd.running = false
		upd.mu.Unlock()
	}()
	dirs, err := update.DefaultDirs()
	if err != nil {
		setUpdate(UpdateState{State: "error", Version: info.Version, Error: err.Error()})
		return
	}
	state := UpdateState{State: "downloading", Version: info.Version, Notes: info.Notes, Force: info.Force}
	setUpdate(state)
	Logs.Infof("开始下载 v%s", info.Version)
	last := -1
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	err = newChecker().Install(ctx, info, dirs, func(done, total int64) {
		if total <= 0 {
			return
		}
		if pct := int(done * 100 / total); pct != last {
			last = pct
			state.Percent = pct
			setUpdate(state)
		}
	})
	if err != nil {
		Logs.Errorf("更新失败：%v", err)
		setUpdate(UpdateState{State: "error", Version: info.Version, Error: err.Error()})
		return
	}
	Logs.Infof("更新包已校验，正在重启安装 v%s", info.Version)
	setUpdate(UpdateState{State: "installing", Version: info.Version})
	time.Sleep(600 * time.Millisecond)
	mygo.App.Quit()
}

// watchUpdates checks shortly after start and then daily. A release marked
// update=1 installs by itself once its signature has verified, as in dazuo-win.
func watchUpdates() {
	time.Sleep(10 * time.Second)
	for {
		s, _ := checkUpdate()
		if s.State == "available" && s.Force {
			Logs.Warnf("v%s 是强制更新，自动安装", s.Version)
			_ = Tolk{}.InstallUpdate()
		}
		time.Sleep(24 * time.Hour)
	}
}

func init() {
	log.SetOutput(Logs.Writer())
	log.SetFlags(0)
}

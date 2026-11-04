// Package dlx manages the independently updated, loopback-only DLX service.
package dlx

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type State struct {
	Stage     string `json:"stage"`
	Route     string `json:"route"`
	Done      int64  `json:"done"`
	Total     int64  `json:"total"`
	Phase     string `json:"phase"`
	Installed string `json:"installed"`
	Latest    string `json:"latest"`
	Message   string `json:"message"`
}
type asset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Digest string `json:"digest"`
}
type release struct {
	Tag    string  `json:"tag_name"`
	Assets []asset `json:"assets"`
}

var mu sync.Mutex
var state State
var directory string
var Download func(context.Context, string, string, string, func(string, string, int64, int64)) (string, error)
var notify = func(State) {}
var client = &http.Client{Timeout: 3 * time.Minute}

func Configure(dir string, changed func(State)) {
	mu.Lock()
	defer mu.Unlock()
	directory = dir
	notify = changed
	b, _ := os.ReadFile(filepath.Join(dir, "current.json"))
	state = State{}
	_ = json.Unmarshal(b, &state)
	if !validVersion(state.Installed) {
		state.Installed = ""
	} else if _, err := os.Stat(executable(state.Installed)); err != nil {
		state.Installed = ""
	}
	state.Phase = "idle"
	state.Latest = ""
	state.Message = ""
}
func Status() State   { mu.Lock(); defer mu.Unlock(); return state }
func publish(s State) { mu.Lock(); state = s; cb := notify; mu.Unlock(); cb(s) }
func begin(phase string) (State, bool) {
	mu.Lock()
	defer mu.Unlock()
	if state.Phase == "checking" || state.Phase == "installing" {
		return state, false
	}
	state.Phase = phase
	state.Message = ""
	state.Stage = "检查版本"
	state.Route = ""
	state.Done = 0
	state.Total = 0
	return state, true
}
func executable(version string) string {
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	return filepath.Join(directory, version, "deeplx"+ext)
}
func validVersion(v string) bool {
	if !strings.HasPrefix(v, "v") {
		return false
	}
	for _, c := range v[1:] {
		if c != '.' && (c < '0' || c > '9') {
			return false
		}
	}
	return len(v) > 1
}
func assetName() string {
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	return "deeplx_" + runtime.GOOS + "_" + runtime.GOARCH + ext
}
func fetch(ctx context.Context) (release, asset, error) {
	var r release
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/repos/OwO-Network/DLX/releases/latest", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return r, asset{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return r, asset{}, fmt.Errorf("检查插件更新失败：HTTP %d", resp.StatusCode)
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&r); err != nil {
		return r, asset{}, err
	}
	if !validVersion(r.Tag) {
		return r, asset{}, fmt.Errorf("插件版本号无效")
	}
	for _, a := range r.Assets {
		if a.Name == assetName() {
			if !strings.HasPrefix(a.URL, "https://github.com/OwO-Network/DLX/releases/download/"+r.Tag+"/") || !strings.HasPrefix(a.Digest, "sha256:") || len(a.Digest) != 71 {
				return r, a, fmt.Errorf("插件发布缺少有效校验信息")
			}
			return r, a, nil
		}
	}
	return r, asset{}, fmt.Errorf("上游未提供 %s 插件", runtime.GOOS+"/"+runtime.GOARCH)
}
func Check() State {
	s, ok := begin("checking")
	if !ok {
		return s
	}
	notify(s)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r, _, err := fetch(ctx)
	if err != nil {
		s.Phase = "error"
		s.Message = err.Error()
	} else {
		s.Latest = r.Tag
		s.Phase = "ready"
	}
	publish(s)
	return s
}

// Install runs in the background; completion and errors are delivered as events.
func Install() {
	s, ok := begin("installing")
	if !ok {
		return
	}
	notify(s)
	go func() {
		err := install(&s)
		if err != nil {
			s.Phase = "error"
			s.Message = err.Error()
		} else {
			s.Phase = "ready"
			s.Message = "插件已更新"
		}
		publish(s)
	}()
}
func install(s *State) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	r, a, err := fetch(ctx)
	if err != nil {
		return err
	}
	s.Latest = r.Tag
	dir := filepath.Dir(executable(r.Tag))
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if Download == nil {
		return fmt.Errorf("插件下载器未初始化")
	}
	var last time.Time
	downloaded, err := Download(ctx, a.URL, a.Digest, dir, func(stage, route string, done, total int64) {
		if stage == s.Stage && route == s.Route && time.Since(last) < 150*time.Millisecond && done != total {
			return
		}
		last = time.Now()
		s.Stage = stage
		s.Route = route
		s.Done = done
		s.Total = total
		publish(*s)
	})
	if err != nil {
		return err
	}
	defer os.Remove(downloaded)
	if err = os.Chmod(downloaded, 0700); err != nil {
		return err
	}
	s.Stage = "启动验证"
	s.Done = 0
	s.Total = 0
	publish(*s)
	processMu.Lock()
	defer processMu.Unlock()
	stopLocked()
	dst := executable(r.Tag)
	// Only remove a previously installed same-version binary after verified download.
	if err = os.Remove(dst); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err = os.Rename(downloaded, dst); err != nil {
		return err
	}
	if _, err = startLocked(dst); err != nil {
		return fmt.Errorf("插件启动验证失败：%w", err)
	}
	next := *s
	next.Installed = r.Tag
	next.Phase = "ready"
	b, _ := json.Marshal(next)
	tmp := filepath.Join(directory, "current.tmp")
	if err = os.WriteFile(tmp, b, 0600); err != nil {
		stopLocked()
		return err
	}
	// Windows rename cannot replace an existing file; retain metadata for recovery.
	current := filepath.Join(directory, "current.json")
	backup := current + ".bak"
	_ = os.Remove(backup)
	if err = os.Rename(current, backup); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err = os.Rename(tmp, current); err != nil {
		stopLocked()
		_ = os.Rename(backup, current)
		return err
	}
	s.Installed = r.Tag
	return nil
}

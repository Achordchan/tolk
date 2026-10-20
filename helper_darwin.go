//go:build darwin

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"

	"tolk/internal/doublecopy"
	"tolk/internal/engine"
	"tolk/internal/helper"
	"tolk/internal/legacy"
	"tolk/internal/macmigrate"
)

type helloInfo struct {
	Version      string   `json:"version"`
	Macos        string   `json:"macos"`
	Capabilities []string `json:"capabilities"`
}

var hs struct {
	mu        sync.Mutex
	client    *helper.Client
	hello     helloInfo
	hkRunning bool
	hkError   string
	fire      func(string)
}

func init() {
	legacy.Register("apple", func() (engine.Engine, error) { return &engine.Apple{Call: appleCall}, nil })
}

func current() (*helper.Client, helloInfo) {
	hs.mu.Lock()
	defer hs.mu.Unlock()
	return hs.client, hs.hello
}

func has(h helloInfo, capability string) bool {
	for _, c := range h.Capabilities {
		if c == capability {
			return true
		}
	}
	return false
}

func helperVersion() string {
	c, h := current()
	if c == nil {
		return ""
	}
	return fmt.Sprintf("v%s · macOS %s", h.Version, h.Macos)
}

// findHelper looks beside the executable, in the bundle's Resources, then at
// TOLK_HELPER for development.
func findHelper() string {
	if p := os.Getenv("TOLK_HELPER"); p != "" {
		return p
	}
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	dir := filepath.Dir(exe)
	for _, p := range []string{
		filepath.Join(dir, "tolk-helper"),
		filepath.Join(dir, "..", "Resources", "tolk-helper"),
		filepath.Join(dir, "..", "Resources", "darwin-universal", "tolk-helper"),
	} {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// startHelper runs the helper for the rest of the session, restarting it if
// it dies. fire receives text picked up by the system-wide ⌘C⌘C hotkey.
func startHelper(fire func(string)) {
	hs.mu.Lock()
	hs.fire = fire
	hs.mu.Unlock()
	go superviseHelper()
}

func superviseHelper() {
	path := findHelper()
	if path == "" {
		Logs.Warnf("没有找到原生助手（tolk-helper），截图翻译、系统级快捷键和 Apple 本地翻译不可用")
		return
	}
	backoff := time.Second
	first := true
	for {
		began := time.Now()
		c, err := helper.Start(path, helperHandle, helperEvent, Logs.Infof)
		if err != nil {
			Logs.Errorf("启动原生助手失败：%v", err)
		} else {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			var h helloInfo
			err = c.Call(ctx, "hello", nil, &h)
			cancel()
			if err != nil {
				Logs.Errorf("原生助手握手失败：%v", err)
				c.Close()
			} else {
				hs.mu.Lock()
				hs.client, hs.hello, hs.hkRunning, hs.hkError = c, h, false, ""
				hs.mu.Unlock()
				Logs.Infof("原生助手已就绪 v%s（macOS %s，能力：%s）", h.Version, h.Macos, strings.Join(h.Capabilities, "、"))
				macApplyHotkeys()
				if first {
					first = false
					go firstRun()
				}
				<-c.Done()
				hs.mu.Lock()
				hs.client, hs.hkRunning = nil, false
				hs.mu.Unlock()
				Logs.Warnf("原生助手已退出")
			}
		}
		if time.Since(began) > 30*time.Second {
			backoff = time.Second
		} else if backoff *= 2; backoff > time.Minute {
			Logs.Errorf("原生助手反复退出，已停止重启；重启 Tolk 再试")
			return
		}
		time.Sleep(backoff)
	}
}

// firstRun imports the old Mac app's settings once and shows the permission
// guide when neither permission is granted yet (an upgrade keeps its grants).
func firstRun() {
	if p, err := legacy.DefaultPath(); err == nil {
		if cfg, err := legacy.Load(p); err != nil || !macmigrate.Imported(cfg) {
			if _, err := macImport(false); err != nil {
				Logs.Warnf("导入旧版 Mac 设置：%v", err)
			}
		}
		repaired := false
		if _, err := legacy.Load(p); err != nil {
			// no config yet: nothing to repair
		} else if err := legacy.Update(p, func(m map[string]any) { repaired = macmigrate.Repair(m) }); err != nil {
			Logs.Warnf("修正双击复制设置：%v", err)
		} else if repaired {
			loadPrefs()
			macApplyHotkeys()
			_ = PrefsChanged.Broadcast(currentPrefs())
		}
	}
	st := macStatus()
	if st.Running && !st.Accessibility && !st.ScreenRecording {
		if err := macSimple("permissions.guide", nil); err != nil {
			Logs.Warnf("授权引导：%v", err)
		}
	}
}

func helperHandle(ctx context.Context, op string, params json.RawMessage) (any, error) {
	switch op {
	case "translate":
		var in struct {
			Texts  []string `json:"texts"`
			Source string   `json:"source"`
			Target string   `json:"target"`
		}
		if err := json.Unmarshal(params, &in); err != nil {
			return nil, err
		}
		out := make([]string, len(in.Texts))
		var wg sync.WaitGroup
		errs := make([]error, len(in.Texts))
		sem := make(chan struct{}, 4)
		for i, t := range in.Texts {
			wg.Add(1)
			go func() {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				if strings.TrimSpace(t) == "" {
					return
				}
				r, err := translate(t, in.Source, in.Target)
				out[i], errs[i] = r.Text, err
			}()
		}
		wg.Wait()
		for _, err := range errs {
			if err != nil {
				return nil, err
			}
		}
		return map[string]any{"texts": out}, nil
	}
	return nil, fmt.Errorf("未知请求：%s", op)
}

func helperEvent(name string, raw json.RawMessage) {
	var e struct {
		Name   string `json:"name"`
		Error  string `json:"error"`
		Needs  string `json:"needs"`
		Count  int64  `json:"changeCount"`
		Level  string `json:"level"`
		Msg    string `json:"msg"`
		State  string `json:"state"`
		Mode   string `json:"mode"`
		Text   string `json:"text"`
		Transl string `json:"translated"`
	}
	_ = json.Unmarshal(raw, &e)
	switch name {
	case "hotkey":
		switch e.Name {
		case "doubleCopy":
			if !currentPrefs().DoubleCopy {
				return // the general switch also covers the system-wide hotkey
			}
			count := uint64(e.Count)
			go func() {
				// The second copy must actually have updated the clipboard;
				// otherwise whatever sits there is stale (or private) text.
				if seq, ok := doublecopy.Sequence(); ok {
					deadline := time.Now().Add(700 * time.Millisecond)
					for seq <= count {
						if time.Now().After(deadline) {
							return
						}
						time.Sleep(30 * time.Millisecond)
						seq, _ = doublecopy.Sequence()
					}
				} else {
					time.Sleep(220 * time.Millisecond)
				}
				text := strings.TrimSpace(mygo.Clipboard.ReadText())
				hs.mu.Lock()
				fire := hs.fire
				hs.mu.Unlock()
				if text != "" && fire != nil {
					fire(text)
				}
			}()
		case "screenshot":
			// Not on the reader goroutine: macScreenshot waits for a reply
			// that only this reader can deliver.
			go func() {
				if err := macScreenshot("translate"); err != nil {
					Logs.Warnf("截图翻译：%v", err)
					_ = MacNotice.Broadcast(err.Error())
				}
			}()
		}
	case "hotkey.stopped":
		hs.mu.Lock()
		hs.hkRunning, hs.hkError = false, e.Error
		hs.mu.Unlock()
		Logs.Warnf("系统级快捷键已停止：%s，已退回到剪贴板检测", e.Error)
		_ = MacNotice.Broadcast("系统级快捷键已停止：" + e.Error)
	case "screenshot.error":
		Logs.Warnf("截图翻译：%s", e.Error)
		_ = MacNotice.Broadcast(e.Error)
	case "screenshot.done":
		Logs.Infof("截图翻译完成（%s，%d 字）", e.Mode, len([]rune(e.Text)))
	case "log":
		Logs.Infof("助手[%s] %s", e.Level, e.Msg)
	}
}

func hotkeyActive() bool {
	hs.mu.Lock()
	defer hs.mu.Unlock()
	return hs.hkRunning && currentPrefs().SystemHotkey
}

func macApplyHotkeys() {
	c, _ := current()
	if c == nil {
		return
	}
	p := loadPrefs()
	ctx, cancel := macCtx()
	defer cancel()
	var res struct {
		Running bool   `json:"running"`
		Error   string `json:"error"`
	}
	err := c.Call(ctx, "hotkeys.configure", map[string]any{
		"doubleCopy": p.SystemHotkey && p.DoubleCopy, "screenshot": p.ScreenshotHotkey,
		"windowMs": p.DoubleCopyMs, "screenshotKeyCode": p.ScreenshotKeyCode,
	}, &res)
	hs.mu.Lock()
	hs.hkRunning, hs.hkError = err == nil && res.Running, res.Error
	if err != nil {
		hs.hkError = err.Error()
	}
	hs.mu.Unlock()
	if err != nil {
		Logs.Warnf("设置系统级快捷键失败：%v", err)
	} else if (p.SystemHotkey || p.ScreenshotHotkey) && !res.Running {
		Logs.Warnf("系统级快捷键没有启动：%s", res.Error)
	}
}

func macStatus() MacStatus {
	c, h := current()
	st := MacStatus{Available: findHelper() != ""}
	if c == nil {
		return st
	}
	st.Running, st.Version, st.Capabilities = true, h.Version, h.Capabilities
	var perms struct {
		Accessibility   bool `json:"accessibility"`
		ScreenRecording bool `json:"screenRecording"`
	}
	ctx, cancel := macCtx()
	defer cancel()
	if err := c.Call(ctx, "permissions", nil, &perms); err == nil {
		st.Accessibility, st.ScreenRecording = perms.Accessibility, perms.ScreenRecording
	}
	hs.mu.Lock()
	st.HotkeyRunning, st.HotkeyError = hs.hkRunning, hs.hkError
	hs.mu.Unlock()
	return st
}

func macSimple(op string, params map[string]any) error {
	c, _ := current()
	if c == nil {
		return errors.New("原生助手没有运行")
	}
	ctx, cancel := macCtx()
	defer cancel()
	return c.Call(ctx, op, params, nil)
}

func macScreenshot(mode string) error {
	p := currentPrefs()
	return macSimple("screenshot.start", map[string]any{"source": p.SourceLang, "target": p.TargetLang, "mode": mode})
}

func appleCall(ctx context.Context, texts []string, source, target string) ([]string, error) {
	c, h := current()
	if c == nil {
		return nil, errors.New("原生助手没有运行，无法使用 Apple 本地翻译")
	}
	if !has(h, "apple") {
		return nil, errors.New("Apple 本地翻译需要 macOS 15.1 或更高版本，请在设置里换一个翻译服务")
	}
	var out struct {
		Texts []string `json:"texts"`
	}
	if err := c.Call(ctx, "apple.translate", map[string]any{"texts": texts, "source": source, "target": target}, &out); err != nil {
		return nil, err
	}
	return out.Texts, nil
}

// macImport reads the old app's data through the helper and merges it into
// the shared config. manual calls may ask for the helper; the automatic one
// quietly skips when it is not running.
func macImport(manual bool) ([]string, error) {
	c, _ := current()
	if c == nil {
		return nil, errors.New("原生助手没有运行")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second) // a Keychain prompt may be waiting
	defer cancel()
	var data macmigrate.Data
	if err := c.Call(ctx, "legacy.read", nil, &data); err != nil {
		return nil, err
	}
	p, err := legacy.DefaultPath()
	if err != nil {
		return nil, err
	}
	var notes []string
	if err := legacy.Update(p, func(m map[string]any) { notes = macmigrate.Apply(m, data) }); err != nil {
		return nil, err
	}
	loadPrefs()
	macApplyHotkeys() // the imported preferences may enable the system hotkeys
	Logs.Infof("已导入旧版 Mac 设置（%d 项设置，%d 个 API Key）", len(data.Defaults), len(data.Keys))
	for _, n := range notes {
		Logs.Warnf("%s", n)
	}
	_ = PrefsChanged.Broadcast(currentPrefs())
	return notes, nil
}

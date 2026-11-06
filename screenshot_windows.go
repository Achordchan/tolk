//go:build windows

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"tolk/internal/engine"
	"tolk/internal/legacy"
	"tolk/internal/screenshot"
)

var captureBusy atomic.Bool

// warmTimeout bounds how long a screenshot waits for its PowerShell to be
// ready. The first run after an update compiles the controls, which on a
// slow virtual machine can take a while.
const warmTimeout = 90 * time.Second

// shotDLL is where the workspace keeps its compiled controls ("" compiles
// in memory every time, as before). The folder is swept once per run.
var shotDLL = sync.OnceValue(func() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	dll, err := screenshot.PrepareCache(filepath.Join(dir, "Tolk", "screenshot"))
	if err != nil {
		Logs.Warnf("截图控件缓存不可用，每次都会重新编译：%v", err)
		return ""
	}
	return dll
})

// The spare is a workspace started ahead of time and waiting for Begin, so
// a screenshot does not pay for PowerShell starting and loading the
// controls. There is at most one. It is started a few seconds after launch
// when the shortcut is on, and again each time a screenshot has opened. A
// spare that fails is not replaced until the next screenshot, which then
// starts cold and reports the real error, so a broken PowerShell cannot
// make Tolk relaunch it in a loop.
var shotSpare struct {
	sync.Mutex
	p        *screenshot.Process
	quitHook bool
	quitting bool
}

func typesLabel(w screenshot.Warm) string {
	switch w.Types {
	case "cache":
		return fmt.Sprintf("控件读自缓存，%d 毫秒", w.TypesMs)
	case "compiled":
		return fmt.Sprintf("控件首次编译并已缓存，%d 毫秒", w.TypesMs)
	default:
		return fmt.Sprintf("控件在内存中编译（缓存不可用），%d 毫秒", w.TypesMs)
	}
}

// prewarmScreenshot starts the spare. forShortcut is for the starts made
// on the shortcut's behalf; it is checked under the same lock that turning
// the shortcut off takes, so a delayed start cannot outlive that.
func prewarmScreenshot(forShortcut bool) {
	shotSpare.Lock()
	defer shotSpare.Unlock()
	if shotSpare.p != nil || shotSpare.quitting || forShortcut && !shotEnabled.Load() {
		return
	}
	if !shotSpare.quitHook {
		shotSpare.quitHook = true
		mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) {
			shotSpare.Lock()
			shotSpare.quitting = true
			shotSpare.Unlock()
			discardScreenshotSpare()
		})
	}
	p, err := screenshot.Launch(shotDLL())
	if err != nil {
		Logs.Warnf("截图预热失败：%v", err)
		return
	}
	shotSpare.p = p
	go func() {
		w, err := p.WaitWarm(warmTimeout)
		if err != nil {
			// Clean up only while it is still the spare. Once a screenshot
			// has taken it, that screenshot waits with its own deadline and
			// owns the process, including killing it.
			shotSpare.Lock()
			owned := shotSpare.p == p
			if owned {
				shotSpare.p = nil
			}
			shotSpare.Unlock()
			if owned {
				Logs.Warnf("截图预热失败：%v %s", err, p.Stderr())
				p.Kill()
				_ = p.Wait()
			}
			return
		}
		Logs.Infof("截图工作区已预热，用时 %d 毫秒（%s）", time.Since(p.Started()).Milliseconds(), typesLabel(w))
	}()
}

// takeScreenshotSpare hands over the spare if it is still running.
func takeScreenshotSpare() *screenshot.Process {
	shotSpare.Lock()
	p := shotSpare.p
	shotSpare.p = nil
	shotSpare.Unlock()
	if p != nil && !p.Alive() {
		_ = p.Wait()
		return nil
	}
	return p
}

// discardScreenshotSpare ends the waiting spare: the shortcut was turned
// off, or Tolk is quitting.
func discardScreenshotSpare() {
	if p := takeScreenshotSpare(); p != nil {
		p.Kill()
		_ = p.Wait() // prompt once killed; also removes its temporary script
	}
}

func (Tolk) ScreenshotWindows() error {
	if !captureBusy.CompareAndSwap(false, true) {
		return fmt.Errorf("截图正在进行中，请完成框选或按 Esc 取消")
	}
	var released sync.Once
	release := func() { released.Do(func() { captureBusy.Store(false) }) }
	defer release()
	started := time.Now()
	Logs.Infof("截图请求已接收，正在启动选区")
	failed := func(p *screenshot.Process, err error) error {
		p.Kill()
		_ = p.Wait()
		Logs.Errorf("Windows 截图启动失败：%v %s", err, p.Stderr())
		return fmt.Errorf("截图识别失败，请确认 Windows 已安装 OCR 语言包且允许运行 PowerShell；详细原因见运行日志")
	}
	how := "预热进程"
	p := takeScreenshotSpare()
	var w screenshot.Warm
	var err error
	if p != nil {
		if w, err = p.WaitWarm(warmTimeout); err != nil {
			Logs.Warnf("预热的截图进程不可用，改为现在启动：%v %s", err, p.Stderr())
			p.Kill()
			_ = p.Wait()
			p = nil
		}
	}
	if p == nil {
		how = "冷启动"
		if p, err = screenshot.Launch(shotDLL()); err != nil {
			return err
		}
		if w, err = p.WaitWarm(warmTimeout); err != nil {
			return failed(p, err)
		}
	}
	var quitting atomic.Bool
	off := mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { quitting.Store(true); p.Kill() })
	defer off()
	prefs := loadPrefs()
	if err = p.Begin(screenshot.Start{Target: prefs.TargetLang, Theme: prefs.Theme, Languages: engine.Languages}); err != nil {
		return failed(p, err)
	}
	protocolErr := screenshot.Serve(p.Output(), p.Input(), func(requestCtx context.Context, text, source, target string) (string, error) {
		path, e := legacy.DefaultPath()
		if e != nil {
			return "", e
		}
		cfg, e := legacy.Load(path)
		if e != nil {
			if !os.IsNotExist(e) {
				return "", fmt.Errorf("无法读取翻译服务配置：%w", e)
			}
			cfg = map[string]any{}
		}
		api := legacy.APIName(cfg)
		eng, _, e := engineFor(cfg, api)
		if e != nil {
			return "", e
		}
		r, e := runEngineContext(requestCtx, eng, api, text, source, target)
		return r.Text, e
	}, release, func() {
		Logs.Infof("截图选区已就绪，启动耗时 %d 毫秒（%s，%s）", time.Since(started).Milliseconds(), how, typesLabel(w))
		// The next screenshot gets its own spare; this one now belongs to
		// the selection and to any pin made from it.
		go prewarmScreenshot(false)
	})
	if protocolErr != nil {
		p.Kill()
	}
	err = p.Wait()
	if protocolErr != nil {
		return fmt.Errorf("截图工作区通信失败：%w", protocolErr)
	}
	if err != nil {
		if quitting.Load() {
			return nil
		}
		Logs.Errorf("Windows 截图识别失败：%v %s", err, p.Stderr())
		return fmt.Errorf("截图识别失败，请确认 Windows 已安装 OCR 语言包且允许运行 PowerShell；详细原因见运行日志")
	}
	return nil
}

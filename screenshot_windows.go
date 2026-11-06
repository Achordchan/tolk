//go:build windows

package main

import (
	"context"
	"fmt"
	"path/filepath"

	"encoding/json"
	"github.com/egoist/mygo"
	"golang.org/x/sys/windows"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"tolk/internal/engine"
	"tolk/internal/legacy"
	"tolk/internal/screenshot"
	"unsafe"
)

var captureBusy atomic.Bool

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

func (Tolk) ScreenshotWindows() error {
	if !captureBusy.CompareAndSwap(false, true) {
		return fmt.Errorf("截图正在进行中，请完成框选或按 Esc 取消")
	}
	var released sync.Once
	release := func() { released.Do(func() { captureBusy.Store(false) }) }
	defer release()
	started := time.Now()
	Logs.Infof("截图请求已接收，正在启动选区")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	off := mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { cancel() })
	defer off()
	file, err := os.CreateTemp("", "tolk-screenshot-*.ps1")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.WriteString(screenshot.Script()); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-STA", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", file.Name())
	// Hide the console, not the WinForms selection surface.
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	prefs := loadPrefs()
	languages, _ := json.Marshal(engine.Languages)
	cmd.Env = append(os.Environ(), "TOLK_SHOT_TARGET="+prefs.TargetLang, "TOLK_SHOT_LANGUAGES="+string(languages), "TOLK_SHOT_THEME="+prefs.Theme, "TOLK_SHOT_DLL="+shotDLL())
	output, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	input, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	defer input.Close()
	// Closing the job (including on a main-process crash) terminates the workspace.
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(job)
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		return err
	}
	if err = cmd.Start(); err != nil {
		return err
	}
	handle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return err
	}
	err = windows.AssignProcessToJobObject(job, handle)
	windows.CloseHandle(handle)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return err
	}
	protocolErr := screenshot.Serve(output, input, func(requestCtx context.Context, text, source, target string) (string, error) {
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
		Logs.Infof("截图选区已就绪，启动耗时 %d 毫秒", time.Since(started).Milliseconds())
	})
	if protocolErr != nil {
		_ = cmd.Process.Kill()
	}
	err = cmd.Wait()
	if protocolErr != nil {
		return fmt.Errorf("截图工作区通信失败：%w", protocolErr)
	}
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		Logs.Errorf("Windows 截图识别失败：%v %s", err, stderr.String())
		return fmt.Errorf("截图识别失败，请确认 Windows 已安装 OCR 语言包且允许运行 PowerShell；详细原因见运行日志")
	}
	return nil
}

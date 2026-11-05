// tolk-watch is a lightweight launcher and crash uploader with no UI dependencies.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"tolk/internal/legacy"
	"tolk/internal/report"
	"tolk/internal/reportqueue"
)

var version = "unknown"

type capture struct {
	mu        sync.Mutex
	body      string
	headline  string
	crashHead string
	partial   string
	path      string
}

func (c *capture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	combined := c.body + string(p)
	if c.headline == "" {
		c.partial += string(p)
		for {
			i := strings.IndexByte(c.partial, '\n')
			if i < 0 {
				break
			}
			line := c.partial[:i]
			c.partial = c.partial[i+1:]
			if h := crashDiagnostic(line, ""); h != "" {
				c.headline = h
				c.crashHead = report.Head(line+"\n"+c.partial, 24000)
				break
			}
		}
		c.partial = report.Tail(c.partial, 24000)
	} else if len(c.crashHead) < 24000 {
		c.crashHead = report.Head(c.crashHead+string(p), 24000)
	}
	c.body = report.Tail(combined, 64<<10)
	// Keep a bounded, sanitized startup trace even if the child dies immediately.
	r := report.Report{Attachments: []report.Attachment{{Name: "启动日志", Body: c.body}}}.Clean()
	if len(r.Attachments) > 0 {
		_ = os.WriteFile(c.path, []byte(r.Attachments[0].Body), 0600)
	}
	return len(p), nil
}
func main() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	if _, err = legacy.MigrateHome(); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	home, err := legacy.Home()
	if err != nil {
		return
	}
	dir := filepath.Join(home, "update_state")
	if err = os.MkdirAll(dir, 0700); err != nil {
		launchWithoutMonitoring(exe, err)
		return
	}
	idBytes := make([]byte, 16)
	if _, err = rand.Read(idBytes); err != nil {
		launchWithoutMonitoring(exe, err)
		return
	}
	id := hex.EncodeToString(idBytes)
	logPath := filepath.Join(dir, "startup-"+id+".log")
	logs := &capture{path: logPath}
	// Initialize consent before the child starts, without overwriting an existing preference.
	store := report.NewStore(dir)
	st := store.Get()
	child := filepath.Join(filepath.Dir(exe), "Tolk-core.exe")
	if runtime.GOOS != "windows" {
		child = filepath.Join(filepath.Dir(exe), "Tolk")
	}
	cmd := exec.Command(child, os.Args[1:]...)
	cmd.Env = append(os.Environ(), "TOLK_LAUNCHER_PATH="+exe, "TOLK_SUPERVISOR_PID="+fmt.Sprint(os.Getpid()), "TOLK_SUPERVISED=1", "TOLK_HOME="+home)
	cmd.Stdout = logs
	cmd.Stderr = logs
	prepare(cmd)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		// A bounded retry scheduler is needed when offline: no server connection exists
		// on which SSE or WebSocket could signal recovery.
		delay := time.Second
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
			retryCrashes(dir)
			reportqueue.Flush(ctx, dir)
			if delay < time.Minute {
				delay *= 2
			}
		}
	}()
	err = cmd.Start()
	if err == nil {
		allowForeground(cmd)
		err = cmd.Wait()
	}
	if err != nil {
		logs.mu.Lock()
		body := logs.body
		headline := logs.headline
		if headline == "" {
			headline = crashDiagnostic(logs.partial, "")
		}
		if logs.crashHead != "" {
			body = logs.crashHead + "\n--- recent output ---\n" + body
		}
		logs.mu.Unlock()
		current := report.State{AutoDisabled: true}
		if b, e := os.ReadFile(filepath.Join(dir, "report_state.json")); e == nil {
			_ = json.Unmarshal(b, &current)
		} else {
			current.AutoDisabled = true
		}
		if !current.AutoDisabled {
			if headline == "" {
				headline = crashDiagnostic(body, "")
			}
			title, description, source := exitSummary(err, headline)
			r := report.Report{Kind: report.Crash, Title: title, Description: description, InstallID: st.InstallID, SubmissionID: id, Version: version, OS: runtime.GOOS, Arch: runtime.GOARCH, Attachments: []report.Attachment{{Name: "启动与退出日志", Body: body, KeepHead: true}}}
			r.Fingerprint = report.Fingerprint(report.Crash, source)
			if e := reserveCrash(dir, r); e != nil {
				fmt.Fprintln(os.Stderr, e)
			}
		}
	}
	// A clean exit must release the launcher executable for the updater.
	if err == nil {
		_ = os.Remove(logPath)
		return
	}
	// Do not require another application launch to deliver a startup crash.
	cancel() // stop the active-run worker before bounded post-exit delivery
	delivery, stopDelivery := context.WithTimeout(context.Background(), 10*time.Minute)
	defer stopDelivery()
	for delivery.Err() == nil {
		retryCrashes(dir)
		sources, _ := filepath.Glob(filepath.Join(dir, "crash-source-*.json"))
		if !reportqueue.Enabled(dir) || (reportqueue.Flush(delivery, dir) && len(sources) == 0) {
			break
		}
		select {
		case <-delivery.Done():
			return
		case <-time.After(30 * time.Second):
		}
	}
	if err == nil {
		_ = os.Remove(logPath)
	}
}

// exitSummary words the report of a failed run. Exiting without a crash headline
// is not evidence of a crash: a window closed after "not responding" and a
// forced end from Task Manager look the same from here. source is what the
// report is fingerprinted by.
func exitSummary(err error, headline string) (title, description, source string) {
	failure := fmt.Sprintf("主程序启动或运行失败：%v", err)
	if headline != "" {
		return "[崩溃] 主程序异常退出", failure + "\n" + headline, headline
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return "[异常退出] 主程序没有留下崩溃信息", failure + "\n没有崩溃信息：可能是界面未响应后被关闭、被强制结束，或原生代码崩溃。", failure
	}
	return "[崩溃] 主程序无法启动", failure, failure
}

// Use the panic/fatal headline rather than generic exit status or volatile logs.
func crashDiagnostic(body, fallback string) string {
	for _, line := range strings.Split(body, "\n") {
		for _, prefix := range []string{"panic:", "fatal error:", "Exception "} {
			if i := strings.Index(line, prefix); i >= 0 {
				return line[i:]
			}
		}
	}
	return fallback
}

func launchWithoutMonitoring(exe string, cause error) {
	child := filepath.Join(filepath.Dir(exe), "Tolk-core.exe")
	cmd := exec.Command(child, os.Args[1:]...)
	cmd.Env = append(os.Environ(), "TOLK_MONITOR_ERROR="+cause.Error())
	prepare(cmd)
	if cmd.Start() == nil {
		allowForeground(cmd)
		_ = cmd.Wait()
	}
}

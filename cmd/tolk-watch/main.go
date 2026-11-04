// tolk-watch is a lightweight launcher and crash uploader with no UI dependencies.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
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
	mu       sync.Mutex
	body     string
	headline string
	path     string
}

func (c *capture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	combined := c.body + string(p)
	if c.headline == "" {
		c.headline = crashDiagnostic(combined, "")
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
		return
	}
	idBytes := make([]byte, 16)
	if _, err = rand.Read(idBytes); err != nil {
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
		logs.mu.Unlock()
		current := report.State{AutoDisabled: true}
		if b, e := os.ReadFile(filepath.Join(dir, "report_state.json")); e == nil {
			_ = json.Unmarshal(b, &current)
		} else {
			current.AutoDisabled = true
		}
		if !current.AutoDisabled {
			r := report.Report{Kind: report.Crash, Title: "[崩溃] 主程序异常退出", Description: fmt.Sprintf("主程序启动或运行失败：%v", err), InstallID: st.InstallID, SubmissionID: id, Version: version, OS: runtime.GOOS, Arch: runtime.GOARCH, Attachments: []report.Attachment{{Name: "启动与退出日志", Body: body, KeepHead: true}}}
			if headline == "" {
				headline = crashDiagnostic(body, r.Description)
			}
			r.Fingerprint = report.Fingerprint(report.Crash, headline)
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

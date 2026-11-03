package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo"

	"tolk/internal/logbuf"
	"tolk/internal/report"
	"tolk/internal/update"
)

// 提交问题 and the automatic error reports. Reports go to a relay
// (report-worker/) that files them as GitHub issues; the GitHub token lives
// there, never in the app. Everything is scrubbed of keys, e-mail addresses
// and account names before it leaves the machine.

var fb struct {
	store *report.Store
	crash string // what the previous run's crash output held

	mu    sync.Mutex
	first *logbuf.Entry // first error of the burst being collected
}

const crashFile = "crash.log"

func init() {
	if testing.Testing() {
		return // never report from go test
	}
	dir := ""
	if dirs, err := update.DefaultDirs(); err == nil {
		dir = dirs.State
	} else {
		dir = filepath.Join(os.TempDir(), "tolk")
	}
	fb.store = report.NewStore(dir)
	setupCrashOutput(dir)
	Logs.OnError(onErrorLine)
	go func() {
		// Leave the network time to come up after login.
		time.Sleep(20 * time.Second)
		reportPreviousRun()
	}()
}

// setupCrashOutput keeps what the previous run's crash output held, then
// sends this run's Go crash report (an unrecovered panic or fatal error) to
// the same file. Without it a crash in the Windows GUI build left no trace.
func setupCrashOutput(dir string) {
	path := filepath.Join(dir, crashFile)
	if raw, err := os.ReadFile(path); err == nil {
		if s := string(raw); strings.Contains(s, "panic:") || strings.Contains(s, "fatal error:") {
			fb.crash = s
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	f, err := os.Create(path)
	if err != nil {
		return
	}
	if err := debug.SetCrashOutput(f, debug.CrashOptions{}); err != nil {
		f.Close()
	}
	// f stays open for the life of the process; the runtime writes to it.
}

// ---- the 提交问题 dialog ----

// FeedbackInfo is what the dialog shows before anything is sent.
type FeedbackInfo struct {
	AutoReport bool   `json:"autoReport"`
	InstallID  string `json:"installId"`
}

// FeedbackInfo returns the auto-report switch and the anonymous install id.
func (Tolk) FeedbackInfo() FeedbackInfo {
	st := fb.store.Get()
	return FeedbackInfo{AutoReport: !st.AutoDisabled, InstallID: st.InstallID}
}

// SetAutoReport turns the automatic error reports on or off.
func (Tolk) SetAutoReport(on bool) error {
	_, err := fb.store.Update(func(st *report.State) { st.AutoDisabled = !on })
	if err == nil {
		Logs.Infof("自动上传错误报告已%s", map[bool]string{true: "开启", false: "关闭"}[on])
	}
	return err
}

// FeedbackPreview is exactly the text that SubmitFeedback would send.
func (Tolk) FeedbackPreview(description, contact string, withLogs bool) string {
	return manualReport(description, contact, withLogs).Markdown()
}

// FeedbackResult tells the dialog how the report went.
type FeedbackResult struct {
	// Sent: the relay filed it; URL is the issue.
	Sent   bool   `json:"sent"`
	URL    string `json:"url"`
	Number int    `json:"number"`
	// When the relay could not be reached, the full report is on the
	// clipboard and IssueURL opens GitHub's new-issue page filled in.
	IssueURL string `json:"issueUrl"`
	Error    string `json:"error"`
}

// SubmitFeedback sends a report from the 提交问题 dialog.
func (Tolk) SubmitFeedback(description, contact string, withLogs bool) FeedbackResult {
	if strings.TrimSpace(description) == "" {
		return FeedbackResult{Error: "请先描述一下遇到的问题"}
	}
	r := manualReport(description, contact, withLogs)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	res, err := report.Send(ctx, nil, r)
	if err == nil && res.Skipped == "" {
		Logs.Infof("问题已提交：%s", res.URL)
		return FeedbackResult{Sent: true, URL: res.URL, Number: res.Number}
	}
	msg := res.Skipped
	if err != nil {
		msg = err.Error()
	}
	Logs.Warnf("提交问题没有成功（%s），改为在 GitHub 上提交", msg)
	mygo.Clipboard.WriteText(r.Markdown())
	return FeedbackResult{IssueURL: report.IssueURL(report.Repo, r), Error: msg}
}

// OpenLink opens an http(s) link in the default browser.
func (Tolk) OpenLink(url string) error {
	if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://") {
		return fmt.Errorf("不支持的链接")
	}
	return mygo.Shell.OpenExternal(url)
}

func manualReport(description, contact string, withLogs bool) report.Report {
	title := strings.TrimSpace(description)
	if i := strings.IndexAny(title, "\r\n"); i >= 0 {
		title = title[:i]
	}
	r := baseReport(report.Manual, "[反馈] "+title, description, withLogs)
	r.Contact = contact
	return r.Clean()
}

// ---- automatic reports ----

// Errors that say the network or a service is down are the user's weather,
// not a bug; reporting them would only flood the tracker.
var weather = []string{
	"timeout", "timed out", "no such host", "connection refused", "connection reset",
	"network is unreachable", "tls handshake", "eof", "i/o timeout", "context deadline",
	"context canceled", "429", "rate limit", "超时", "网络", "无法连接",
}

func ignorable(text string) bool {
	l := strings.ToLower(text)
	for _, w := range weather {
		if strings.Contains(l, w) {
			return true
		}
	}
	return false
}

// onErrorLine collects a burst of error lines for a few seconds and reports
// the first one, with the log around it.
func onErrorLine(e logbuf.Entry) {
	if ignorable(e.Text) || strings.Contains(e.Text, "错误报告") {
		return
	}
	fb.mu.Lock()
	defer fb.mu.Unlock()
	if fb.first != nil {
		return
	}
	fb.first = &e
	time.AfterFunc(8*time.Second, func() {
		fb.mu.Lock()
		first := fb.first
		fb.first = nil
		fb.mu.Unlock()
		if first != nil {
			sendAuto(report.Error, "[自动] "+first.Text, "运行中记录到错误：\n\n"+first.Text, first.Text, nil)
		}
	})
}

// reportPreviousRun reports a crash of the previous run and an update the
// update script says failed, once each.
func reportPreviousRun() {
	if fb.crash != "" {
		head := crashHead(fb.crash)
		sendAuto(report.Crash, "[崩溃] "+head, "上一次运行异常退出：\n\n"+head, head,
			[]report.Attachment{{Name: "崩溃信息", Body: fb.crash}})
		fb.crash = ""
	}
	dirs, err := update.DefaultDirs()
	if err != nil {
		return
	}
	path := filepath.Join(dirs.State, "last_update_result.json")
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	raw, _ := os.ReadFile(path)
	var res struct {
		Status   string `json:"status"`
		Message  string `json:"message"`
		Expected string `json:"expected_version"`
	}
	if json.Unmarshal(bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf")), &res) != nil || res.Status != "failed" {
		return
	}
	stamp := info.ModTime().UTC().Format(time.RFC3339)
	if fb.store.Get().UpdateResultAt == stamp {
		return
	}
	_, _ = fb.store.Update(func(st *report.State) { st.UpdateResultAt = stamp })
	sendAuto(report.Update, fmt.Sprintf("[更新失败] v%s → v%s：%s", Version, res.Expected, res.Message),
		fmt.Sprintf("更新到 v%s 失败：%s", res.Expected, res.Message), res.Message, nil)
}

// crashHead is the "panic: …" line and the first Tolk frame below it.
func crashHead(s string) string {
	lines := strings.Split(s, "\n")
	head := ""
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if head == "" && (strings.HasPrefix(l, "panic:") || strings.HasPrefix(l, "fatal error:")) {
			head = l
			continue
		}
		if head != "" && (strings.HasPrefix(l, "main.") || strings.HasPrefix(l, "tolk/")) {
			return head + " @ " + l
		}
	}
	if head == "" {
		head = "程序崩溃"
	}
	return head
}

func sendAuto(kind, title, description, fpText string, extra []report.Attachment) {
	fp := report.Fingerprint(kind, fpText)
	if !fb.store.Reserve(fp, time.Now()) {
		return
	}
	r := baseReport(kind, title, description, true)
	r.Fingerprint = fp
	r.Attachments = append(extra, r.Attachments...)
	r = r.Clean()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		res, err := report.Send(ctx, nil, r)
		switch {
		case err != nil:
			// Warn, never error: an error line would report itself.
			Logs.Warnf("自动上传错误报告没有成功：%v", err)
		case res.Skipped != "":
			Logs.Infof("错误报告未上传：%s", res.Skipped)
		default:
			Logs.Infof("已自动上传错误报告：%s", res.URL)
		}
	}()
}

// ---- what goes into every report ----

func baseReport(kind, title, description string, withLogs bool) report.Report {
	st := fb.store.Get()
	r := report.Report{
		Kind: kind, Title: title, Description: description,
		InstallID: st.InstallID, Version: Version, OS: runtime.GOOS, Arch: runtime.GOARCH,
	}
	add := func(name, value string) {
		if value != "" {
			r.Env = append(r.Env, report.Field{Name: name, Value: value})
		}
	}
	add("系统版本", report.OSVersion())
	if s, err := (Tolk{}).GetSettings(); err == nil {
		engine := s.Engine
		if p, ok := s.Profiles[s.Vendor]; ok && engine == "openai_compat" {
			engine += fmt.Sprintf("（%s %s）", s.Vendor, p.Model)
		}
		add("翻译引擎", engine)
		p := s.Prefs
		add("语言", p.SourceLang+" → "+p.TargetLang)
		add("设置", fmt.Sprintf("小窗 %v · 双击复制 %v(%dms) · 主题 %s", p.MiniMode, p.DoubleCopy, p.DoubleCopyMs, p.Theme))
	}
	add("原生助手", helperVersion())
	if exe, err := os.Executable(); err == nil {
		add("程序位置", exe)
	}
	add("Go", runtime.Version())
	if !withLogs {
		return r
	}
	var b strings.Builder
	entries := Logs.Since(0)
	if len(entries) > 400 {
		entries = entries[len(entries)-400:]
	}
	for _, e := range entries {
		fmt.Fprintf(&b, "%s [%s] %s\n", time.UnixMilli(e.Time).Format("01-02 15:04:05"), e.Level, e.Text)
	}
	r.Attachments = append(r.Attachments, report.Attachment{Name: "运行日志（最近 400 行）", Body: b.String()})
	if dirs, err := update.DefaultDirs(); err == nil {
		if raw, err := os.ReadFile(filepath.Join(dirs.State, "update.log")); err == nil {
			r.Attachments = append(r.Attachments, report.Attachment{Name: "更新日志 update.log", Body: report.Tail(string(raw), 12000)})
		}
		if raw, err := os.ReadFile(filepath.Join(dirs.State, "last_update_result.json")); err == nil {
			r.Attachments = append(r.Attachments, report.Attachment{Name: "上次更新结果", Body: string(bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf")))})
		}
	}
	return r
}

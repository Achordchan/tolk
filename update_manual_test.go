package main

import (
	"runtime"
	"strings"
	"testing"

	"tolk/internal/logbuf"
	"tolk/internal/update"
)

// The test binary sits in a temporary folder, which is just what a copy that
// cannot update itself looks like (#47): not installed by the installer on
// Windows, outside an .app bundle on macOS.
func TestDownloadUpdateOfCopyThatCannotInstall(t *testing.T) {
	if runtime.GOOS != "windows" && runtime.GOOS != "darwin" {
		t.Skip("updates only run on Windows and macOS")
	}
	var errs []string
	Logs.OnError(func(e logbuf.Entry) { errs = append(errs, e.Text) })
	defer Logs.OnError(nil)
	since := Logs.Since(0)
	mark := int64(0)
	if len(since) > 0 {
		mark = since[len(since)-1].ID
	}

	downloadUpdate(&update.Info{Version: "9.9.9", Notes: "说明"}, false)

	s := (Tolk{}).UpdateStatus()
	if s.State != "manual" || s.Version != "9.9.9" || s.Notes != "说明" || s.Download != installerURL(runtime.GOOS) {
		t.Fatalf("state %+v, want manual with the installer link", s)
	}
	if !strings.Contains(s.Error, "无法自动更新") {
		t.Fatalf("error %q should say why", s.Error)
	}
	if len(errs) > 0 {
		t.Fatalf("logged as errors (these are reported automatically): %v", errs)
	}
	for _, e := range Logs.Since(mark) {
		if strings.Contains(e.Text, "开始下载") {
			t.Fatalf("started a download it could not install: %q", e.Text)
		}
	}
}

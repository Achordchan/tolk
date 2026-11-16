package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

// An update that cannot go in on quit because the copy needs the installer
// leaves no failed result behind: the next launch would report that
// automatically (reportPreviousRun).
func TestWriteUpdateFailureSkipsCopiesThatNeedTheInstaller(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TOLK_HOME", home)
	result := filepath.Join(home, "update_state", "last_update_result.json")

	writeUpdateFailure("9.9.9", fmt.Errorf("%w：安装目录里有不属于 Tolk 的文件", update.ErrCannotInstall))
	if _, err := os.Stat(result); !os.IsNotExist(err) {
		t.Fatalf("a manual-only update left %s behind (%v)", result, err)
	}

	writeUpdateFailure("9.9.9", errors.New("磁盘已满"))
	if _, err := os.Stat(result); err != nil {
		t.Fatalf("a real failure must still be recorded: %v", err)
	}
}

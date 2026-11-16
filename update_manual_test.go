package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

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

// Reopening Tolk after a quit (relaunchApp) is decided in the quit handler,
// from the one call that claims the swap, so only one thing opens the app:
// the swap script when a swap is (or already was) under way and reopens,
// otherwise a wait-and-open.
func TestQuitHandlerLetsOneThingReopen(t *testing.T) {
	const app = "/Applications/Tolk.app"
	cases := []struct {
		name       string
		reopen     string
		out        swapOutcome
		reopens    bool
		wantSwap   []bool // relaunch passed to swap
		wantReopen []string
	}{
		{"plain quit, nothing to put in", "", swapNone, false, []bool{false}, nil},
		{"plain quit puts the update in, no reopen", "", swapStarted, false, []bool{false}, nil},
		{"reopen with an update: the swap script reopens", app, swapStarted, true, []bool{true}, nil},
		{"reopen while 重启更新 runs: that swap reopens", app, swapBusy, true, []bool{true}, nil},
		{"reopen while a swap that does not reopen runs: no racing open", app, swapBusy, false, []bool{true}, nil},
		{"reopen without an update: wait-and-open", app, swapNone, false, []bool{true}, []string{app}},
		{"reopen when the update could not start: wait-and-open", app, swapFailed, false, []bool{true}, []string{app}},
	}
	for _, c := range cases {
		reopenAfterQuit(c.reopen)
		var swaps []bool
		var reopens []string
		quitHandler(
			func(relaunch, quitting bool) (swapOutcome, bool) {
				if !quitting {
					t.Errorf("%s: the quit handler swaps as quitting", c.name)
				}
				swaps = append(swaps, relaunch)
				return c.out, c.reopens
			},
			func(app string) error { reopens = append(reopens, app); return nil },
		)
		if len(swaps) != len(c.wantSwap) || fmt.Sprint(swaps) != fmt.Sprint(c.wantSwap) ||
			len(reopens) != len(c.wantReopen) || fmt.Sprint(reopens) != fmt.Sprint(c.wantReopen) {
			t.Errorf("%s: swaps %v reopens %v, want %v %v", c.name, swaps, reopens, c.wantSwap, c.wantReopen)
		}
		upd.mu.Lock()
		left := upd.reopen
		upd.mu.Unlock()
		if left != "" {
			t.Errorf("%s: the reopen request outlived the quit", c.name)
		}
	}
}

// startSwap claims the swap and reports which case it is in under one lock,
// and once the quit handler has asked, nothing starts a swap any more: a
// download finishing during the quit cannot slip in after the decision.
func TestStartSwapClaimsAndCloses(t *testing.T) {
	upd.mu.Lock()
	pending, applied, reopens, closed := upd.pending, upd.applied, upd.reopens, upd.closed
	upd.pending, upd.applied, upd.reopens, upd.closed = nil, false, false, false
	upd.mu.Unlock()
	defer func() {
		upd.mu.Lock()
		upd.pending, upd.applied, upd.reopens, upd.closed = pending, applied, reopens, closed
		upd.mu.Unlock()
	}()

	upd.mu.Lock()
	upd.applied, upd.reopens = true, true
	upd.mu.Unlock()
	if out, reopens := startSwap(true, true); out != swapBusy || !reopens {
		t.Fatalf("a swap under way: %v %v, want busy and reopening", out, reopens)
	}

	upd.mu.Lock()
	upd.applied, upd.reopens = false, false
	upd.mu.Unlock()
	if out, _ := startSwap(true, true); out != swapNone {
		t.Fatalf("nothing downloaded at quit: %v", out)
	}
	// A download finishes after the quit decision: it must not start.
	upd.mu.Lock()
	upd.pending = &update.Pending{Info: &update.Info{Version: "9.9.9"}}
	upd.mu.Unlock()
	if out, _ := startSwap(true, false); out != swapNone {
		t.Fatalf("a swap started after the quit decision: %v", out)
	}
	if applyUpdate(true) {
		t.Fatal("applyUpdate started a swap after the quit decision")
	}
}

// With nothing downloaded, applyUpdate starts no swap and quits nothing.
func TestApplyUpdateWithNothingWaiting(t *testing.T) {
	upd.mu.Lock()
	saved := upd.pending
	upd.pending = nil
	upd.mu.Unlock()
	defer func() {
		upd.mu.Lock()
		upd.pending = saved
		upd.mu.Unlock()
	}()
	if applyUpdate(true) {
		t.Fatal("nothing to swap in, yet a swap started")
	}
}

// A swap still being started by another caller settles who reopens only
// once it has started or failed: the quit handler waits for that instead of
// taking the claim as a started swap (which would leave Tolk closed if the
// launch then failed).
func TestStartSwapWaitsForALaunchUnderWay(t *testing.T) {
	upd.mu.Lock()
	pending, applied, reopens, closed, launching := upd.pending, upd.applied, upd.reopens, upd.closed, upd.launching
	upd.mu.Unlock()
	defer func() {
		upd.mu.Lock()
		upd.pending, upd.applied, upd.reopens, upd.closed, upd.launching = pending, applied, reopens, closed, launching
		upd.mu.Unlock()
	}()

	for _, launchWorks := range []bool{false, true} {
		ch := make(chan struct{})
		upd.mu.Lock()
		upd.pending, upd.applied, upd.reopens, upd.closed, upd.launching = nil, true, true, false, ch
		upd.mu.Unlock()

		type answer struct {
			out     swapOutcome
			reopens bool
		}
		got := make(chan answer, 1)
		go func() {
			out, r := startSwap(true, true)
			got <- answer{out, r}
		}()
		select {
		case a := <-got:
			t.Fatalf("answered %v before the launch under way settled", a)
		case <-time.After(50 * time.Millisecond):
		}
		// The other caller's launch finishes: started, or failed and released.
		upd.mu.Lock()
		if !launchWorks {
			upd.applied, upd.reopens = false, false
		}
		upd.launching = nil
		upd.mu.Unlock()
		close(ch)

		a := <-got
		if launchWorks && (a.out != swapBusy || !a.reopens) {
			t.Errorf("launch started: %v, want busy and reopening", a)
		}
		if !launchWorks && a.out != swapNone {
			t.Errorf("launch failed, nothing left to put in: %v, want none (so Tolk reopens itself)", a)
		}
	}
}

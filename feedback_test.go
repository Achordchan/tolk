package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCrashHead(t *testing.T) {
	out := "panic: runtime error: index out of range [3] with length 3\n\ngoroutine 1 [running]:\nmain.translateInMain(...)\n\t/src/main.go:70 +0x1d\n"
	if got := crashHead(out); got != "panic: runtime error: index out of range [3] with length 3 @ main.translateInMain(...)" {
		t.Errorf("crashHead = %q", got)
	}
	if crashHead("garbage") != "程序崩溃" {
		t.Error("no panic line must still give a title")
	}
}

// A dump that lost its first line, as issue #27's did: the "fatal error" below
// the stack header is the stack printer failing, not what went wrong.
func TestCrashHeadIgnoresErrorsAfterTheStack(t *testing.T) {
	dump := "\nruntime stack:\nfatal error: slice bounds out of range\npanic during panic\n\nruntime stack:\nfatal error: slice bounds out of range\nstack trace unavailable\n"
	if !isCrash(dump) {
		t.Fatal("dump dropped")
	}
	if got := crashHead(dump); got != "程序崩溃（原始错误未记录）" {
		t.Fatalf("crashHead = %q", got)
	}
	full := "fatal error: found bad pointer in Go heap" + dump
	if got := crashHead(full); got != "fatal error: found bad pointer in Go heap" {
		t.Fatalf("crashHead = %q", got)
	}
}

func TestStrayStderrIsNotACrash(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, crashFile), []byte("some library said something\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := takeCrash(dir); got != "" {
		t.Fatalf("takeCrash = %q", got)
	}
}

// The real thing, in a child process: a runtime fatal error prints its message
// before the crash output of debug.SetCrashOutput takes effect.
func TestCrashLogKeepsFatalErrorLine(t *testing.T) {
	if runtime.GOOS != "windows" && runtime.GOOS != "darwin" {
		t.Skip("crash_output_other.go keeps the runtime's own option, which misses this line")
	}
	if dir := os.Getenv("TOLK_TEST_CRASH_DIR"); dir != "" {
		setupCrashOutput(dir)
		runtime.GC() // the log must survive collection
		var m sync.Mutex
		m.Unlock()
		return
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	cmd := exec.Command(self, "-test.run=^TestCrashLogKeepsFatalErrorLine$")
	cmd.Env = append(os.Environ(), "TOLK_TEST_CRASH_DIR="+dir, "TOLK_SUPERVISED=")
	if err := cmd.Run(); err == nil {
		t.Fatal("the child should have crashed")
	}
	raw, err := os.ReadFile(filepath.Join(dir, crashFile))
	if err != nil {
		t.Fatal(err)
	}
	if got := crashHead(string(raw)); !strings.HasPrefix(got, "fatal error: sync: unlock of unlocked mutex") {
		t.Fatalf("crashHead = %q\n%s", got, raw)
	}
}

func TestIgnorableNetworkErrors(t *testing.T) {
	for _, s := range []string{"更新失败：Get \"https://x\": dial tcp: i/o timeout", "检查更新失败：no such host"} {
		if !ignorable(s) {
			t.Errorf("%q should not be reported", s)
		}
	}
	if ignorable("启动原生助手失败：exec format error") {
		t.Error("a real bug was dropped")
	}
}

func TestCrashWaitsUntilReported(t *testing.T) {
	dir := t.TempDir()
	crash := "panic: boom\n\ngoroutine 1 [running]:\nmain.main()\n"
	if err := os.WriteFile(filepath.Join(dir, crashFile), []byte(crash), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := takeCrash(dir); got != crash {
		t.Fatalf("first run after the crash: %q", got)
	}
	// The next run truncates crash.log; closed again before the report went
	// out, the run after that must still find the crash.
	if err := os.WriteFile(filepath.Join(dir, crashFile), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := takeCrash(dir); got != crash {
		t.Fatalf("crash lost after a short run: %q", got)
	}
	_ = os.Remove(filepath.Join(dir, crashPending)) // what reporting does once tried
	if got := takeCrash(dir); got != "" {
		t.Fatalf("reported crash came back: %q", got)
	}
}

func TestSignalCrashIsKept(t *testing.T) {
	for _, dump := range []string{
		"SIGSEGV: segmentation violation\nPC=0x7ff6 m=0 sigcode=1\n\ngoroutine 1 [running]:\nmain.callC()\n",
		"Exception 0xc0000005 0x0 0x0 0x7ffd\nPC=0x7ffd\n\nmain.callC()\n",
	} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, crashFile), []byte(dump), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := takeCrash(dir); got != dump {
			t.Fatalf("dump not kept: %q", got)
		}
		if h := crashHead(dump); h == "程序崩溃" || !strings.Contains(h, "@ main.callC()") {
			t.Fatalf("crashHead = %q", h)
		}
	}
	if takeCrash(t.TempDir()) != "" || isCrash(" \n") {
		t.Fatal("an empty crash.log is not a crash")
	}
}

func TestUpdateLogScrubbedBeforeCut(t *testing.T) {
	line := `copy C:\Users\Jane Doe\AppData\Local\Tolk\new\tolk.exe -> C:\Users\Jane Doe\AppData\Local\Programs\Tolk\tolk.exe ` + strings.Repeat("x", 300) + "\n"
	log := strings.Repeat(line, 200)
	path := filepath.Join(t.TempDir(), "update.log")
	tail := func(s string) string {
		if err := os.WriteFile(path, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
		return updateLogTail(path)
	}
	for _, cut := range []int{0, 7, 20, 40} { // move where the 12000-byte cut lands
		got := tail(log[:len(log)-cut])
		if strings.Contains(got, "Jane") || strings.Contains(got, "Doe") || strings.Contains(got, "ane D") {
			t.Fatalf("account name left after cut %d", cut)
		}
	}
	big := strings.Repeat(line, 2000) // over the 256 KiB read
	if got := tail(big); strings.Contains(got, "Doe") || got == "" {
		t.Fatal("long log not scrubbed or empty")
	}
	if updateLogTail(filepath.Join(t.TempDir(), "none")) != "" {
		t.Fatal("no log, no attachment")
	}
}

func TestHugeCrashReadsOnlyHead(t *testing.T) {
	dir := t.TempDir()
	head := "fatal error: concurrent map writes\n\ngoroutine 7 [running]:\n"
	dump := head + strings.Repeat("goroutine 9 [chan receive]:\nmain.worker()\n", 200000)
	if err := os.WriteFile(filepath.Join(dir, crashFile), []byte(dump), 0o600); err != nil {
		t.Fatal(err)
	}
	got := takeCrash(dir)
	if len(got) != maxCrashRead || !strings.HasPrefix(got, head) {
		t.Fatalf("read %d bytes", len(got))
	}
	// Moved aside whole, not rewritten.
	if fi, err := os.Stat(filepath.Join(dir, crashPending)); err != nil || fi.Size() != int64(len(dump)) {
		t.Fatalf("pending file: %v", err)
	}
}

func TestUpdateFailureOutlivesNotice(t *testing.T) {
	dir := t.TempDir()
	result := filepath.Join(dir, "last_update_result.json")
	failed := `{"status":"failed","message":"swap blocked","expected_version":"2.2.4"}`
	if err := os.WriteFile(result, []byte("\xef\xbb\xbf"+failed), 0o600); err != nil {
		t.Fatal(err)
	}
	when := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	_ = os.Chtimes(result, when, when)
	if got := keepUpdateFailure(dir); got != failed {
		t.Fatalf("snapshot %q", got)
	}
	_ = os.Remove(result) // what UpdateNotice does
	pending := filepath.Join(dir, updatePending)
	info, err := os.Stat(pending)
	if err != nil || !info.ModTime().Equal(when) {
		t.Fatalf("pending copy: %v", err)
	}
	// Closed before the report went out: the next start still has it.
	if got := keepUpdateFailure(dir); got != failed {
		t.Fatalf("lost after a short run: %q", got)
	}
	if res, ok := updateFailed(failed); !ok || res.Expected != "2.2.4" {
		t.Fatal("not read as a failure")
	}
	// A success is not kept for reporting.
	_ = os.Remove(pending)
	_ = os.WriteFile(result, []byte(`{"status":"success","expected_version":"2.2.4"}`), 0o600)
	keepUpdateFailure(dir)
	if _, err := os.Stat(pending); err == nil {
		t.Fatal("a success was queued as a failure")
	}
}

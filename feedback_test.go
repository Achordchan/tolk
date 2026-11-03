package main

import (
	"os"
	"path/filepath"
	"testing"
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

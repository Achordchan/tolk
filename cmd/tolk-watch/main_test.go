package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCrashDiagnostic(t *testing.T) {
	a := crashDiagnostic("timestamp info\npanic: first\ngoroutine 1", "exit status 2")
	b := crashDiagnostic("timestamp info\npanic: second\ngoroutine 1", "exit status 2")
	if a == b || a != "panic: first" {
		t.Fatalf("fingerprints share %q %q", a, b)
	}
	if crashDiagnostic("", "start failed") != "start failed" {
		t.Fatal("missing fallback")
	}
}

func TestCaptureKeepsPanicHeadline(t *testing.T) {
	c := capture{path: filepath.Join(t.TempDir(), "startup.log")}
	c.Write([]byte("panic: distinct error\n"))
	c.Write([]byte(strings.Repeat("stack frame\n", 10000)))
	if c.headline != "panic: distinct error" {
		t.Fatal(c.headline)
	}
}

func TestCaptureBuffersSplitHeadline(t *testing.T) {
	c := capture{path: filepath.Join(t.TempDir(), "log")}
	c.Write([]byte("panic: "))
	if c.headline != "" {
		t.Fatal("partial headline committed")
	}
	c.Write([]byte("specific failure\nstack\n"))
	if c.headline != "panic: specific failure" || !strings.Contains(c.crashHead, "stack") {
		t.Fatalf("headline %q, head %q", c.headline, c.crashHead)
	}
}

func TestExitSummary(t *testing.T) {
	// The fingerprint source of a silent exit is what it always was, so repeats
	// go to the issue already open for it.
	start := errors.New("file not found")
	title, description, source := exitSummary(start, "")
	if title != "[崩溃] 主程序无法启动" || source != "主程序启动或运行失败：file not found" || description != source {
		t.Fatalf("start failure: %q %q %q", title, description, source)
	}
	title, description, source = exitSummary(start, "panic: boom")
	if title != "[崩溃] 主程序异常退出" || source != "panic: boom" || !strings.Contains(description, "panic: boom") {
		t.Fatalf("crash: %q %q %q", title, description, source)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	exit := exec.Command(self, "-test.run=^$", "-test.badflag").Run()
	if exit == nil {
		t.Fatal("the child should have failed")
	}
	title, description, source = exitSummary(exit, "")
	if title != "[异常退出] 主程序没有留下崩溃信息" || !strings.Contains(description, "未响应") || source != "主程序启动或运行失败："+exit.Error() {
		t.Fatalf("silent exit: %q %q %q", title, description, source)
	}
}

package main

import (
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
		t.Fatalf("%+v", c)
	}
}

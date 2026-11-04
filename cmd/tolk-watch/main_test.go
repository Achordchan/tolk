package main

import "testing"

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

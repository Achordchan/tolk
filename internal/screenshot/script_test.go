package screenshot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScriptStartsWithBOM(t *testing.T) {
	s := Script()
	if !strings.HasPrefix(s, "\xef\xbb\xbf") || !strings.Contains(s, "function LoadControls") || !strings.Contains(s, "ShowDialog") {
		t.Fatal("workspace script incomplete")
	}
}

func TestPrepareCacheKeepsOnlyTheCurrentBuild(t *testing.T) {
	dir := t.TempDir()
	current := ControlsDLL(dir)
	for _, name := range []string{filepath.Base(current), "controls-0000000000000000.dll", "controls-x.dll.42.tmp"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "build-42"), 0o755); err != nil {
		t.Fatal(err)
	}
	dll, err := PrepareCache(dir)
	if err != nil || dll != current {
		t.Fatalf("got %q, %v", dll, err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != filepath.Base(current) {
		t.Fatalf("left behind: %v", entries)
	}
}

func TestPrepareCacheCreatesTheFolder(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Tolk", "screenshot")
	if _, err := PrepareCache(dir); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatal("folder not created")
	}
}

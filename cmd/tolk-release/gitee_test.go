package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestCollectAssetsKeepsAppcast(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"dazuofanyiguan_full.for.windows_2.0.0.zip", "Tolk-2.0.0-macos.zip", "appcast.xml", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files, err := collectAssets(dir, "2.0.0")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range files {
		got = append(got, filepath.Base(f))
	}
	if !slices.Contains(got, "appcast.xml") || slices.Contains(got, "notes.txt") {
		t.Fatalf("%v", got)
	}
}

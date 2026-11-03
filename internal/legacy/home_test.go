package legacy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMigrateMovesOldFolder(t *testing.T) {
	root := t.TempDir()
	old, dest := filepath.Join(root, ".dzfyq"), filepath.Join(root, ".tolk")
	os.MkdirAll(filepath.Join(old, "update_state"), 0o700)
	os.WriteFile(filepath.Join(old, "config.json"), []byte(`{"a":1}`), 0o600)
	moved, err := migrate(old, dest)
	if err != nil || !moved {
		t.Fatal(moved, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dest, "config.json")); string(b) != `{"a":1}` {
		t.Fatalf("%s", b)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("old folder should be gone")
	}
	// Second launch: nothing to do.
	if moved, err := migrate(old, dest); moved || err != nil {
		t.Fatal(moved, err)
	}
}

func TestMigrateKeepsExistingNewFolder(t *testing.T) {
	root := t.TempDir()
	old, dest := filepath.Join(root, ".dzfyq"), filepath.Join(root, ".tolk")
	os.MkdirAll(old, 0o700)
	os.WriteFile(filepath.Join(old, "config.json"), []byte("old"), 0o600)
	os.MkdirAll(dest, 0o700)
	os.WriteFile(filepath.Join(dest, "config.json"), []byte("new"), 0o600)
	if moved, err := migrate(old, dest); moved || err != nil {
		t.Fatal(moved, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dest, "config.json")); string(b) != "new" {
		t.Fatalf("%s", b)
	}
}

func TestCopyTreeSkipsUpdateCaches(t *testing.T) {
	root := t.TempDir()
	src, dst := filepath.Join(root, "a"), filepath.Join(root, "b")
	os.MkdirAll(filepath.Join(src, "update_cache"), 0o700)
	os.WriteFile(filepath.Join(src, "update_cache", "big.zip"), []byte("x"), 0o600)
	os.MkdirAll(filepath.Join(src, "update_state"), 0o700)
	os.WriteFile(filepath.Join(src, "update_state", "update.log"), []byte("log"), 0o600)
	if err := copyTree(src, dst, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dst, "update_cache")); !os.IsNotExist(err) {
		t.Fatal("update_cache copied")
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "update_state", "update.log")); string(b) != "log" {
		t.Fatal("update_state not copied")
	}
}

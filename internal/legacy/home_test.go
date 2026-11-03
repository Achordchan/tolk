package legacy

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
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

func TestHomeFallsBackWhenMoveFails(t *testing.T) {
	t.Setenv("TOLK_HOME", "")
	defer func() { fallbackHome = "" }()
	fallbackHome = "/old"
	if h, _ := Home(); h != "/old" {
		t.Fatal(h)
	}
}

func TestMigrateKeepsOldFolderWhenUnreadable(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a folder this user cannot enter: not on Windows, not as root")
	}
	root := t.TempDir()
	locked := filepath.Join(root, "locked")
	os.MkdirAll(filepath.Join(locked, ".dzfyq"), 0o700)
	os.Chmod(locked, 0o000)
	t.Cleanup(func() { os.Chmod(locked, 0o700) })
	if moved, err := migrate(filepath.Join(locked, ".dzfyq"), filepath.Join(root, ".tolk")); moved || err == nil {
		t.Fatalf("an unreadable old folder must be an error, got %v %v", moved, err)
	}
}

func TestMigrateLeavesLinkedSettingsInPlace(t *testing.T) {
	root := t.TempDir()
	synced := filepath.Join(root, "synced")
	os.MkdirAll(synced, 0o700)
	os.WriteFile(filepath.Join(synced, "work.json"), []byte("{}"), 0o600)

	inside := filepath.Join(root, "a", ".dzfyq")
	os.MkdirAll(inside, 0o700)
	if err := os.Symlink(filepath.Join(synced, "work.json"), filepath.Join(inside, "config.json")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	whole := filepath.Join(root, "b", ".dzfyq")
	os.MkdirAll(filepath.Dir(whole), 0o700)
	os.Symlink(synced, whole)

	for _, old := range []string{inside, whole} {
		dest := filepath.Join(filepath.Dir(old), ".tolk")
		if moved, err := migrate(old, dest); moved || !errors.Is(err, errHasLinks) {
			t.Fatalf("%s: want it left in place, got %v %v", old, moved, err)
		}
		if _, err := os.Lstat(dest); err == nil {
			t.Fatalf("%s: no new folder may appear", old)
		}
		if b, err := os.ReadFile(filepath.Join(old, "config.json")); old == inside && (err != nil || string(b) != "{}") {
			t.Fatalf("settings no longer readable: %q %v", b, err)
		}
	}
}

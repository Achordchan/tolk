package legacy

import (
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

func TestCopyKeepsSymlinks(t *testing.T) {
	root := t.TempDir()
	src, dst := filepath.Join(root, "old"), filepath.Join(root, "new")
	os.MkdirAll(src, 0o700)
	synced := filepath.Join(root, "synced.json")
	os.WriteFile(synced, []byte("{}"), 0o600)
	if err := os.Symlink(synced, filepath.Join(src, "config.json")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	if err := copyTree(src, dst, true); err != nil {
		t.Fatal(err)
	}
	if got, err := os.Readlink(filepath.Join(dst, "config.json")); err != nil || got != synced {
		t.Fatalf("link not kept: %q %v", got, err)
	}
}

func TestMigrateRepointsLinksIntoOldFolder(t *testing.T) {
	root := t.TempDir()
	old, dest := filepath.Join(root, ".dzfyq"), filepath.Join(root, ".tolk")
	os.MkdirAll(filepath.Join(old, "profiles"), 0o700)
	os.WriteFile(filepath.Join(old, "profiles", "work.json"), []byte(`{"a":1}`), 0o600)
	if err := os.Symlink(filepath.Join(old, "profiles", "work.json"), filepath.Join(old, "config.json")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	if moved, err := migrate(old, dest); !moved || err != nil {
		t.Fatal(moved, err)
	}
	b, err := os.ReadFile(filepath.Join(dest, "config.json"))
	if err != nil || string(b) != `{"a":1}` {
		t.Fatalf("link does not lead to the moved file: %q %v", b, err)
	}
}

func TestMigrateRollsBackWhenRelinkFails(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a folder this user cannot write to: not on Windows, not as root")
	}
	root := t.TempDir()
	old, dest := filepath.Join(root, ".dzfyq"), filepath.Join(root, ".tolk")
	sub := filepath.Join(old, "sub")
	os.MkdirAll(sub, 0o700)
	os.WriteFile(filepath.Join(old, "work.json"), []byte("{}"), 0o600)
	if err := os.Symlink(filepath.Join(old, "work.json"), filepath.Join(sub, "config.json")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	// A link earlier in the walk is rewritten before the one in sub fails;
	// it must be put back too.
	os.MkdirAll(filepath.Join(old, "a"), 0o700)
	os.Symlink(filepath.Join(old, "work.json"), filepath.Join(old, "a", "config.json"))
	os.Chmod(sub, 0o500) // the new link cannot be made here
	t.Cleanup(func() { os.Chmod(sub, 0o700); os.Chmod(filepath.Join(dest, "sub"), 0o700) })
	moved, err := migrate(old, dest)
	if moved || err == nil {
		t.Fatalf("want a rolled-back failure, got %v %v", moved, err)
	}
	if got, err := os.Readlink(filepath.Join(sub, "config.json")); err != nil || got != filepath.Join(old, "work.json") {
		t.Fatalf("old link changed: %q %v", got, err)
	}
	if got, err := os.Readlink(filepath.Join(old, "a", "config.json")); err != nil || got != filepath.Join(old, "work.json") {
		t.Fatalf("rewritten link not put back: %q %v", got, err)
	}
	if _, err := os.Lstat(dest); err == nil {
		t.Fatal("the new folder must not stay behind")
	}
}

func TestMigrateLinkedOldFolder(t *testing.T) {
	root := t.TempDir()
	synced := filepath.Join(root, "synced")
	os.MkdirAll(synced, 0o700)
	old, dest := filepath.Join(root, ".dzfyq"), filepath.Join(root, ".tolk")
	if err := os.Symlink(synced, old); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	os.WriteFile(filepath.Join(synced, "work.json"), []byte("{}"), 0o600)
	os.Symlink(filepath.Join(old, "work.json"), filepath.Join(synced, "config.json"))
	if moved, err := migrate(old, dest); !moved || err != nil {
		t.Fatal(moved, err)
	}
	if b, err := os.ReadFile(filepath.Join(dest, "config.json")); err != nil || string(b) != "{}" {
		t.Fatalf("settings not readable through the new folder: %q %v", b, err)
	}
}

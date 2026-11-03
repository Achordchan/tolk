package legacy

import (
	"io"
	"os"
	"path/filepath"
)

// oldHomeName is the folder versions up to 2.2.2 used, in the home folder.
const oldHomeName = ".dzfyq"

// skipOnCopy are the update downloads and backups: not worth copying.
var skipOnCopy = map[string]bool{"update_cache": true, "update_backup": true}

// fallbackHome is the old folder, used for this run when moving it failed, so
// the user's settings stay in use (and nothing new is written to the new
// folder, which would stop the move from being tried again).
var fallbackHome string

// MigrateHome moves the settings of 2.2.2 and older to Home once. It reports
// whether it moved anything. A folder that cannot be moved (a file still in
// use on Windows) is copied instead and the old one left in place. If neither
// works, this run keeps using the old folder and the next launch tries again.
func MigrateHome() (bool, error) {
	if os.Getenv("TOLK_HOME") != "" {
		return false, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false, err
	}
	old := filepath.Join(home, oldHomeName)
	moved, err := migrate(old, filepath.Join(home, ".tolk"))
	if err != nil {
		fallbackHome = old
	}
	return moved, err
}

func migrate(old, dest string) (bool, error) {
	if _, err := os.Lstat(dest); err == nil {
		return false, nil
	}
	if fi, err := os.Stat(old); err != nil || !fi.IsDir() {
		return false, nil
	}
	if err := os.Rename(old, dest); err == nil {
		return true, nil
	}
	tmp := dest + ".partial"
	_ = os.RemoveAll(tmp)
	if err := copyTree(old, tmp, true); err != nil {
		_ = os.RemoveAll(tmp)
		return false, err
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.RemoveAll(tmp)
		return false, err
	}
	return true, nil
}

func copyTree(src, dst string, top bool) error {
	if err := os.MkdirAll(dst, 0o700); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if top && skipOnCopy[e.Name()] {
			continue
		}
		from, to := filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())
		switch {
		case e.IsDir():
			if err := copyTree(from, to, false); err != nil {
				return err
			}
		case e.Type()&os.ModeSymlink != 0:
			// A settings file linked to a synced folder stays a link. If
			// the link cannot be made (Windows without the right), the
			// move fails and the old folder stays in use.
			target, err := os.Readlink(from)
			if err != nil {
				return err
			}
			if err := os.Symlink(target, to); err != nil {
				return err
			}
		case e.Type().IsRegular():
			if err := copyFile(from, to); err != nil {
				return err
			}
		}
	}
	return nil
}

func copyFile(from, to string) error {
	r, err := os.Open(from)
	if err != nil {
		return err
	}
	defer r.Close()
	w, err := os.OpenFile(to, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, r); err != nil {
		w.Close()
		return err
	}
	return w.Close()
}

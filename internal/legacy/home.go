package legacy

import (
	"errors"
	"io"
	"io/fs"
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

// errHasLinks means the old folder is, or holds, a symbolic link. Where such
// a link leads can depend on the folder's name and place (relative links,
// links through other links), so the folder is not moved: Tolk keeps using it
// where it is, as it always did.
var errHasLinks = errors.New("旧设置目录里有符号链接")

// MigrateHome moves the settings of 2.2.2 and older to Home once. It reports
// whether it moved anything. A folder that cannot be moved (a file still in
// use on Windows) is copied instead and the old one left in place. If neither
// works, this run keeps using the old folder and the next launch tries again.
// A folder with links in it stays in use where it is.
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
	if err != nil && !moved {
		fallbackHome = old
	}
	if errors.Is(err, errHasLinks) {
		return false, nil
	}
	return moved, err
}

func migrate(old, dest string) (bool, error) {
	if _, err := os.Lstat(dest); err == nil {
		return false, nil
	}
	fi, err := os.Stat(old)
	if errors.Is(err, fs.ErrNotExist) || (err == nil && !fi.IsDir()) {
		return false, nil // nothing to move
	}
	if err != nil {
		// There but unreadable now: keep using it rather than start afresh.
		return false, err
	}
	if linked, err := hasLinks(old); err != nil {
		return false, err
	} else if linked {
		return false, errHasLinks
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

// hasLinks reports whether dir is a symbolic link or has one anywhere in it.
func hasLinks(dir string) (bool, error) {
	found := false
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	if fi, lerr := os.Lstat(dir); lerr == nil && fi.Mode()&os.ModeSymlink != 0 {
		return true, nil
	}
	return found, err
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

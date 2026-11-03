package legacy

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// updateMu makes load, change, backup and replace one transaction, so two
// quick saves (or a save racing the Mac import) cannot lose each other's edits.
var updateMu sync.Mutex

// fileMu keeps Tolk's own reads off the file while it is being replaced. On
// Windows a file another handle has open cannot be replaced: switching the
// target language saved the config while a translation was reading it, and
// the save failed with "rename …config.json: Access is denied".
var fileMu sync.RWMutex

// Update loads the config (an empty one if the file is missing), lets fn
// change it, and writes it back atomically. Unknown keys survive because the
// file is handled as a generic map. The first time an existing file is
// changed, a copy is kept next to it as config.json.pre-tolk.bak.
func Update(path string, fn func(m map[string]any)) error {
	updateMu.Lock()
	defer updateMu.Unlock()
	m, err := Load(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		m = map[string]any{}
	case err != nil:
		return err
	default:
		bak := path + ".pre-tolk.bak"
		if _, statErr := os.Stat(bak); errors.Is(statErr, os.ErrNotExist) {
			if b, readErr := os.ReadFile(path); readErr == nil {
				if err := os.WriteFile(bak, b, 0o600); err != nil {
					return err
				}
			}
		}
	}
	fn(m)
	out, err := json.MarshalIndent(m, "", "    ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "config.*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(out, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	fileMu.Lock()
	defer fileMu.Unlock()
	return replaceFile(tmp.Name(), path)
}

// Sub returns m[key] as a map, creating it when missing or of another type.
func Sub(m map[string]any, key string) map[string]any {
	if s, ok := m[key].(map[string]any); ok {
		return s
	}
	s := map[string]any{}
	m[key] = s
	return s
}

// replaceFile renames from over to. Antivirus and search indexers open new
// files for a moment, which on Windows makes the rename fail with "Access is
// denied"; it is retried for up to about two seconds.
func replaceFile(from, to string) error {
	err := os.Rename(from, to)
	if runtime.GOOS != "windows" {
		return err
	}
	for wait := 20 * time.Millisecond; err != nil && wait <= 640*time.Millisecond; wait *= 2 {
		time.Sleep(wait)
		err = os.Rename(from, to)
	}
	return err
}

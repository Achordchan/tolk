package legacy

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Update loads the config (an empty one if the file is missing), lets fn
// change it, and writes it back atomically. Unknown keys survive because the
// file is handled as a generic map. The first time an existing file is
// changed, a copy is kept next to it as config.json.pre-tolk.bak.
func Update(path string, fn func(m map[string]any)) error {
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
	return os.Rename(tmp.Name(), path)
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

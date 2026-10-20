// Package legacy reads the settings of the Python (dazuo-win) app so a
// Tolk upgrade keeps them. The file is only read; unknown keys are kept.
package legacy

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Summary describes an existing config without exposing secrets.
type Summary struct {
	Found       bool     `json:"found"`
	Path        string   `json:"path"`
	Engine      string   `json:"engine"`
	TargetLang  string   `json:"targetLang"`
	KeyVendors  []string `json:"keyVendors"`
	UnknownKeys []string `json:"unknownKeys"`
}

// DefaultPath is where dazuo-win keeps config.json.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".dzfyq", "config.json"), nil
}

var known = map[string]bool{
	"translation": true, "openai_compat": true, "deepl": true,
}

// Load parses the file into a generic map so nothing is lost on write-back.
func Load(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("legacy config is not a JSON object: %w", err)
	}
	return m, nil
}

// Summarize reports what a legacy config holds.
func Summarize(path string) (Summary, error) {
	s := Summary{Path: path}
	m, err := Load(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	s.Found = true
	if t, ok := m["translation"].(map[string]any); ok {
		s.Engine, _ = t["api"].(string)
		s.TargetLang, _ = t["target_lang"].(string)
	}
	if oc, ok := m["openai_compat"].(map[string]any); ok {
		if ps, ok := oc["profiles"].(map[string]any); ok {
			for vendor, p := range ps {
				if pm, ok := p.(map[string]any); ok {
					if k, _ := pm["api_key"].(string); k != "" {
						s.KeyVendors = append(s.KeyVendors, vendor)
					}
				}
			}
		}
	}
	for k := range m {
		if !known[k] {
			s.UnknownKeys = append(s.UnknownKeys, k)
		}
	}
	return s, nil
}

// Backup copies the file to config.json.pre-upgrade.<time>.bak once per run
// and returns the backup path. It never modifies the original.
func Backup(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	dst := fmt.Sprintf("%s.pre-upgrade.%s.bak", path, time.Now().Format("20060102_150405"))
	return dst, os.WriteFile(dst, b, 0o600)
}

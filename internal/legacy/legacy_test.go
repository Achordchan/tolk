package legacy

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

const sample = `{
 "translation": {"api": "google", "target_lang": "简体中文"},
 "openai_compat": {"vendor": "deepseek", "profiles": {
   "deepseek": {"base_url": "x", "model": "m", "api_key": "sk-secret"},
   "openai": {"base_url": "y", "model": "m", "api_key": ""}}},
 "hotkeys": {"copy": "Ctrl+Shift+C"}
}`

func TestSummarizeKeepsSecretsOut(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(sample), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Summarize(p)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Found || s.Engine != "google" || s.TargetLang != "简体中文" {
		t.Fatalf("bad summary: %+v", s)
	}
	if !reflect.DeepEqual(s.KeyVendors, []string{"deepseek"}) {
		t.Fatalf("key vendors: %v", s.KeyVendors)
	}
	sort.Strings(s.UnknownKeys)
	if !reflect.DeepEqual(s.UnknownKeys, []string{"hotkeys"}) {
		t.Fatalf("unknown keys: %v", s.UnknownKeys)
	}
}

func TestMissingFileIsNotAnError(t *testing.T) {
	s, err := Summarize(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil || s.Found {
		t.Fatalf("got %+v, %v", s, err)
	}
}

func TestBackupLeavesOriginal(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(p, []byte(sample), 0o600)
	b, err := Backup(p)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(b)
	orig, _ := os.ReadFile(p)
	if string(got) != sample || string(orig) != sample {
		t.Fatal("backup or original changed")
	}
}

func TestBuildEngineUsesVendorProfile(t *testing.T) {
	m := map[string]any{"openai_compat": map[string]any{
		"vendor": "deepseek", "base_url": "old", "model": "old", "api_key": "old",
		"profiles": map[string]any{"deepseek": map[string]any{"base_url": "https://x/v1", "model": "m", "api_key": "k"}},
	}}
	e, err := BuildEngine(m, "openai_compat")
	if err != nil || e == nil {
		t.Fatalf("got %v, %v", e, err)
	}
	if _, err := BuildEngine(map[string]any{}, "openai_compat"); err == nil {
		t.Fatal("expected missing url error")
	}
	if _, err := BuildEngine(m, "microsoft"); err == nil {
		t.Fatal("microsoft is not ported yet")
	}
}

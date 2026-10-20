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

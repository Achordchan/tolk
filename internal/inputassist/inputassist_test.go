package inputassist

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestDefaults(t *testing.T) {
	d := Default()
	if d.Enabled || d.AutoShow || !d.CacheBadge || d.ShortcutKeyCode != 49 || d.ShortcutModifiers != 2560 || d.Scope != "global" || d.Allowlist != "" {
		t.Fatalf("%+v", d)
	}
	if !reflect.DeepEqual(d.Targets, []string{"英语"}) || len(strings.Split(d.Blocklist, "\n")) != 6 || !strings.HasPrefix(d.Blocklist, "com.apple.Terminal\n") {
		t.Fatalf("%+v", d)
	}
	if err := Validate(d); err != nil {
		t.Fatal(err)
	}
	if got := FromConfig(map[string]any{}); !reflect.DeepEqual(got, d) {
		t.Fatalf("empty config: %+v", got)
	}
}

func TestValidate(t *testing.T) {
	bad := map[string]func(*Config){
		"no targets": func(s *Config) { s.Targets = nil },
		"seven": func(s *Config) {
			s.Targets = []string{"英语", "日语", "韩语", "法语", "德语", "俄语", "西班牙语"}
		},
		"auto":         func(s *Config) { s.Targets = []string{"自动检测"} },
		"unknown":      func(s *Config) { s.Targets = []string{"火星语"} },
		"duplicate":    func(s *Config) { s.Targets = []string{"英语", "英语"} },
		"key high":     func(s *Config) { s.ShortcutKeyCode = 128 },
		"key negative": func(s *Config) { s.ShortcutKeyCode = -1 },
		"modifiers":    func(s *Config) { s.ShortcutModifiers = -1 },
		"scope":        func(s *Config) { s.Scope = "everything" },
	}
	for name, mut := range bad {
		s := Default()
		mut(&s)
		if Validate(s) == nil {
			t.Errorf("%s accepted", name)
		}
	}
	s := Default()
	s.Targets = []string{"英语", "日语", "韩语", "法语", "德语", "俄语"}
	s.Scope = ScopeAllowlist
	if err := Validate(s); err != nil {
		t.Fatal(err)
	}
}

func TestRoundTripKeepsUnknownKeys(t *testing.T) {
	cfg := map[string]any{"other": 1.0, "tolk": map[string]any{"mini_mode": true, Key: map[string]any{"future": "x"}}}
	s := Default()
	s.Enabled, s.Targets, s.Scope, s.Allowlist, s.AutoShow = true, []string{"日语", "英语"}, ScopeAllowlist, "Notes\nSafari", true
	Store(cfg, s)
	b, _ := json.Marshal(cfg) // through the file, as numbers become float64
	var back map[string]any
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if got := FromConfig(back); !reflect.DeepEqual(got, s) {
		t.Fatalf("%+v != %+v", got, s)
	}
	m := back["tolk"].(map[string]any)
	if m["mini_mode"] != true || m[Key].(map[string]any)["future"] != "x" || back["other"] != 1.0 {
		t.Fatalf("lost keys: %v", back)
	}
}

func TestFromConfigIsLenient(t *testing.T) {
	cfg := map[string]any{"tolk": map[string]any{Key: map[string]any{
		"targets": []any{"火星语", "自动检测", "日语", "日语", 5.0}, "shortcut_key_code": 999.0,
		"shortcut_modifiers": -3.0, "scope": "x", "enabled": "yes",
	}}}
	s := FromConfig(cfg)
	if !reflect.DeepEqual(s.Targets, []string{"日语"}) || s.ShortcutKeyCode != 49 || s.ShortcutModifiers != 2560 || s.Scope != "global" || s.Enabled {
		t.Fatalf("%+v", s)
	}
}

func TestParseListAndConfigure(t *testing.T) {
	if got := ParseList(" a \n\nb, c\n"); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Fatal(got)
	}
	c := Configure(Default(), "pink")
	if c["appearance"] != "pink" || len(c["blocklist"].([]string)) != 6 || len(c["allowlist"].([]string)) != 0 {
		t.Fatal(c)
	}
}

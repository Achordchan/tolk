// Package inputassist holds the settings of the Mac "输入助手" (select text,
// press a shortcut, pick a translation to replace it with), ported from
// bagayalu's InputAssist. They live under "tolk"."input_assist" in the shared
// config file; the native helper does the work and Go only pushes the settings.
package inputassist

import (
	"fmt"
	"strings"

	"tolk/internal/engine"
	"tolk/internal/legacy"
)

// Key is the object kept under "tolk" in the config.
const Key = "input_assist"

const (
	// MaxTargets is the most target languages a user can pick.
	MaxTargets = 6
	// RecommendedTargets is where the settings page starts to hint.
	RecommendedTargets = 3

	ScopeGlobal    = "global"
	ScopeAllowlist = "allowlist"

	// Carbon modifier bits, as the old app's carbonModifiers.
	ModCmd     = 256
	ModShift   = 512
	ModOption  = 2048
	ModControl = 4096

	keySpace = 49
)

// DefaultBlocklist is the old app's InputAssistAppFilter.defaultBlocklist:
// terminals and password managers.
var DefaultBlocklist = []string{
	"com.apple.Terminal",
	"com.googlecode.iterm2",
	"com.apple.keychainaccess",
	"com.1password.1password",
	"com.agilebits.onepassword7",
	"com.bitwarden.desktop",
}

// Config is the whole feature configuration. It is what the settings page
// edits and (with the appearance added) what the helper receives.
type Config struct {
	Enabled           bool     `json:"enabled"`
	Targets           []string `json:"targets"` // Tolk's Chinese language labels
	ShortcutKeyCode   int      `json:"shortcutKeyCode"`
	ShortcutModifiers int      `json:"shortcutModifiers"` // Carbon bits
	Scope             string   `json:"scope"`             // "global" | "allowlist"
	Blocklist         string   `json:"blocklist"`         // newline separated
	Allowlist         string   `json:"allowlist"`
	AutoShow          bool     `json:"autoShow"`
	CacheBadge        bool     `json:"cacheBadge"`
}

// Default is a fresh install: off, English, ⌥⇧Space, global with the blocklist.
func Default() Config {
	return Config{
		Targets:           []string{"英语"},
		ShortcutKeyCode:   keySpace,
		ShortcutModifiers: ModOption | ModShift,
		Scope:             ScopeGlobal,
		Blocklist:         strings.Join(DefaultBlocklist, "\n"),
		CacheBadge:        true,
	}
}

// KnownTarget reports whether l can be a target: a Tolk language, not 自动检测.
func KnownTarget(l string) bool {
	for _, x := range engine.Languages {
		if x == l {
			return true
		}
	}
	return false
}

// SanitizeTargets drops unknown and repeated languages and keeps the first
// MaxTargets, like the old app's sanitizeTargets. Empty input gives the default.
func SanitizeTargets(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, l := range in {
		l = strings.TrimSpace(l)
		if !KnownTarget(l) || seen[l] {
			continue
		}
		seen[l] = true
		out = append(out, l)
		if len(out) == MaxTargets {
			break
		}
	}
	if len(out) == 0 {
		return Default().Targets
	}
	return out
}

// Validate is the strict check used when the user saves.
func Validate(s Config) error {
	if n := len(s.Targets); n < 1 || n > MaxTargets {
		return fmt.Errorf("目标语言需要 1 到 %d 个", MaxTargets)
	}
	seen := map[string]bool{}
	for _, l := range s.Targets {
		if !KnownTarget(l) {
			return fmt.Errorf("不支持的目标语言：%s", l)
		}
		if seen[l] {
			return fmt.Errorf("目标语言重复：%s", l)
		}
		seen[l] = true
	}
	if s.ShortcutKeyCode < 0 || s.ShortcutKeyCode > 127 {
		return fmt.Errorf("不支持的快捷键")
	}
	if s.ShortcutModifiers < 0 {
		return fmt.Errorf("不支持的快捷键")
	}
	if s.Scope != ScopeGlobal && s.Scope != ScopeAllowlist {
		return fmt.Errorf("不支持的应用范围：%s", s.Scope)
	}
	return nil
}

func integer(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		if n == float64(int(n)) {
			return int(n), true
		}
	case int:
		return n, true
	}
	return 0, false
}

// FromConfig reads the settings out of the whole config map, falling back to
// the defaults for anything missing or invalid.
func FromConfig(cfg map[string]any) Config {
	s := Default()
	t, _ := cfg["tolk"].(map[string]any)
	m, _ := t[Key].(map[string]any)
	if m == nil {
		return s
	}
	if v, ok := m["enabled"].(bool); ok {
		s.Enabled = v
	}
	var raw []string
	switch l := m["targets"].(type) {
	case []any:
		for _, x := range l {
			if str, ok := x.(string); ok {
				raw = append(raw, str)
			}
		}
	case []string:
		raw = l
	}
	s.Targets = SanitizeTargets(raw)
	if v, ok := integer(m["shortcut_key_code"]); ok && v >= 0 && v <= 127 {
		s.ShortcutKeyCode = v
	}
	if v, ok := integer(m["shortcut_modifiers"]); ok && v >= 0 {
		s.ShortcutModifiers = v
	}
	if v, _ := m["scope"].(string); v == ScopeGlobal || v == ScopeAllowlist {
		s.Scope = v
	}
	if v, ok := m["blocklist"].(string); ok {
		s.Blocklist = v
	}
	if v, ok := m["allowlist"].(string); ok {
		s.Allowlist = v
	}
	if v, ok := m["auto_show"].(bool); ok {
		s.AutoShow = v
	}
	if v, ok := m["cache_badge"].(bool); ok {
		s.CacheBadge = v
	}
	return s
}

// Store writes s into the config map, keeping any unknown keys already in
// the input_assist object.
func Store(cfg map[string]any, s Config) {
	m := legacy.Sub(legacy.Sub(cfg, "tolk"), Key)
	m["enabled"] = s.Enabled
	m["targets"] = append([]string{}, s.Targets...)
	m["shortcut_key_code"] = s.ShortcutKeyCode
	m["shortcut_modifiers"] = s.ShortcutModifiers
	m["scope"] = s.Scope
	m["blocklist"] = s.Blocklist
	m["allowlist"] = s.Allowlist
	m["auto_show"] = s.AutoShow
	m["cache_badge"] = s.CacheBadge
}

// ParseList splits a newline (or comma) separated list, as the old app does.
func ParseList(raw string) []string {
	out := []string{}
	for _, f := range strings.FieldsFunc(raw, func(r rune) bool { return r == '\n' || r == ',' }) {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

// Configure is the params object of the helper's inputassist.configure.
func Configure(s Config, appearance string) map[string]any {
	return map[string]any{
		"enabled":           s.Enabled,
		"targets":           s.Targets,
		"shortcutKeyCode":   s.ShortcutKeyCode,
		"shortcutModifiers": s.ShortcutModifiers,
		"scope":             s.Scope,
		"blocklist":         ParseList(s.Blocklist),
		"allowlist":         ParseList(s.Allowlist),
		"autoShow":          s.AutoShow,
		"cacheBadge":        s.CacheBadge,
		"appearance":        appearance,
	}
}

package main

import (
	"fmt"
	"sync"

	"tolk/internal/engine"
	"tolk/internal/legacy"
)

// Prefs are Tolk's own switches, kept under "tolk" in the shared config file
// so dazuo-win ignores them. The language pair lives where dazuo-win keeps it.
type Prefs struct {
	MiniMode     bool `json:"miniMode"`
	DoubleCopy   bool `json:"doubleCopy"`
	DoubleCopyMs int  `json:"doubleCopyMs"`
	// macOS: listen for ⌘C⌘C system-wide through the helper (needs Accessibility)
	// instead of watching the clipboard; and the double ⌘+key screenshot shortcut.
	SystemHotkey      bool   `json:"systemHotkey"`
	ScreenshotHotkey  bool   `json:"screenshotHotkey"`
	ScreenshotKeyCode int    `json:"screenshotKeyCode"`
	SourceLang        string `json:"sourceLang"`
	TargetLang        string `json:"targetLang"`
	// Theme is "system", "light", "dark" or "pink" (dazuo-win's three themes).
	Theme      string `json:"theme"`
	SourceFont int    `json:"sourceFont"`
	TargetFont int    `json:"targetFont"`
}

const (
	minFont     = 12
	maxFont     = 28
	defaultFont = 18
)

func defaultPrefs() Prefs {
	return Prefs{DoubleCopy: true, DoubleCopyMs: 550, ScreenshotHotkey: true, ScreenshotKeyCode: 7, SourceLang: engine.Auto, TargetLang: "简体中文",
		Theme: "system", SourceFont: defaultFont, TargetFont: defaultFont}
}

func validTheme(t string) bool { return t == "system" || t == "light" || t == "dark" || t == "pink" }

func clampFont(n int) int {
	if n < minFont {
		return minFont
	}
	if n > maxFont {
		return maxFont
	}
	return n
}

var (
	prefsMu sync.Mutex
	prefs   = defaultPrefs()
)

func loadPrefs() Prefs {
	prefsMu.Lock()
	defer prefsMu.Unlock()
	p, err := legacy.DefaultPath()
	if err != nil {
		return prefs
	}
	m, err := legacy.Load(p)
	if err != nil {
		return prefs
	}
	out := defaultPrefs()
	if t, ok := m["tolk"].(map[string]any); ok {
		if v, ok := t["mini_mode"].(bool); ok {
			out.MiniMode = v
		}
		if v, ok := t["double_copy"].(bool); ok {
			out.DoubleCopy = v
		}
		if v, ok := t["double_copy_ms"].(float64); ok && v >= 200 && v <= 2000 {
			out.DoubleCopyMs = int(v)
		}
		if v, ok := t["system_hotkey"].(bool); ok {
			out.SystemHotkey = v
		}
		if v, ok := t["screenshot_hotkey"].(bool); ok {
			out.ScreenshotHotkey = v
		}
		if v, ok := t["screenshot_key_code"].(float64); ok && v >= 0 && v < 128 {
			out.ScreenshotKeyCode = int(v)
		}
	}
	if v, _ := m["theme"].(string); v == "light" || v == "dark" || v == "pink" {
		out.Theme = v
	}
	if d, ok := m["display"].(map[string]any); ok {
		if v, ok := d["source_font_size"].(float64); ok {
			out.SourceFont = clampFont(int(v))
		}
		if v, ok := d["target_font_size"].(float64); ok {
			out.TargetFont = clampFont(int(v))
		}
	}
	if t, ok := m["translation"].(map[string]any); ok {
		if v, _ := t["source_lang"].(string); v != "" {
			out.SourceLang = v
		}
		if v, _ := t["target_lang"].(string); v != "" {
			out.TargetLang = v
		}
	}
	prefs = out
	return out
}

func currentPrefs() Prefs {
	prefsMu.Lock()
	defer prefsMu.Unlock()
	return prefs
}

func validLang(l string, allowAuto bool) bool {
	if allowAuto && l == engine.Auto {
		return true
	}
	for _, x := range engine.Languages {
		if x == l {
			return true
		}
	}
	return false
}

// GetPrefs returns the switches and the language pair.
func (Tolk) GetPrefs() Prefs { return loadPrefs() }

// SetLanguages remembers the language pair in the shared config.
func (Tolk) SetLanguages(source, target string) error {
	if !validLang(source, true) || !validLang(target, false) {
		return fmt.Errorf("不支持的语言：%s → %s", source, target)
	}
	p, err := legacy.DefaultPath()
	if err != nil {
		return err
	}
	if err := legacy.Update(p, func(m map[string]any) {
		t := legacy.Sub(m, "translation")
		t["source_lang"], t["target_lang"] = source, target
	}); err != nil {
		return err
	}
	loadPrefs()
	return nil
}

// SetMini turns the Mini bubble on or off.
func (Tolk) SetMini(on bool) error {
	return setTolkPref(func(t map[string]any) { t["mini_mode"] = on })
}

// SetDoubleCopy sets the Cmd/Ctrl+C+C switch and its time window.
func (Tolk) SetDoubleCopy(on bool, windowMs int) error {
	if windowMs < 200 || windowMs > 2000 {
		return fmt.Errorf("时间窗口需要在 200 到 2000 毫秒之间")
	}
	if err := setTolkPref(func(t map[string]any) { t["double_copy"], t["double_copy_ms"] = on, windowMs }); err != nil {
		return err
	}
	macApplyHotkeys() // the helper's double-copy hotkey follows this switch too
	return nil
}

func setTolkPref(fn func(t map[string]any)) error {
	p, err := legacy.DefaultPath()
	if err != nil {
		return err
	}
	if err := legacy.Update(p, func(m map[string]any) { fn(legacy.Sub(m, "tolk")) }); err != nil {
		return err
	}
	loadPrefs()
	return nil
}

// SetAppearance stores the theme and text sizes where dazuo-win keeps them. An
// automatic theme drops the key, which the old app reads as its default.
func (Tolk) SetAppearance(theme string, sourceFont, targetFont int) error {
	if !validTheme(theme) {
		return fmt.Errorf("不支持的主题：%s", theme)
	}
	p, err := legacy.DefaultPath()
	if err != nil {
		return err
	}
	if err := legacy.Update(p, func(m map[string]any) {
		if theme == "system" {
			delete(m, "theme")
		} else {
			m["theme"] = theme
		}
		d := legacy.Sub(m, "display")
		d["source_font_size"], d["target_font_size"] = clampFont(sourceFont), clampFont(targetFont)
	}); err != nil {
		return err
	}
	loadPrefs()
	_ = PrefsChanged.Broadcast(currentPrefs())
	return nil
}

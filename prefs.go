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
	MiniMode     bool   `json:"miniMode"`
	DoubleCopy   bool   `json:"doubleCopy"`
	DoubleCopyMs int    `json:"doubleCopyMs"`
	SourceLang   string `json:"sourceLang"`
	TargetLang   string `json:"targetLang"`
}

var (
	prefsMu sync.Mutex
	prefs   = Prefs{DoubleCopy: true, DoubleCopyMs: 550, SourceLang: engine.Auto, TargetLang: "简体中文"}
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
	out := Prefs{DoubleCopy: true, DoubleCopyMs: 550, SourceLang: engine.Auto, TargetLang: "简体中文"}
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
	return setTolkPref(func(t map[string]any) { t["double_copy"], t["double_copy_ms"] = on, windowMs })
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

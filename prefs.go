package main

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/egoist/mygo"

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
	SystemHotkey      bool `json:"systemHotkey"`
	ScreenshotHotkey  bool `json:"screenshotHotkey"`
	ScreenshotKeyCode int  `json:"screenshotKeyCode"`
	// ScreenshotAutoTranslate translates as soon as a region is selected, on
	// both platforms; off waits for the 翻译 button. Off by default: the
	// manual flow is what Windows always did and what users preferred.
	ScreenshotAutoTranslate bool   `json:"screenshotAutoTranslate"`
	SourceLang              string `json:"sourceLang"`
	TargetLang              string `json:"targetLang"`
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

// applyNativeTheme makes the title bar and other native chrome match the page
// theme: pink is a light theme, and only "system" follows the OS.
func applyNativeTheme(theme string) {
	switch theme {
	case "dark":
		mygo.Theme.SetSource(mygo.ThemeDark)
	case "light", "pink":
		mygo.Theme.SetSource(mygo.ThemeLight)
	default:
		mygo.Theme.SetSource(mygo.ThemeSystem)
	}
	tintTitleBar(theme == "pink")
}

// mainWindow is the main window once created; its title bar follows the
// pink theme, which the system light/dark title bars cannot show.
var mainWindow atomic.Pointer[mygo.Window]

const (
	mainBackground = "light-dark(#ececec, #2b2b2b)"
	pinkBackground = "#fff4f7" // --bg of the pink theme (src/style.css)
	pinkTitle      = 0x4a3039  // --fg of the pink theme
)

// tintTitleBar paints the main window's title bar in the pink theme's
// background (on: true) or gives it back to the system.
func tintTitleBar(on bool) {
	w := mainWindow.Load()
	if w == nil {
		return
	}
	bg := mainBackground
	if on {
		bg = pinkBackground
	}
	_ = w.SetBackgroundColor(bg)
	nativeTitleBar(w, on, 0xfff4f7, pinkTitle)
}

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
	prefsMu sync.Mutex // held by loadPrefs while it reads the settings file
	prefs   = defaultPrefs()
	// prefsNow is the last loaded prefs, published so currentPrefs never
	// waits on that file: the Windows keyboard hook reads it on every key,
	// and Windows drops a hook that is slow to answer.
	prefsNow atomic.Pointer[Prefs]
)

func init() { p := prefs; prefsNow.Store(&p) }

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
		if v, ok := t["screenshot_auto_translate"].(bool); ok {
			out.ScreenshotAutoTranslate = v
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
	prefsNow.Store(&out)
	return out
}

// currentPrefs is the last loaded prefs, without blocking.
func currentPrefs() Prefs { return *prefsNow.Load() }

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

// SetScreenshotAutoTranslate sets whether a screenshot translates as soon as
// the region is selected. It applies from the next screenshot.
func (Tolk) SetScreenshotAutoTranslate(on bool) error {
	return setTolkPref(func(t map[string]any) { t["screenshot_auto_translate"] = on })
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
	go refreshTray()
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
	applyNativeTheme(theme)
	_ = PrefsChanged.Broadcast(currentPrefs())
	go pushInputAssist() // the panel follows the theme
	return nil
}

package main

import (
	"context"
	"fmt"
	"time"

	"github.com/egoist/mygo"
)

// MacNotice carries a short message from the native helper to the windows
// (a screenshot problem, a lost permission).
var MacNotice = mygo.NewEvent[string]("macNotice")

// MacStatus is what the "截图与权限" pane shows.
type MacStatus struct {
	Available       bool     `json:"available"` // this build has a helper at all
	Running         bool     `json:"running"`
	Version         string   `json:"version"`
	Capabilities    []string `json:"capabilities"`
	Accessibility   bool     `json:"accessibility"`
	ScreenRecording bool     `json:"screenRecording"`
	HotkeyRunning   bool     `json:"hotkeyRunning"`
	HotkeyError     string   `json:"hotkeyError"`
}

// MacStatus reads the helper's state and permissions (no system prompts).
func (Tolk) MacStatus() MacStatus { return macStatus() }

// MacRequestPermission asks macOS for "accessibility" or "screenRecording".
func (Tolk) MacRequestPermission(kind string) error {
	if kind != "accessibility" && kind != "screenRecording" {
		return fmt.Errorf("未知的权限：%s", kind)
	}
	return macSimple("permissions.request", map[string]any{"kind": kind})
}

// MacGuide opens the helper's permission guide window.
func (Tolk) MacGuide() error { return macSimple("permissions.guide", nil) }

// MacScreenshot starts screenshot translation ("translate") or text
// extraction ("ocr") with the current language pair.
func (Tolk) MacScreenshot(mode string) error {
	if mode != "ocr" {
		mode = "translate"
	}
	return macScreenshot(mode)
}

// SetMacHotkeys saves the two Mac shortcut switches and applies them.
func (Tolk) SetMacHotkeys(system, screenshot bool, keyCode int) error {
	if keyCode < 0 || keyCode > 127 {
		return fmt.Errorf("不支持的按键")
	}
	if err := setTolkPref(func(t map[string]any) {
		t["system_hotkey"], t["screenshot_hotkey"], t["screenshot_key_code"] = system, screenshot, keyCode
	}); err != nil {
		return err
	}
	macApplyHotkeys()
	return nil
}

// MacImportLegacy copies the old Mac app's settings and API key into Tolk.
// It returns notes about anything that did not carry over.
func (Tolk) MacImportLegacy() ([]string, error) { return macImport(true) }

func macCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 20*time.Second)
}

package main

import (
	"net/url"
	"strings"

	"tolk/internal/inputassist"
	"tolk/internal/legacy"
)

// InputAssistState is what the settings page shows for 输入助手: the saved
// settings plus whether the helper has it running.
type InputAssistState struct {
	Settings inputassist.Config `json:"settings"`
	Active   bool               `json:"active"`
	Error    string             `json:"error"`
}

func loadInputAssist() inputassist.Config {
	p, err := legacy.DefaultPath()
	if err != nil {
		return inputassist.Default()
	}
	m, err := legacy.Load(p)
	if err != nil {
		return inputassist.Default()
	}
	return inputassist.FromConfig(m)
}

func inputAssistState() InputAssistState {
	st := InputAssistState{Settings: loadInputAssist()}
	st.Active, st.Error = macInputAssistStatus()
	return st
}

// GetInputAssist returns the 输入助手 settings and the helper's status.
func (Tolk) GetInputAssist() InputAssistState { return inputAssistState() }

// SetInputAssist validates and saves the settings, then pushes them to the
// native helper and returns the resulting status.
func (Tolk) SetInputAssist(s inputassist.Config) (InputAssistState, error) {
	if err := inputassist.Validate(s); err != nil {
		return InputAssistState{}, err
	}
	p, err := legacy.DefaultPath()
	if err != nil {
		return InputAssistState{}, err
	}
	if err := legacy.Update(p, func(m map[string]any) { inputassist.Store(m, s) }); err != nil {
		return InputAssistState{}, err
	}
	macPushInputAssist()
	return inputAssistState(), nil
}

// InputAssistTrigger does what the shortcut does (the settings page's 测试).
func (Tolk) InputAssistTrigger() error { return macSimple("inputassist.trigger", nil) }

var engineTitles = map[string]string{
	"google": "Google 翻译", "microsoft": "微软翻译", "deepl": "DeepL", "openai_compat": "AI 翻译",
	"achord_builtin": "Achord 内置引擎", "apple": "Apple 本地翻译",
}

// engineIdentity names the selected engine for the helper's cache key: the
// engine id, and for AI translation the model and endpoint, never a key.
func engineIdentity(cfg map[string]any) (fingerprint, title string) {
	api := legacy.APIName(cfg)
	title = engineTitles[api]
	if title == "" {
		title = api
	}
	fingerprint = api
	if api == "openai_compat" {
		base, model, _ := legacy.OpenAICompat(cfg)
		if u, err := url.Parse(base); err == nil && u.Host != "" {
			base = u.Host + strings.TrimRight(u.Path, "/")
		} else {
			base = strings.TrimRight(base, "/")
		}
		fingerprint = api + ":" + model + "@" + base
	}
	return fingerprint, title
}

// stableFingerprint is what a translate reply reports as the engine used: the
// identity read before and after the call, only when they agree. A mismatch
// means the engine was switched mid-request, so nothing is reported and the
// helper skips its cache write.
func stableFingerprint(before, after string) string {
	if before == after {
		return after
	}
	return ""
}

func currentEngineIdentity() (fingerprint, title string) {
	cfg := map[string]any{}
	if p, err := legacy.DefaultPath(); err == nil {
		if m, err := legacy.Load(p); err == nil {
			cfg = m
		}
	}
	return engineIdentity(cfg)
}

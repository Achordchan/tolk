package macmigrate

import (
	"testing"

	"tolk/internal/engine"
)

func TestApplyMapsOldMacSettings(t *testing.T) {
	cfg := map[string]any{"translation": map[string]any{"api": "google"}}
	notes := Apply(cfg, Data{
		Defaults: map[string]any{
			"engineType": "openAICompatible", "sourceLanguageCode": "auto", "targetLanguageCode": "nl",
			"openAIBaseURL": "https://api.deepseek.com/", "openAIModel": "deepseek-chat", "openAIEndpointMode": "responses",
			"appearance": "dark", "sourceTextFontSize": 15.0, "translatedTextFontSize": 40.0,
			"doubleCopyEnabled": false, "globalHotkeyEnabled": true, "screenshotHotkeyKeyCode": 7.0,
		},
		Keys: map[string]string{"openAIAPIKey": "sk-test"},
	})
	tr := cfg["translation"].(map[string]any)
	if tr["api"] != "openai_compat" || tr["source_lang"] != "自动检测" || tr["target_lang"] != "荷兰语" {
		t.Fatalf("translation: %v", tr)
	}
	oc := cfg["openai_compat"].(map[string]any)
	p := oc["profiles"].(map[string]any)["DeepSeek"].(map[string]any)
	if oc["vendor"] != "DeepSeek" || p["api_key"] != "sk-test" || oc["api_key"] != "sk-test" || p["model"] != "deepseek-chat" {
		t.Fatalf("openai: %v", oc)
	}
	if cfg["theme"] != "dark" || cfg["display"].(map[string]any)["target_font_size"] != 28.0 {
		t.Fatalf("look: %v", cfg)
	}
	tk := cfg["tolk"].(map[string]any)
	if tk["double_copy"] != false || tk["system_hotkey"] != true || !Imported(cfg) {
		t.Fatalf("tolk: %v", tk)
	}
	if len(notes) != 1 {
		t.Fatalf("notes: %v", notes)
	}
}

func TestExistingKeyIsKept(t *testing.T) {
	cfg := map[string]any{"openai_compat": map[string]any{"vendor": "OpenAI", "profiles": map[string]any{
		"OpenAI": map[string]any{"api_key": "keep", "base_url": "https://api.openai.com/v1", "model": "m"}}}}
	Apply(cfg, Data{Defaults: map[string]any{"openAIBaseURL": "https://api.openai.com/v1"}, Keys: map[string]string{"openAIAPIKey": "new"}})
	if cfg["openai_compat"].(map[string]any)["profiles"].(map[string]any)["OpenAI"].(map[string]any)["api_key"] != "keep" {
		t.Fatal("overwrote an existing key")
	}
}

func TestEveryMacCodeIsAKnownLanguage(t *testing.T) {
	known := map[string]bool{"自动检测": true}
	for _, l := range engine.Languages {
		known[l] = true
	}
	for code, name := range codeNames {
		if !known[name] {
			t.Errorf("%s → %s is not a Tolk language", code, name)
		}
	}
	if len(codeNames) != 38 {
		t.Fatalf("Mac has 38 entries, got %d", len(codeNames))
	}
}

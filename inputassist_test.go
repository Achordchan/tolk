package main

import (
	"strings"
	"testing"
)

func TestEngineIdentityNeverLeaksKeys(t *testing.T) {
	cfg := map[string]any{
		"translation": map[string]any{"api": "openai_compat"},
		"openai_compat": map[string]any{
			"vendor": "DeepSeek", "api_key": "sk-top", "base_url": "https://old.example/v1", "model": "old",
			"profiles": map[string]any{"DeepSeek": map[string]any{"base_url": "https://api.deepseek.com/v1/", "model": "deepseek-chat", "api_key": "sk-secret"}},
		},
	}
	fp, title := engineIdentity(cfg)
	if fp != "openai_compat:deepseek-chat@api.deepseek.com/v1" || title != "AI 翻译" || strings.Contains(fp, "sk-") {
		t.Fatalf("%q %q", fp, title)
	}
	cfg["openai_compat"].(map[string]any)["profiles"].(map[string]any)["DeepSeek"].(map[string]any)["model"] = "other"
	if fp2, _ := engineIdentity(cfg); fp2 == fp {
		t.Fatal("a model change must change the fingerprint")
	}
}

func TestEngineIdentityOtherEngines(t *testing.T) {
	for api, want := range map[string]string{"google": "Google 翻译", "deepl": "DeepL", "apple": "Apple 本地翻译", "microsoft": "微软翻译"} {
		fp, title := engineIdentity(map[string]any{"translation": map[string]any{"api": api}})
		if fp != api || title != want {
			t.Errorf("%s: %q %q", api, fp, title)
		}
	}
	if fp, title := engineIdentity(map[string]any{}); fp != "google" || title != "Google 翻译" {
		t.Fatalf("%q %q", fp, title)
	}
}

func TestStableFingerprint(t *testing.T) {
	if got := stableFingerprint("google", "google"); got != "google" {
		t.Fatalf("same engine before and after: got %q", got)
	}
	if got := stableFingerprint("google", "apple"); got != "" {
		t.Fatalf("engine switched mid-request must report nothing, got %q", got)
	}
}

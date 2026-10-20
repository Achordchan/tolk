package macmigrate

import (
	"reflect"
	"testing"

	"tolk/internal/engine"
	"tolk/internal/inputassist"
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
	if tk["double_copy"] != true || tk["system_hotkey"] != true || !Imported(cfg) {
		t.Fatalf("tolk: %v", tk)
	}
	if len(notes) != 1 {
		t.Fatalf("notes: %v", notes)
	}
}

func TestRepairRevivesDoubleCopyOnceForImportedGlobalMode(t *testing.T) {
	global := Data{Defaults: map[string]any{"globalHotkeyEnabled": true, "doubleCopyEnabled": false}}
	cfg := map[string]any{"tolk": map[string]any{Done: true, "system_hotkey": true, "double_copy": false}}
	if !Repair(cfg, global) || cfg["tolk"].(map[string]any)["double_copy"] != true {
		t.Fatalf("not repaired: %v", cfg)
	}
	cfg["tolk"].(map[string]any)["double_copy"] = false // the user turned it off afterwards
	if Repair(cfg, global) || cfg["tolk"].(map[string]any)["double_copy"] != false {
		t.Fatal("repaired twice")
	}
}

func TestRepairPendingSkipsConfigsThatCannotNeedIt(t *testing.T) {
	broken := map[string]any{"tolk": map[string]any{Done: true, "system_hotkey": true, "double_copy": false}}
	if !RepairPending(broken) {
		t.Fatal("the broken combination must be pending")
	}
	for name, tk := range map[string]map[string]any{
		"already repaired": {Done: true, repairMarker: true, "system_hotkey": true, "double_copy": false},
		"never imported":   {"system_hotkey": true, "double_copy": false},
		"double copy on":   {Done: true, "system_hotkey": true, "double_copy": true},
		"clipboard mode":   {Done: true, "system_hotkey": false, "double_copy": false},
	} {
		if RepairPending(map[string]any{"tolk": tk}) {
			t.Errorf("%s: should not read the old app's settings", name)
		}
	}
}

func TestRepairLeavesConfigsThatDidNotComeFromGlobalMode(t *testing.T) {
	for name, d := range map[string]Data{
		"clipboard mode":  {Defaults: map[string]any{"globalHotkeyEnabled": false, "doubleCopyEnabled": true}},
		"both on":         {Defaults: map[string]any{"globalHotkeyEnabled": true, "doubleCopyEnabled": true}},
		"no legacy data":  {},
		"global, unknown": {Defaults: map[string]any{"globalHotkeyEnabled": true}},
	} {
		cfg := map[string]any{"tolk": map[string]any{Done: true, "system_hotkey": true, "double_copy": false}}
		Repair(cfg, d)
		if cfg["tolk"].(map[string]any)["double_copy"] != false {
			t.Errorf("%s: switched on a double copy the old app did not have in global mode", name)
		}
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

func oldInputAssist() Data {
	return Data{Defaults: map[string]any{
		"inputAssistEnabled": true, "inputAssistTargetLanguages": "ja, en,xx,auto,ja,zh-CN",
		"inputAssistShortcutKeyCode": 17.0, "inputAssistShortcutModifiers": 2304.0,
		"inputAssistAppScope": "allowlistOnly", "inputAssistBlocklist": "A\nB", "inputAssistAllowlist": "Notes",
		"inputAssistSelectionAutoShowEnabled": true, "inputAssistShowsCacheBadge": false,
	}}
}

func TestApplyMapsInputAssist(t *testing.T) {
	cfg := map[string]any{}
	Apply(cfg, oldInputAssist())
	ia := cfg["tolk"].(map[string]any)["input_assist"].(map[string]any)
	got := inputassist.FromConfig(cfg)
	want := inputassist.Config{Enabled: true, Targets: []string{"日语", "英语", "简体中文"}, ShortcutKeyCode: 17, ShortcutModifiers: 2304,
		Scope: "allowlist", Blocklist: "A\nB", Allowlist: "Notes", AutoShow: true, CacheBadge: false}
	if !reflect.DeepEqual(got, want) || ia["scope"] != "allowlist" {
		t.Fatalf("%+v", got)
	}
	if cfg["tolk"].(map[string]any)[InputAssistDone] != true || InputAssistPending(cfg) {
		t.Fatal("the full import must also settle the one-time import")
	}
}

func TestApplyWithoutInputAssistKeysWritesNothing(t *testing.T) {
	cfg := map[string]any{}
	Apply(cfg, Data{Defaults: map[string]any{"inputAssistTargetLanguages": "xx,auto", "inputAssistAppScope": "weird"}})
	if _, ok := cfg["tolk"].(map[string]any)["input_assist"]; ok {
		t.Fatalf("%v", cfg)
	}
}

func TestApplyInputAssistOnceOnlyTouchesItsKeys(t *testing.T) {
	cfg := map[string]any{"theme": "dark", "tolk": map[string]any{Done: true, "double_copy": false}}
	if !InputAssistPending(cfg) {
		t.Fatal("imported config without the marker must be pending")
	}
	d := oldInputAssist()
	d.Defaults["doubleCopyEnabled"] = true // must not be re-imported
	d.Defaults["appearance"] = "light"
	if !ApplyInputAssist(cfg, d) {
		t.Fatal("not applied")
	}
	tk := cfg["tolk"].(map[string]any)
	if tk["double_copy"] != false || cfg["theme"] != "dark" || tk[InputAssistDone] != true {
		t.Fatalf("touched other settings: %v", cfg)
	}
	if got := inputassist.FromConfig(cfg); !got.Enabled || got.Scope != "allowlist" || got.Targets[0] != "日语" {
		t.Fatalf("%+v", got)
	}
	tk["input_assist"].(map[string]any)["enabled"] = false // user turned it off afterwards
	if ApplyInputAssist(cfg, d) || tk["input_assist"].(map[string]any)["enabled"] != false || InputAssistPending(cfg) {
		t.Fatal("ran twice")
	}
}

func TestInputAssistPendingSkipsConfigsThatCannotNeedIt(t *testing.T) {
	for name, tk := range map[string]map[string]any{
		"never imported": {},
		"already done":   {Done: true, InputAssistDone: true},
	} {
		if InputAssistPending(map[string]any{"tolk": tk}) {
			t.Errorf("%s: should not read the old app's settings", name)
		}
	}
	if InputAssistPending(map[string]any{}) {
		t.Error("empty config")
	}
}

func TestApplyInputAssistKeepsExistingTolkSettings(t *testing.T) {
	cfg := map[string]any{"tolk": map[string]any{Done: true, "input_assist": map[string]any{"enabled": false, "targets": []any{"法语"}}}}
	if ApplyInputAssist(cfg, oldInputAssist()) {
		t.Fatal("overwrote a saved Tolk setting")
	}
	if got := inputassist.FromConfig(cfg); got.Enabled || got.Targets[0] != "法语" || InputAssistPending(cfg) {
		t.Fatalf("%+v", got)
	}
}

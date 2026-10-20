// Package macmigrate folds the old Mac app's settings (UserDefaults values and
// the Keychain API key, as read by tolk-helper's legacy.read) into the shared
// config file. It only reads from the old app and never touches its data, so
// going back to the old app keeps working.
package macmigrate

import (
	"strings"

	"tolk/internal/inputassist"
	"tolk/internal/legacy"
)

// Data is the legacy.read result.
type Data struct {
	Defaults  map[string]any    `json:"defaults"`
	Keys      map[string]string `json:"keys"`
	Languages map[string]string `json:"languages"`
}

// codeNames maps the Mac app's language codes to Tolk's labels.
var codeNames = map[string]string{
	"auto": "自动检测", "zh-CN": "简体中文", "zh-TW": "繁体中文", "en": "英语", "ja": "日语", "ko": "韩语",
	"it": "意大利语", "fr": "法语", "de": "德语", "es": "西班牙语", "pt": "葡萄牙语", "nl": "荷兰语",
	"sv": "瑞典语", "da": "丹麦语", "no": "挪威语", "fi": "芬兰语", "pl": "波兰语", "cs": "捷克语",
	"hu": "匈牙利语", "ro": "罗马尼亚语", "bg": "保加利亚语", "el": "希腊语", "uk": "乌克兰语",
	"ru": "俄语", "tr": "土耳其语", "ar": "阿拉伯语", "he": "希伯来语", "fa": "波斯语", "hi": "印地语",
	"bn": "孟加拉语", "ta": "泰米尔语", "th": "泰语", "vi": "越南语", "id": "印尼语", "ms": "马来语",
	"fil": "菲律宾语", "ur": "乌尔都语", "sw": "斯瓦希里语",
}

var engines = map[string]string{
	"apple": "apple", "google": "google", "microsoft": "microsoft", "openAICompatible": "openai_compat",
}

// vendor presets by base URL, mirroring the settings sheet.
var vendorByBase = map[string]string{
	"https://open.bigmodel.cn/api/paas/v4":                    "智谱",
	"https://api.openai.com/v1":                               "OpenAI",
	"https://api.deepseek.com":                                "DeepSeek",
	"https://dashscope.aliyuncs.com/compatible-mode/v1":       "通义千问(Qwen)",
	"https://ark.cn-beijing.volces.com/api/v3":                "字节豆包",
	"https://generativelanguage.googleapis.com/v1beta/openai": "Google Gemini",
}

// Done is the marker kept under "tolk" once the import ran.
const Done = "mac_imported"

// Imported reports whether the config already carries the import marker.
func Imported(cfg map[string]any) bool {
	t, _ := cfg["tolk"].(map[string]any)
	v, _ := t[Done].(bool)
	return v
}

// repairMarker is kept under "tolk" once Repair ran.
const repairMarker = "mac_repair_double_copy"

// RepairPending reports whether the config could still need Repair: it was
// imported, the repair has not run, and it has the broken combination. It
// lets the caller skip reading the old app's settings (which touches the
// Keychain) when the answer is already no.
func RepairPending(cfg map[string]any) bool {
	t, _ := cfg["tolk"].(map[string]any)
	if done, _ := t[repairMarker].(bool); done {
		return false
	}
	imported, _ := t[Done].(bool)
	sys, _ := t["system_hotkey"].(bool)
	dc, ok := t["double_copy"].(bool)
	return imported && sys && ok && !dc
}

// RepairDone reports whether the repair marker is already set.
func RepairDone(cfg map[string]any) bool {
	t, _ := cfg["tolk"].(map[string]any)
	done, _ := t[repairMarker].(bool)
	return done
}

// Repair fixes a config imported before double_copy followed the old app's
// global mode: system_hotkey on with double_copy off, which left ⌘C⌘C dead.
// It only acts when the old app's own settings show that mode (global hotkey
// on, clipboard mode off), so a config that never came from that import is
// left alone. Until now the broken import and a deliberate "off" looked the
// same, because double copy did nothing either way; a user who really wants
// it off can switch it off again. It runs once and reports whether it
// changed the config.
func Repair(cfg map[string]any, d Data) bool {
	t, _ := cfg["tolk"].(map[string]any)
	if t == nil {
		return false
	}
	if done, _ := t[repairMarker].(bool); done {
		return false
	}
	t[repairMarker] = true
	if imported, _ := t[Done].(bool); !imported {
		return true
	}
	global, _ := d.Defaults["globalHotkeyEnabled"].(bool)
	clipboard, ok := d.Defaults["doubleCopyEnabled"].(bool)
	if !global || !ok || clipboard {
		return true
	}
	if sys, _ := t["system_hotkey"].(bool); sys {
		if dc, ok := t["double_copy"].(bool); ok && !dc {
			t["double_copy"] = true
		}
	}
	return true
}

func str(v any) string { s, _ := v.(string); return strings.TrimSpace(s) }

func num(v any) (float64, bool) { f, ok := v.(float64); return f, ok }

// Apply merges d into cfg and returns notes for anything that could not carry
// over. Values already present in cfg for secrets are kept.
func Apply(cfg map[string]any, d Data) (notes []string) {
	t := legacy.Sub(cfg, "tolk")
	if e, ok := engines[str(d.Defaults["engineType"])]; ok {
		legacy.Sub(cfg, "translation")["api"] = e
	}
	label := func(code string) string {
		if n := d.Languages[code]; n != "" {
			return n
		}
		return codeNames[code]
	}
	tr := legacy.Sub(cfg, "translation")
	if n := label(str(d.Defaults["sourceLanguageCode"])); n != "" {
		tr["source_lang"] = n
	}
	if n := label(str(d.Defaults["targetLanguageCode"])); n != "" && n != "自动检测" {
		tr["target_lang"] = n
	}

	base, model, key := str(d.Defaults["openAIBaseURL"]), str(d.Defaults["openAIModel"]), strings.TrimSpace(d.Keys["openAIAPIKey"])
	if base != "" || model != "" || key != "" {
		vendor := vendorByBase[strings.TrimRight(base, "/")]
		if vendor == "" {
			vendor = "自定义"
		}
		oc := legacy.Sub(cfg, "openai_compat")
		prof := legacy.Sub(legacy.Sub(oc, "profiles"), vendor)
		if str(prof["base_url"]) == "" {
			prof["base_url"] = base
		}
		if str(prof["model"]) == "" {
			prof["model"] = model
		}
		if str(prof["api_key"]) == "" && key != "" {
			prof["api_key"] = key
		}
		if str(oc["vendor"]) == "" {
			oc["vendor"] = vendor
			oc["base_url"], oc["model"] = prof["base_url"], prof["model"]
			if k, ok := prof["api_key"]; ok {
				oc["api_key"] = k
			}
		}
	}
	if str(d.Defaults["openAIEndpointMode"]) == "responses" {
		notes = append(notes, "旧版的 OpenAI「Responses」接口模式暂不支持，已按 Chat Completions 处理；如服务商不支持请换用兼容 Chat Completions 的地址")
	}

	switch str(d.Defaults["appearance"]) {
	case "light", "dark":
		cfg["theme"] = str(d.Defaults["appearance"])
	}
	disp := legacy.Sub(cfg, "display")
	clamp := func(f float64) float64 {
		if f < 12 {
			return 12
		}
		if f > 28 {
			return 28
		}
		return float64(int(f + 0.5))
	}
	if f, ok := num(d.Defaults["sourceTextFontSize"]); ok {
		disp["source_font_size"] = clamp(f)
	}
	if f, ok := num(d.Defaults["translatedTextFontSize"]); ok {
		disp["target_font_size"] = clamp(f)
	}

	if b, ok := d.Defaults["doubleCopyEnabled"].(bool); ok {
		t["double_copy"] = b
	}
	if f, ok := num(d.Defaults["doubleCopyWindowMs"]); ok && f >= 200 && f <= 2000 {
		t["double_copy_ms"] = f
	}
	if b, ok := d.Defaults["globalHotkeyEnabled"].(bool); ok {
		t["system_hotkey"] = b
		// The old app turned "global" mode on and the clipboard mode off
		// (doubleCopyEnabled = false), and either one meant "⌘C⌘C translates".
		// In Tolk double_copy is the one switch for both, so it must stay on.
		if b {
			t["double_copy"] = true
		}
	}
	if b, ok := d.Defaults["miniModeEnabled"].(bool); ok {
		t["mini_mode"] = b
	}
	if b, ok := d.Defaults["screenshotHotkeyEnabled"].(bool); ok {
		t["screenshot_hotkey"] = b
	}
	if f, ok := num(d.Defaults["screenshotHotkeyKeyCode"]); ok && f >= 0 && f < 128 {
		t["screenshot_key_code"] = f
	}
	applyInputAssist(cfg, d)
	t[Done] = true
	t[InputAssistDone] = true
	return notes
}

// InputAssistDone is the marker kept under "tolk" once the old app's
// input-assist settings were imported (by Apply or by ApplyInputAssist).
const InputAssistDone = "mac_input_assist_imported"

// InputAssistPending reports whether the config was imported before the
// input-assist settings existed and still lacks them. It lets the caller skip
// reading the old app's settings (which touches the Keychain) when the answer
// is already no.
func InputAssistPending(cfg map[string]any) bool {
	t, _ := cfg["tolk"].(map[string]any)
	done, _ := t[InputAssistDone].(bool)
	imported, _ := t[Done].(bool)
	return imported && !done
}

// ApplyInputAssist is the one-time import for configs that already ran the
// main import: it touches nothing but tolk.input_assist and its marker, and
// leaves an input_assist object the user may already have saved alone. It
// reports whether it did anything.
func ApplyInputAssist(cfg map[string]any, d Data) bool {
	if !InputAssistPending(cfg) {
		return false
	}
	t := legacy.Sub(cfg, "tolk")
	t[InputAssistDone] = true
	if _, exists := t[inputassist.Key].(map[string]any); exists {
		return false
	}
	return applyInputAssist(cfg, d)
}

// applyInputAssist maps the old InputAssistSettings keys into
// tolk.input_assist, only the ones present and valid.
func applyInputAssist(cfg map[string]any, d Data) bool {
	m := map[string]any{}
	if b, ok := d.Defaults["inputAssistEnabled"].(bool); ok {
		m["enabled"] = b
	}
	if raw, ok := d.Defaults["inputAssistTargetLanguages"].(string); ok {
		var labels []string
		for _, code := range strings.Split(raw, ",") {
			code = strings.TrimSpace(code)
			n := d.Languages[code]
			if n == "" {
				n = codeNames[code]
			}
			if n != "" && n != "自动检测" {
				labels = append(labels, n)
			}
		}
		if got := inputassist.SanitizeTargets(labels); len(labels) > 0 {
			m["targets"] = got
		}
	}
	if f, ok := num(d.Defaults["inputAssistShortcutKeyCode"]); ok && f >= 0 && f <= 127 && f == float64(int(f)) {
		m["shortcut_key_code"] = int(f)
	}
	if f, ok := num(d.Defaults["inputAssistShortcutModifiers"]); ok && f >= 0 && f == float64(int(f)) {
		m["shortcut_modifiers"] = int(f)
	}
	switch str(d.Defaults["inputAssistAppScope"]) {
	case "globalWithBlocklist":
		m["scope"] = inputassist.ScopeGlobal
	case "allowlistOnly":
		m["scope"] = inputassist.ScopeAllowlist
	}
	if v, ok := d.Defaults["inputAssistBlocklist"].(string); ok {
		m["blocklist"] = v
	}
	if v, ok := d.Defaults["inputAssistAllowlist"].(string); ok {
		m["allowlist"] = v
	}
	if b, ok := d.Defaults["inputAssistSelectionAutoShowEnabled"].(bool); ok {
		m["auto_show"] = b
	}
	if b, ok := d.Defaults["inputAssistShowsCacheBadge"].(bool); ok {
		m["cache_badge"] = b
	}
	if len(m) == 0 {
		return false
	}
	dst := legacy.Sub(legacy.Sub(cfg, "tolk"), inputassist.Key)
	for k, v := range m {
		dst[k] = v
	}
	return true
}

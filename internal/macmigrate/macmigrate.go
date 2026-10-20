// Package macmigrate folds the old Mac app's settings (UserDefaults values and
// the Keychain API key, as read by tolk-helper's legacy.read) into the shared
// config file. It only reads from the old app and never touches its data, so
// going back to the old app keeps working.
package macmigrate

import (
	"strings"

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
	t[Done] = true
	return notes
}

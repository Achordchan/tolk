package main

import (
	"fmt"
	"strings"

	"tolk/internal/legacy"
)

// Vendor presets, the same as dazuo-win's VENDOR_DEFAULTS.
type Vendor struct {
	Name    string `json:"name"`
	BaseURL string `json:"baseUrl"`
	Model   string `json:"model"`
}

var vendors = []Vendor{
	{"智谱", "https://open.bigmodel.cn/api/paas/v4", "glm-4-flash"},
	{"OpenAI", "https://api.openai.com/v1", "gpt-4o-mini"},
	{"DeepSeek", "https://api.deepseek.com", ""},
	{"通义千问(Qwen)", "https://dashscope.aliyuncs.com/compatible-mode/v1", ""},
	{"字节豆包", "https://ark.cn-beijing.volces.com/api/v3", ""},
	{"Google Gemini", "https://generativelanguage.googleapis.com/v1beta/openai", ""},
	{"自定义", "", ""},
}

// Profile is one AI vendor's saved settings; the key never leaves Go.
type Profile struct {
	BaseURL string `json:"baseUrl"`
	Model   string `json:"model"`
	HasKey  bool   `json:"hasKey"`
}

// Settings is what the settings sheet shows.
type Settings struct {
	Engine          string             `json:"engine"`
	Vendor          string             `json:"vendor"`
	Profiles        map[string]Profile `json:"profiles"`
	DeepLHasKey     bool               `json:"deeplHasKey"`
	MicrosoftHasKey bool               `json:"microsoftHasKey"`
	MicrosoftRegion string             `json:"microsoftRegion"`
	Vendors         []Vendor           `json:"vendors"`
	Prefs           Prefs              `json:"prefs"`
}

// SettingsUpdate is what the sheet sends back. An empty key keeps the saved one.
type SettingsUpdate struct {
	Engine          string `json:"engine"`
	Vendor          string `json:"vendor"`
	BaseURL         string `json:"baseUrl"`
	Model           string `json:"model"`
	AIKey           string `json:"aiKey"`
	DeepLKey        string `json:"deeplKey"`
	MicrosoftKey    string `json:"microsoftKey"`
	MicrosoftRegion string `json:"microsoftRegion"`
}

func cfgPath() (string, error) { return legacy.DefaultPath() }

func hasKey(m map[string]any, k string) bool {
	s, _ := m[k].(string)
	return strings.TrimSpace(s) != ""
}

// GetSettings returns the current settings without any secret.
func (Tolk) GetSettings() (Settings, error) {
	p, err := cfgPath()
	if err != nil {
		return Settings{}, err
	}
	m, err := legacy.Load(p)
	if err != nil {
		m = map[string]any{}
	}
	oc, _ := m["openai_compat"].(map[string]any)
	dl, _ := m["deepl"].(map[string]any)
	ms, _ := m["microsoft"].(map[string]any)
	s := Settings{
		Engine:          legacy.APIName(m),
		Vendor:          "智谱",
		Profiles:        map[string]Profile{},
		DeepLHasKey:     hasKey(dl, "api_key"),
		MicrosoftHasKey: hasKey(ms, "api_key"),
		MicrosoftRegion: strings.TrimSpace(fmt.Sprint(valOr(ms, "region"))),
		Vendors:         vendors,
		Prefs:           loadPrefs(),
	}
	if v, _ := oc["vendor"].(string); v != "" {
		s.Vendor = v
	}
	if ps, ok := oc["profiles"].(map[string]any); ok {
		for name, raw := range ps {
			if pm, ok := raw.(map[string]any); ok {
				b, _ := pm["base_url"].(string)
				mo, _ := pm["model"].(string)
				s.Profiles[name] = Profile{BaseURL: b, Model: mo, HasKey: hasKey(pm, "api_key")}
			}
		}
	}
	// A legacy config without profiles keeps its values at the top level.
	if _, ok := s.Profiles[s.Vendor]; !ok && oc != nil {
		b, _ := oc["base_url"].(string)
		mo, _ := oc["model"].(string)
		s.Profiles[s.Vendor] = Profile{BaseURL: b, Model: mo, HasKey: hasKey(oc, "api_key")}
	}
	return s, nil
}

func valOr(m map[string]any, k string) any {
	if v, ok := m[k]; ok && v != nil {
		return v
	}
	return ""
}

var engineNames = map[string]bool{"google": true, "microsoft": true, "deepl": true, "openai_compat": true, "achord_builtin": true, "apple": true}

// SaveSettings writes the settings back to the shared config file.
func (Tolk) SaveSettings(u SettingsUpdate) error {
	if !engineNames[u.Engine] {
		return fmt.Errorf("未知的翻译服务：%s", u.Engine)
	}
	p, err := cfgPath()
	if err != nil {
		return err
	}
	return legacy.Update(p, func(m map[string]any) {
		legacy.Sub(m, "translation")["api"] = u.Engine
		if k := strings.TrimSpace(u.DeepLKey); k != "" {
			legacy.Sub(m, "deepl")["api_key"] = k
		}
		ms := legacy.Sub(m, "microsoft")
		ms["region"] = strings.TrimSpace(u.MicrosoftRegion)
		if k := strings.TrimSpace(u.MicrosoftKey); k != "" {
			ms["api_key"] = k
		}
		if strings.TrimSpace(u.Vendor) == "" {
			return
		}
		oc := legacy.Sub(m, "openai_compat")
		profiles := legacy.Sub(oc, "profiles")
		prof := legacy.Sub(profiles, u.Vendor)
		prof["base_url"] = strings.TrimSpace(u.BaseURL)
		prof["model"] = strings.TrimSpace(u.Model)
		if k := strings.TrimSpace(u.AIKey); k != "" {
			prof["api_key"] = k
		}
		// dazuo-win also mirrors the active vendor at the top level.
		oc["vendor"] = u.Vendor
		oc["base_url"], oc["model"] = prof["base_url"], prof["model"]
		if k, ok := prof["api_key"]; ok {
			oc["api_key"] = k
		} else {
			delete(oc, "api_key")
		}
	})
}

// SetEngine switches the active service from the main window's picker.
func (Tolk) SetEngine(engine string) error {
	if !engineNames[engine] {
		return fmt.Errorf("未知的翻译服务：%s", engine)
	}
	p, err := cfgPath()
	if err != nil {
		return err
	}
	return legacy.Update(p, func(m map[string]any) { legacy.Sub(m, "translation")["api"] = engine })
}

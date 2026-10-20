package legacy

import (
	"fmt"
	"strings"
	"sync"

	"tolk/internal/engine"
)

// APIName returns translation.api from a legacy config, "google" if unset.
func APIName(m map[string]any) string {
	if t, ok := m["translation"].(map[string]any); ok {
		if s, _ := t["api"].(string); s != "" {
			return s
		}
	}
	return "google"
}

func str(m map[string]any, k string) string {
	s, _ := m[k].(string)
	return strings.TrimSpace(s)
}

var (
	extraMu sync.Mutex
	extra   = map[string]func() (engine.Engine, error){}
)

// Register adds a platform engine (Apple's local translation on macOS).
func Register(name string, build func() (engine.Engine, error)) {
	extraMu.Lock()
	defer extraMu.Unlock()
	extra[name] = build
}

// BuildEngine creates the engine named by api from a legacy config map.
// Keys stay on the Go side; nothing here is sent to the page.
func BuildEngine(m map[string]any, api string) (engine.Engine, error) {
	switch api {
	case "google":
		return engine.NewGoogle(), nil
	case "deepl":
		d, _ := m["deepl"].(map[string]any)
		return engine.NewDeepL(str(d, "api_key"))
	case "openai_compat":
		oc, _ := m["openai_compat"].(map[string]any)
		base, model, key := str(oc, "base_url"), str(oc, "model"), str(oc, "api_key")
		// Per-vendor profiles win, as in the Python app.
		if vendor := str(oc, "vendor"); vendor != "" {
			if ps, ok := oc["profiles"].(map[string]any); ok {
				if p, ok := ps[vendor].(map[string]any); ok {
					if v := str(p, "base_url"); v != "" {
						base = v
					}
					if v := str(p, "model"); v != "" {
						model = v
					}
					// The key comes from the selected profile only: falling
					// back to the mirrored top-level key would send another
					// vendor's credential to this endpoint.
					key = str(p, "api_key")
				}
			}
		}
		return engine.NewOpenAICompat(base, model, key)
	case "microsoft":
		ms, _ := m["microsoft"].(map[string]any)
		return engine.NewMicrosoft(str(ms, "api_key"), str(ms, "region")), nil
	case "achord_builtin":
		return engine.NewBuiltin(), nil
	}
	extraMu.Lock()
	build := extra[api]
	extraMu.Unlock()
	if build != nil {
		return build()
	}
	if api == "apple" {
		return nil, fmt.Errorf("Apple 本地翻译只在 macOS 上可用，请在设置里换一个翻译服务")
	}
	return nil, fmt.Errorf("未知的翻译服务：%s", api)
}

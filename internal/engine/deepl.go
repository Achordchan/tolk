package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

var (
	deeplTarget = codes(func(l lang) string { return l.dlTgt })
	deeplSource = codes(func(l lang) string { return l.dlSrc })
)

// DeepL uses the official API; keys ending in ":fx" are the free plan.
type DeepL struct {
	Client  *http.Client
	BaseURL string
	APIKey  string
}

func NewDeepL(apiKey string) (*DeepL, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("未设置DeepL API密钥")
	}
	base := "https://api.deepl.com"
	if strings.HasSuffix(apiKey, ":fx") {
		base = "https://api-free.deepl.com"
	}
	return &DeepL{Client: defaultClient(), BaseURL: base, APIKey: apiKey}, nil
}

func (d *DeepL) Translate(ctx context.Context, text, source, target string) (Result, error) {
	if strings.TrimSpace(text) == "" {
		return Result{}, ErrEmpty
	}
	tl, ok := deeplTarget[target]
	if !ok {
		return Result{}, unsupported("DeepL", "目标", target)
	}
	payload := map[string]any{"text": []string{text}, "target_lang": tl, "preserve_formatting": true}
	if source != Auto {
		sl, ok := deeplSource[source]
		if !ok {
			return Result{}, unsupported("DeepL", "源", source)
		}
		payload["source_lang"] = sl
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.BaseURL+"/v2/translate", bytes.NewReader(body))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Authorization", "DeepL-Auth-Key "+d.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.Client.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("无法连接到 DeepL 服务，请检查网络连接或代理设置")
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(raw, &e)
		return Result{}, fmt.Errorf("DeepL翻译失败: HTTP %d %s", resp.StatusCode, e.Message)
	}
	var out struct {
		Translations []struct {
			Text     string `json:"text"`
			Detected string `json:"detected_source_language"`
		} `json:"translations"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || len(out.Translations) == 0 {
		return Result{}, fmt.Errorf("DeepL未返回翻译结果")
	}
	return Result{Text: strings.TrimSpace(out.Translations[0].Text), Detected: out.Translations[0].Detected}, nil
}

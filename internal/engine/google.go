package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

var googleCodes = map[string]string{
	"简体中文": "zh-CN", "繁体中文": "zh-TW", "英语": "en", "日语": "ja", "韩语": "ko",
	"法语": "fr", "德语": "de", "西班牙语": "es", "俄语": "ru", "意大利语": "it",
	"葡萄牙语": "pt", "越南语": "vi", "泰语": "th", "阿拉伯语": "ar", Auto: "auto",
}

// Google uses the unofficial web endpoints, with the same fallback as the
// Python app: translate.googleapis.com first, clients5.google.com second.
type Google struct {
	Client      *http.Client
	PrimaryURL  string
	FallbackURL string
}

// NewGoogle returns a Google engine with the public endpoints.
func NewGoogle() *Google {
	return &Google{
		Client:      defaultClient(),
		PrimaryURL:  "https://translate.googleapis.com/translate_a/single",
		FallbackURL: "https://clients5.google.com/translate_a/t",
	}
}

func (g *Google) Translate(ctx context.Context, text, source, target string) (Result, error) {
	if strings.TrimSpace(text) == "" {
		return Result{}, ErrEmpty
	}
	sl, ok := googleCodes[source]
	if !ok {
		sl = "auto"
	}
	tl, ok := googleCodes[target]
	if !ok {
		tl = "zh-CN"
	}
	res, err := g.primary(ctx, text, sl, tl)
	if err == nil {
		return res, nil
	}
	res2, err2 := g.fallback(ctx, text, sl, tl)
	if err2 == nil {
		return res2, nil
	}
	return Result{}, fmt.Errorf("Google 翻译失败（需要能访问海外网站的网络）: %w", err)
}

func (g *Google) get(ctx context.Context, base string, q url.Values) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Referer", "https://translate.google.com/")
	resp, err := g.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func (g *Google) primary(ctx context.Context, text, sl, tl string) (Result, error) {
	q := url.Values{"client": {"gtx"}, "sl": {sl}, "tl": {tl}, "dt": {"t", "ld"}, "q": {text}}
	b, err := g.get(ctx, g.PrimaryURL, q)
	if err != nil {
		return Result{}, err
	}
	return parseGooglePrimary(b)
}

func (g *Google) fallback(ctx context.Context, text, sl, tl string) (Result, error) {
	q := url.Values{"client": {"dict-chrome-ex"}, "sl": {sl}, "tl": {tl}, "q": {text}}
	b, err := g.get(ctx, g.FallbackURL, q)
	if err != nil {
		return Result{}, err
	}
	return parseGoogleFallback(b)
}

func parseGooglePrimary(b []byte) (Result, error) {
	var data []any
	if err := json.Unmarshal(b, &data); err != nil || len(data) == 0 {
		return Result{}, fmt.Errorf("Google 返回了无法识别的结果")
	}
	parts, ok := data[0].([]any)
	if !ok {
		return Result{}, fmt.Errorf("Google 返回了无法识别的结果")
	}
	var sb strings.Builder
	for _, p := range parts {
		if item, ok := p.([]any); ok && len(item) > 0 {
			if s, ok := item[0].(string); ok {
				sb.WriteString(s)
			}
		}
	}
	res := Result{Text: strings.TrimSpace(sb.String())}
	if len(data) > 2 {
		res.Detected, _ = data[2].(string)
	}
	if res.Text == "" {
		return Result{}, fmt.Errorf("Google 返回了空结果")
	}
	return res, nil
}

func parseGoogleFallback(b []byte) (Result, error) {
	var data []any
	if err := json.Unmarshal(b, &data); err != nil || len(data) == 0 {
		return Result{}, fmt.Errorf("Google 返回了无法识别的结果")
	}
	switch first := data[0].(type) {
	case []any: // sl=auto: [[text, detected]]
		var res Result
		if len(first) > 0 {
			s, _ := first[0].(string)
			res.Text = strings.TrimSpace(s)
		}
		if len(first) > 1 {
			res.Detected, _ = first[1].(string)
		}
		if res.Text != "" {
			return res, nil
		}
	case string:
		if t := strings.TrimSpace(first); t != "" {
			return Result{Text: t}, nil
		}
	}
	return Result{}, fmt.Errorf("Google 返回了无法识别的结果")
}

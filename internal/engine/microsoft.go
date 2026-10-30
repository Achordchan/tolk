package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

var microsoftCodes = func() map[string]string {
	m := codes(func(l lang) string { return l.ms })
	m[Auto] = "auto-detect"
	return m
}()

var (
	igRe     = regexp.MustCompile(`IG:"([^"]+)"`)
	iidRe    = regexp.MustCompile(`data-iid="([^"]+)"`)
	paramsRe = regexp.MustCompile(`params_AbusePreventionHelper\s*=\s*(\[[^\]]+\])`)
)

const (
	webChunkLimit = 1000
	edgeUA        = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36 Edg/124.0.0.0"
)

// Microsoft uses the free Bing translator web endpoint. There is no key mode.
type Microsoft struct {
	Client   *http.Client
	EntryURL string

	mu      sync.Mutex
	host    string
	ig, iid string
	key     int64
	token   string
	expires time.Time
}

// NewMicrosoft returns the free web-mode engine.
func NewMicrosoft() *Microsoft {
	jar, _ := cookiejar.New(nil)
	return &Microsoft{
		Client:   &http.Client{Timeout: 20 * time.Second, Jar: jar},
		EntryURL: "https://www.bing.com/translator",
	}
}

// Warm fetches the web token if it is missing or old and keeps the connection open.
func (m *Microsoft) Warm(ctx context.Context) error {
	if err := m.refresh(ctx, false); err != nil {
		return err
	}
	m.mu.Lock()
	host := m.host
	m.mu.Unlock()
	if strings.HasPrefix(m.EntryURL, "http://") {
		return nil // tests
	}
	return warmHost(ctx, m.Client, "https://"+host+"/translator")
}

func (m *Microsoft) Translate(ctx context.Context, text, source, target string) (Result, error) {
	if strings.TrimSpace(text) == "" {
		return Result{}, ErrEmpty
	}
	tl, ok := microsoftCodes[target]
	if !ok || tl == "auto-detect" {
		return Result{}, unsupported("微软翻译", "目标", target)
	}
	sl, ok := microsoftCodes[source]
	if !ok {
		return Result{}, unsupported("微软翻译", "源", source)
	}
	limit, one := webChunkLimit, m.webChunk
	var parts []chunkPart
	var detected string
	for _, chunk := range splitText(text, limit) {
		core := strings.TrimSpace(chunk)
		if core == "" {
			parts = append(parts, chunkPart{"", chunk, ""})
			continue
		}
		lead := chunk[:len(chunk)-len(strings.TrimLeft(chunk, " \t\r\n"))]
		trail := chunk[len(strings.TrimRight(chunk, " \t\r\n")):]
		res, err := one(ctx, core, sl, tl)
		if err != nil {
			return Result{}, err
		}
		parts = append(parts, chunkPart{lead, strings.TrimSpace(res.Text), trail})
		if detected == "" {
			detected = res.Detected
		}
	}
	return Result{Text: strings.TrimSpace(joinChunks(parts)), Detected: detected}, nil
}

// ---- web mode ----

func parseWebPage(html string) (ig, iid string, key int64, token string, expiry time.Duration, err error) {
	a, b, c := igRe.FindStringSubmatch(html), iidRe.FindStringSubmatch(html), paramsRe.FindStringSubmatch(html)
	if a == nil || b == nil || c == nil {
		return "", "", 0, "", 0, fmt.Errorf("微软翻译页面结构已变化，暂时无法获取访问令牌，请稍后重试")
	}
	var params []any
	if json.Unmarshal([]byte(c[1]), &params) != nil || len(params) < 2 {
		return "", "", 0, "", 0, fmt.Errorf("微软翻译访问令牌解析失败，请稍后重试")
	}
	key = int64(toFloat(params[0]))
	token, _ = params[1].(string)
	ms := 3600000.0
	if len(params) > 2 {
		ms = toFloat(params[2])
	}
	return a[1], b[1], key, token, time.Duration(ms) * time.Millisecond, nil
}

func toFloat(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case string:
		f, _ := strconv.ParseFloat(x, 64)
		return f
	}
	return 0
}

func (m *Microsoft) refresh(ctx context.Context, force bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !force && m.token != "" && time.Now().Before(m.expires) {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.EntryURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", edgeUA)
	resp, err := m.Client.Do(req)
	if err != nil {
		return fmt.Errorf("无法连接到微软翻译，请检查网络连接或代理设置")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("微软翻译返回错误：HTTP %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	ig, iid, key, token, expiry, err := parseWebPage(string(body))
	if err != nil {
		return err
	}
	m.host, m.ig, m.iid, m.key, m.token = resp.Request.URL.Host, ig, iid, key, token
	wait := expiry - time.Minute
	if wait < time.Minute {
		wait = time.Minute
	}
	m.expires = time.Now().Add(wait)
	return nil
}

func (m *Microsoft) webChunk(ctx context.Context, text, sl, tl string) (Result, error) {
	if err := m.refresh(ctx, false); err != nil {
		return Result{}, err
	}
	for attempt := 0; attempt < 2; attempt++ {
		m.mu.Lock()
		host, ig, iid, key, token := m.host, m.ig, m.iid, m.key, m.token
		m.mu.Unlock()
		endpoint := fmt.Sprintf("https://%s/ttranslatev3?%s", host,
			url.Values{"isVertical": {"1"}, "IG": {ig}, "IID": {iid}}.Encode())
		if strings.HasPrefix(m.EntryURL, "http://") { // tests point the entry at a local server
			endpoint = fmt.Sprintf("http://%s/ttranslatev3?%s", host,
				url.Values{"isVertical": {"1"}, "IG": {ig}, "IID": {iid}}.Encode())
		}
		form := url.Values{"fromLang": {sl}, "to": {tl}, "text": {text}, "token": {token},
			"key": {strconv.FormatInt(key, 10)}, "tryFetchingGenderDebiasedTranslations": {"true"}}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
		if err != nil {
			return Result{}, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("User-Agent", edgeUA)
		req.Header.Set("Referer", "https://"+host+"/translator")
		resp, err := m.Client.Do(req)
		if err != nil {
			return Result{}, fmt.Errorf("无法连接到微软翻译，请检查网络连接或代理设置")
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		if (resp.StatusCode == 200 || resp.StatusCode == 205) && len(bytes.TrimSpace(body)) == 0 {
			if attempt == 0 {
				if err := m.refresh(ctx, true); err != nil {
					return Result{}, err
				}
				continue
			}
			return Result{}, fmt.Errorf("微软翻译未返回结果，请稍后重试")
		}
		if resp.StatusCode != http.StatusOK {
			return Result{}, statusError("微软翻译", resp.StatusCode)
		}
		res, expired, err := parseMicrosoft(body)
		if expired && attempt == 0 {
			if err := m.refresh(ctx, true); err != nil {
				return Result{}, err
			}
			continue
		}
		return res, err
	}
	return Result{}, fmt.Errorf("微软翻译请求失败，请稍后重试")
}

// parseMicrosoft reads the response of the web endpoint.
// expired is true when the web token was rejected (statusCode 205).
func parseMicrosoft(b []byte) (res Result, expired bool, err error) {
	var obj map[string]any
	if json.Unmarshal(b, &obj) == nil {
		if sc := toFloat(obj["statusCode"]); sc != 0 {
			return Result{}, sc == 205, fmt.Errorf("微软翻译返回状态 %d", int(sc))
		}
		if e, ok := obj["error"].(map[string]any); ok {
			return Result{}, false, fmt.Errorf("微软翻译返回错误：%v", e["message"])
		}
	}
	var arr []struct {
		Detected struct {
			Language string `json:"language"`
		} `json:"detectedLanguage"`
		Translations []struct {
			Text string `json:"text"`
		} `json:"translations"`
	}
	if json.Unmarshal(b, &arr) != nil || len(arr) == 0 {
		return Result{}, false, fmt.Errorf("微软翻译返回了无法识别的结果")
	}
	if len(arr[0].Translations) == 0 {
		return Result{}, false, fmt.Errorf("微软翻译未返回翻译结果")
	}
	return Result{Text: arr[0].Translations[0].Text, Detected: arr[0].Detected.Language}, false, nil
}

func statusError(service string, status int) error {
	switch {
	case status == 429:
		return fmt.Errorf("%s暂时限制了当前网络的访问（HTTP 429），请稍后再试", service)
	case status == 401 || status == 403:
		return fmt.Errorf("%s认证失败（HTTP %d），请检查 API Key 是否正确", service, status)
	case status >= 500:
		return fmt.Errorf("%s服务端暂时不可用（HTTP %d），请稍后重试", service, status)
	}
	return fmt.Errorf("%s返回错误：HTTP %d", service, status)
}

// ---- chunking ----

type chunkPart struct{ lead, text, trail string }

func isCJK(r rune) bool {
	switch {
	case r >= 0x2E80 && r <= 0x2FFF, r >= 0x3000 && r <= 0x303F, r >= 0x3040 && r <= 0x30FF,
		r >= 0x3100 && r <= 0x312F, r >= 0x3400 && r <= 0x4DBF, r >= 0x4E00 && r <= 0x9FFF,
		r >= 0xAC00 && r <= 0xD7AF, r >= 0xF900 && r <= 0xFAFF, r >= 0xFF00 && r <= 0xFFEF,
		r >= 0x20000 && r <= 0x3134F:
		return true
	}
	return false
}

func isSpace(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '\r' }

// joinChunks puts translated chunks back together, restoring the source's
// boundary whitespace and adding a space between two non-CJK sides.
func joinChunks(parts []chunkPart) string {
	var sb strings.Builder
	for _, p := range parts {
		seg := p.lead + p.text + p.trail
		if seg == "" {
			continue
		}
		if sb.Len() > 0 {
			prev, _ := utf8.DecodeLastRuneInString(sb.String())
			next, _ := utf8.DecodeRuneInString(seg)
			if !isSpace(prev) && !isSpace(next) && !isCJK(prev) && !isCJK(next) {
				sb.WriteByte(' ')
			}
		}
		sb.WriteString(seg)
	}
	return sb.String()
}

// splitText cuts text into pieces of at most limit characters, preferring
// newlines, sentence ends and spaces.
func splitText(text string, limit int) []string {
	r := []rune(text)
	if limit <= 0 || len(r) <= limit {
		if text == "" {
			return nil
		}
		return []string{text}
	}
	seps := []string{"\n", "。", "！", "？", ". ", "! ", "? ", "；", "; ", "，", ", ", " "}
	var out []string
	for len(r) > limit {
		window := string(r[:limit])
		cut := -1
		for _, s := range seps {
			if i := strings.LastIndex(window, s); i >= 0 {
				if pos := utf8.RuneCountInString(window[:i]); pos > limit/4 {
					cut = pos + utf8.RuneCountInString(s)
					break
				}
			}
		}
		if cut <= 0 {
			cut = limit
		}
		out = append(out, string(r[:cut]))
		r = r[cut:]
	}
	if len(r) > 0 {
		out = append(out, string(r))
	}
	return out
}

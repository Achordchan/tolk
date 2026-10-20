package engine

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/OwO-Network/DeepLX/translate"
)

// Builtin is the "Achord built-in" engine. The legacy Windows app shipped
// DeepLX as a sidecar executable; Tolk links the same MIT-licensed library
// (OwO Network, v1.2.2) in-process, so there is no binary to trust or start.
type Builtin struct{}

func NewBuiltin() *Builtin { return &Builtin{} }

const builtinCooldown = 60 * time.Second

var (
	builtinMu    sync.Mutex
	builtinUntil time.Time
	builtinSlot  = make(chan struct{}, 1) // one request at a time, like the legacy semaphore
	builtinCall  = translate.TranslateByDeepLX
)

var builtinSource = func() map[string]string {
	m := codes(func(l lang) string { return l.dlSrc })
	m["自动检测"] = "auto"
	m["中文"] = "ZH"
	m["繁体中文"] = "ZH-HANT"
	return m
}()

var builtinTarget = func() map[string]string {
	m := codes(func(l lang) string { return l.dlSrc })
	m["中文"] = "ZH"
	m["繁体中文"] = "ZH-HANT"
	m["英语"] = "EN-US"
	m["葡萄牙语"] = "PT-BR"
	return m
}()

var builtinNames = map[string]string{
	"ZH": "简体中文", "ZH-HANS": "简体中文", "ZH-HANT": "繁体中文", "EN": "英语", "EN-US": "英语",
	"JA": "日语", "KO": "韩语", "FR": "法语", "DE": "德语", "ES": "西班牙语", "RU": "俄语",
	"IT": "意大利语", "PT": "葡萄牙语", "PT-BR": "葡萄牙语", "VI": "越南语", "TH": "泰语", "AR": "阿拉伯语",
	"NL": "荷兰语", "SV": "瑞典语", "DA": "丹麦语", "NB": "挪威语", "FI": "芬兰语", "PL": "波兰语", "CS": "捷克语",
	"HU": "匈牙利语", "RO": "罗马尼亚语", "BG": "保加利亚语", "EL": "希腊语", "UK": "乌克兰语", "TR": "土耳其语",
	"HE": "希伯来语", "ID": "印尼语",
}

func builtinWait(now time.Time) time.Duration {
	builtinMu.Lock()
	defer builtinMu.Unlock()
	return builtinUntil.Sub(now)
}

func builtinCoolDown(now time.Time) {
	builtinMu.Lock()
	defer builtinMu.Unlock()
	if u := now.Add(builtinCooldown); u.After(builtinUntil) {
		builtinUntil = u
	}
}

func limitedErr(d time.Duration) error {
	s := int(math.Ceil(d.Seconds()))
	if s < 1 {
		s = 1
	}
	return fmt.Errorf("Achord 内置引擎请求过于频繁，请 %d 秒后再试；也可切换到 Google、DeepL 或 AI 翻译继续使用", s)
}

func (b *Builtin) Translate(ctx context.Context, text, source, target string) (Result, error) {
	if strings.TrimSpace(text) == "" {
		return Result{}, ErrEmpty
	}
	tgt, ok := builtinTarget[target]
	if !ok {
		return Result{}, unsupported("内置引擎", "目标", target)
	}
	src, ok := builtinSource[source]
	if !ok {
		return Result{}, unsupported("内置引擎", "源", source)
	}
	if d := builtinWait(time.Now()); d > 0 {
		return Result{}, limitedErr(d)
	}
	select {
	case builtinSlot <- struct{}{}:
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
	// Requests queued behind a 429 must respect the cooldown it just set.
	if d := builtinWait(time.Now()); d > 0 {
		<-builtinSlot
		return Result{}, limitedErr(d)
	}

	type out struct {
		r   translate.DeepLXTranslationResult
		err error
	}
	ch := make(chan out, 1)
	go func() {
		// The slot is held until the call itself ends, not until the caller
		// gives up, so timeouts cannot pile up concurrent DeepLX requests.
		defer func() { <-builtinSlot }()
		r, err := builtinCall(src, tgt, text, "", "", "")
		// Record a rate limit before the slot is released, so a queued request
		// cannot slip past the cooldown while the caller is still reading ch.
		if err == nil && (r.Code == http.StatusTooManyRequests || looksLimited(r.Message)) {
			builtinCoolDown(time.Now())
		}
		ch <- out{r, err}
	}()
	select {
	case <-ctx.Done():
		return Result{}, ctx.Err()
	case o := <-ch:
		if o.err != nil {
			return Result{}, fmt.Errorf("内置引擎请求失败：%v", o.err)
		}
		if o.r.Code == http.StatusTooManyRequests || looksLimited(o.r.Message) {
			return Result{}, limitedErr(builtinCooldown)
		}
		if o.r.Code != http.StatusOK {
			msg := strings.TrimSpace(o.r.Message)
			if msg == "" {
				msg = fmt.Sprintf("HTTP %d", o.r.Code)
			}
			return Result{}, fmt.Errorf("内置引擎翻译失败：%s", msg)
		}
		res := strings.TrimSpace(o.r.Data)
		if res == "" {
			return Result{}, fmt.Errorf("内置引擎没有返回译文")
		}
		return Result{Text: res, Detected: builtinNames[strings.ToUpper(o.r.SourceLang)]}, nil
	}
}

func looksLimited(msg string) bool {
	m := strings.ToLower(msg)
	return strings.Contains(m, "429") || strings.Contains(m, "too many requests") ||
		strings.Contains(m, "rate limit") || strings.Contains(m, "ratelimit") || strings.Contains(msg, "请求过于频繁")
}

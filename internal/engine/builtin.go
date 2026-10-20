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

var builtinSource = map[string]string{
	"自动检测": "auto", "简体中文": "ZH", "中文": "ZH", "繁体中文": "ZH-HANT",
	"英语": "EN", "日语": "JA", "韩语": "KO", "法语": "FR", "德语": "DE",
	"西班牙语": "ES", "俄语": "RU", "意大利语": "IT", "葡萄牙语": "PT",
	"越南语": "VI", "泰语": "TH", "阿拉伯语": "AR",
}

var builtinTarget = func() map[string]string {
	m := make(map[string]string, len(builtinSource))
	for k, v := range builtinSource {
		m[k] = v
	}
	m["英语"] = "EN-US"
	m["葡萄牙语"] = "PT-BR"
	delete(m, "自动检测")
	return m
}()

var builtinNames = map[string]string{
	"ZH": "简体中文", "ZH-HANS": "简体中文", "ZH-HANT": "繁体中文", "EN": "英语", "EN-US": "英语",
	"JA": "日语", "KO": "韩语", "FR": "法语", "DE": "德语", "ES": "西班牙语", "RU": "俄语",
	"IT": "意大利语", "PT": "葡萄牙语", "PT-BR": "葡萄牙语", "VI": "越南语", "TH": "泰语", "AR": "阿拉伯语",
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
		return Result{}, fmt.Errorf("内置引擎不支持目标语言：%s", target)
	}
	src, ok := builtinSource[source]
	if !ok {
		src = "auto"
	}
	if d := builtinWait(time.Now()); d > 0 {
		return Result{}, limitedErr(d)
	}
	select {
	case builtinSlot <- struct{}{}:
		defer func() { <-builtinSlot }()
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}

	type out struct {
		r   translate.DeepLXTranslationResult
		err error
	}
	ch := make(chan out, 1)
	go func() {
		r, err := builtinCall(src, tgt, text, "", "", "")
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
			builtinCoolDown(time.Now())
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

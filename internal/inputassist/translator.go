package inputassist

import (
	"context"
	"strings"
	"sync"
	"time"
)

// EngineInfo names the engine in use: Fingerprint is the cache key part and
// Title what the ⚡ tooltip shows. An empty fingerprint means "unknown": the
// translation still works, it is just neither read from nor written to the
// cache.
type EngineInfo struct{ Fingerprint, Title string }

// Cacheable reports whether translations may use the cache.
func (e EngineInfo) Cacheable() bool { return e.Fingerprint != "" }

// serializes is true when requests must go out one after another: the Apple
// on-device engine handles one at a time (a new request interrupts the old),
// and an engine that could not be identified may be anything.
func (e EngineInfo) serializes() bool {
	l := strings.ToLower(e.Fingerprint)
	return l == "apple" || strings.HasPrefix(l, "apple:") || !e.Cacheable()
}

// Backend is what the translator needs from the rest of the app.
type Backend interface {
	// Engine identifies the engine now selected.
	Engine() EngineInfo
	// Translate translates text between two language labels. fingerprint is
	// the engine that really did it ("" when the engine changed meanwhile).
	Translate(text, sourceLabel, targetLabel string) (translated, fingerprint string, err error)
}

// Request is the frozen input of one candidate session.
type Request struct {
	Text   string
	Source string // code, or AutoCode
	Codes  []string
}

// Translator runs the per-language translations of a candidate session: one
// at a time per language, whoever finishes first updates its own row.
type Translator struct {
	Backend Backend
	Cache   *Cache

	mu     sync.Mutex
	engine *EngineInfo
}

// Update receives the new state of the row for a language code. It may be
// called from several goroutines, only until ctx is done.
type Update func(code string, st RowState)

// Start translates every target. It returns at once; cancel ctx to drop the
// session (rows still in flight then stop reporting).
func (t *Translator) Start(ctx context.Context, req Request, update Update) {
	t.mu.Lock()
	t.engine = nil
	t.mu.Unlock()
	go func() {
		info := t.Backend.Engine()
		if ctx.Err() != nil {
			return
		}
		t.mu.Lock()
		t.engine = &info
		t.mu.Unlock()
		if info.serializes() {
			for _, code := range req.Codes {
				if ctx.Err() != nil {
					return
				}
				t.one(ctx, req, info, code, update)
			}
			return
		}
		var wg sync.WaitGroup
		for _, code := range req.Codes {
			wg.Add(1)
			go func() {
				defer wg.Done()
				t.one(ctx, req, info, code, update)
			}()
		}
		wg.Wait()
	}()
}

// Retry translates one language again, after the user clicked a failed row.
func (t *Translator) Retry(ctx context.Context, req Request, code string, update Update) {
	go func() {
		update(code, Loading())
		t.mu.Lock()
		info := t.engine
		t.mu.Unlock()
		if info == nil {
			e := t.Backend.Engine()
			info = &e
			t.mu.Lock()
			t.engine = info
			t.mu.Unlock()
		}
		if ctx.Err() != nil {
			return
		}
		t.one(ctx, req, *info, code, update)
	}()
}

func (t *Translator) one(ctx context.Context, req Request, info EngineInfo, code string, update Update) {
	if ctx.Err() != nil {
		return
	}
	started := time.Now()
	ms := func() int { return int(time.Since(started) / time.Millisecond) }
	key := CacheKey{Text: req.Text, Source: req.Source, Target: code, Engine: info.Fingerprint}
	if info.Cacheable() && t.Cache != nil {
		if text, ok := t.Cache.Get(key); ok {
			update(code, RowState{Kind: RowTranslated, Text: text, Cached: true, LatencyMs: ms(), Engine: info.Title})
			return
		}
	}
	// Nothing is sent once the session is gone: AI engines bill per request.
	if ctx.Err() != nil {
		return
	}
	text, fp, err := t.Backend.Translate(req.Text, LabelForRequest(req.Source), LabelForRequest(code))
	if ctx.Err() != nil {
		return
	}
	if err != nil {
		update(code, RowState{Kind: RowFailed, Message: err.Error()})
		return
	}
	if strings.TrimSpace(text) == "" {
		update(code, RowState{Kind: RowFailed, Message: "翻译结果为空"})
		return
	}
	// An engine switched mid-request reports a different fingerprint: do not
	// file this translation under the engine that did not make it.
	if info.Cacheable() && t.Cache != nil && fp == info.Fingerprint {
		t.Cache.Put(key, text)
	}
	update(code, RowState{Kind: RowTranslated, Text: text, LatencyMs: ms(), Engine: info.Title})
}

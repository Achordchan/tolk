package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"

	"tolk/internal/engine"
	"tolk/internal/legacy"
)

// Tolk is callable from the frontend.
type Tolk struct{}

// Version of the app.
func (Tolk) Version() string { return Version }

// LegacyConfig summarizes an existing dazuo-win config (no secrets).
func (Tolk) LegacyConfig() (legacy.Summary, error) {
	p, err := legacy.DefaultPath()
	if err != nil {
		return legacy.Summary{}, err
	}
	return legacy.Summarize(p)
}

// TranslateResult is what the page shows after a translation.
type TranslateResult struct {
	Text       string `json:"text"`
	Detected   string `json:"detected"`
	Engine     string `json:"engine"`
	DurationMs int64  `json:"durationMs"`
}

// Languages are the labels for the language pickers.
func (Tolk) Languages() []string { return engine.Languages }

// Translate runs the engine chosen in the legacy config.
func (Tolk) Translate(text, source, target string) (TranslateResult, error) {
	return translate(text, source, target)
}

// translate wraps newlines in a marker so engines keep paragraph breaks, as
// dazuo-win did.
func translate(text, source, target string) (TranslateResult, error) {
	p, err := legacy.DefaultPath()
	if err != nil {
		return TranslateResult{}, err
	}
	cfg, err := legacy.Load(p)
	if err != nil {
		cfg = map[string]any{} // no config yet: the default engine needs no key
	}
	api := legacy.APIName(cfg)
	eng, key, err := engineFor(cfg, api)
	if err != nil {
		return TranslateResult{}, err
	}
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	ck := strings.Join([]string{key, source, target, text}, "\x00")
	if r, ok := results.get(ck); ok {
		return r, nil
	}
	r, err := runEngine(eng, api, text, source, target)
	if err == nil {
		results.put(ck, r)
	}
	return r, err
}

func runEngine(eng engine.Engine, api, text, source, target string) (TranslateResult, error) {
	// Apple's on-device translator may be waiting on the user to approve a
	// language-pack download, which can take minutes; network engines keep 30s.
	timeout := 30 * time.Second
	if api == "apple" {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	start := time.Now()
	if api == "apple" { // the system translator keeps line breaks itself
		res, err := eng.Translate(ctx, text, source, target)
		if err != nil {
			return TranslateResult{}, err
		}
		return TranslateResult{Text: res.Text, Detected: res.Detected, Engine: api, DurationMs: time.Since(start).Milliseconds()}, nil
	}
	token := fmt.Sprintf("[[DZFYQ_NL_%x]]", time.Now().UnixNano())
	res, err := eng.Translate(ctx, strings.ReplaceAll(text, "\n", " "+token+" "), source, target)
	if err != nil {
		return TranslateResult{}, err
	}
	out := strings.ReplaceAll(strings.ReplaceAll(res.Text, " "+token+" ", "\n"), token, "\n")
	return TranslateResult{Text: out, Detected: res.Detected, Engine: api, DurationMs: time.Since(start).Milliseconds()}, nil
}

// Engines are kept between translations: rebuilding one per request threw
// away its pooled connection and, for 微软翻译, the web token, which cost a
// page download (seconds) on every translation.
var (
	enginesMu sync.Mutex
	engines   = map[string]engine.Engine{}
)

// engineFor returns the engine for api, reused while its settings stay the same.
func engineFor(cfg map[string]any, api string) (engine.Engine, string, error) {
	b, _ := json.Marshal([]any{cfg["deepl"], cfg["openai_compat"]})
	key := api + "|" + string(b)
	enginesMu.Lock()
	defer enginesMu.Unlock()
	if e, ok := engines[key]; ok {
		return e, key, nil
	}
	e, err := legacy.BuildEngine(cfg, api)
	if err != nil {
		return nil, "", err
	}
	clear(engines) // the old settings are gone
	engines[key] = e
	return e, key, nil
}

// results remembers recent translations, so copying the same text again or
// switching the pair back and forth answers at once.
var results = &resultCache{m: map[string]TranslateResult{}}

type resultCache struct {
	mu    sync.Mutex
	m     map[string]TranslateResult
	order []string
}

func (c *resultCache) get(k string) (TranslateResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.m[k]
	if ok {
		r.DurationMs = 0
	}
	return r, ok
}

func (c *resultCache) put(k string, r TranslateResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.m[k]; !ok {
		c.order = append(c.order, k)
	}
	c.m[k] = r
	for len(c.order) > 200 {
		delete(c.m, c.order[0])
		c.order = c.order[1:]
	}
}

// warmEngine opens the current engine's connection ahead of the next
// translation. Called at start, after the engine changes and every so often
// while idle, so a double copy after a pause does not pay a TLS handshake.
func warmEngine() {
	p, err := legacy.DefaultPath()
	if err != nil {
		return
	}
	cfg, err := legacy.Load(p)
	if err != nil {
		cfg = map[string]any{}
	}
	e, _, err := engineFor(cfg, legacy.APIName(cfg))
	if err != nil {
		return
	}
	if w, ok := e.(engine.Warmer); ok {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = w.Warm(ctx)
	}
}

func keepEngineWarm() {
	warmEngine()
	for range time.Tick(90 * time.Second) {
		warmEngine()
	}
}

// ClipboardText returns the current clipboard text.
func (Tolk) ClipboardText() string { return mygo.Clipboard.ReadText() }

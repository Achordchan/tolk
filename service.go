package main

import (
	"context"
	"fmt"
	"strings"
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
	eng, err := legacy.BuildEngine(cfg, api)
	if err != nil {
		return TranslateResult{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	token := fmt.Sprintf("[[DZFYQ_NL_%x]]", time.Now().UnixNano())
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	start := time.Now()
	res, err := eng.Translate(ctx, strings.ReplaceAll(text, "\n", " "+token+" "), source, target)
	if err != nil {
		return TranslateResult{}, err
	}
	out := strings.ReplaceAll(strings.ReplaceAll(res.Text, " "+token+" ", "\n"), token, "\n")
	return TranslateResult{Text: out, Detected: res.Detected, Engine: api, DurationMs: time.Since(start).Milliseconds()}, nil
}

// ClipboardText returns the current clipboard text.
func (Tolk) ClipboardText() string { return mygo.Clipboard.ReadText() }

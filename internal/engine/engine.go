// Package engine holds the translation engines ported from dazuo-win.
// Language names are the Chinese labels the legacy config uses.
package engine

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// Auto is the "detect the source language" label.
const Auto = "自动检测"

// Result is a finished translation.
type Result struct {
	Text     string `json:"text"`
	Detected string `json:"detected"` // engine language code, may be empty
}

// Engine translates text between two named languages.
type Engine interface {
	Translate(ctx context.Context, text, source, target string) (Result, error)
}

// ErrEmpty is returned when there is nothing to translate.
var ErrEmpty = errors.New("没有要翻译的文本")

func defaultClient() *http.Client { return &http.Client{Timeout: 20 * time.Second} }

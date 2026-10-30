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

// A translation is a few small requests minutes apart; the TLS handshake costs
// more than the request itself, so keep idle connections around longer than
// Go's 90 seconds.
func init() {
	if t, ok := http.DefaultTransport.(*http.Transport); ok {
		t.IdleConnTimeout = 10 * time.Minute
	}
}

// Warmer is an engine that can open its connection (and fetch any token)
// ahead of the first translation.
type Warmer interface {
	Warm(ctx context.Context) error
}

// warmHost opens a pooled connection to the host of url; the response does not matter.
func warmHost(ctx context.Context, c *http.Client, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return err
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

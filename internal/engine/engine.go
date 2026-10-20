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

// Languages lists the language labels in display order.
var Languages = []string{
	"简体中文", "繁体中文", "英语", "日语", "韩语", "法语", "德语",
	"西班牙语", "俄语", "意大利语", "葡萄牙语", "越南语", "泰语", "阿拉伯语",
}

// ErrEmpty is returned when there is nothing to translate.
var ErrEmpty = errors.New("没有要翻译的文本")

func defaultClient() *http.Client { return &http.Client{Timeout: 20 * time.Second} }

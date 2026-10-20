package engine

import (
	"context"
	"errors"
	"strings"
)

// Apple translates on this Mac through the helper (Translation framework).
// Call runs one batch and returns the texts in the same order.
type Apple struct {
	Call func(ctx context.Context, texts []string, source, target string) ([]string, error)
}

func (a *Apple) Translate(ctx context.Context, text, source, target string) (Result, error) {
	if strings.TrimSpace(text) == "" {
		return Result{}, ErrEmpty
	}
	if a.Call == nil {
		return Result{}, errors.New("Apple 本地翻译需要 macOS 15.1 或更高版本的原生助手")
	}
	out, err := a.Call(ctx, []string{text}, source, target)
	if err != nil {
		return Result{}, err
	}
	if len(out) != 1 {
		return Result{}, errors.New("Apple 本地翻译没有返回译文")
	}
	return Result{Text: out[0]}, nil
}

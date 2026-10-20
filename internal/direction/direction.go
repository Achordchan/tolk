// Package direction picks the language pair for the Mini bubble, as bagayalu's
// MiniTranslationDirectionResolver does: Chinese text goes out of Chinese,
// anything else goes into Chinese. Language labels are the legacy Chinese ones.
package direction

import (
	"strings"
	"unicode"

	"tolk/internal/engine"
)

// Pair is a source and target language label.
type Pair struct{ Source, Target string }

type scripts struct{ han, kana, hangul, bopomofo, letters bool }

func scan(text string) scripts {
	var s scripts
	for _, r := range text {
		switch {
		case unicode.Is(unicode.Han, r):
			s.han, s.letters = true, true
		case unicode.Is(unicode.Hiragana, r), unicode.Is(unicode.Katakana, r):
			s.kana, s.letters = true, true
		case unicode.Is(unicode.Hangul, r):
			s.hangul, s.letters = true, true
		case unicode.Is(unicode.Bopomofo, r):
			s.bopomofo, s.letters = true, true
		case unicode.IsLetter(r):
			s.letters = true
		}
	}
	return s
}

func isChineseLabel(l string) bool { return strings.HasSuffix(l, "中文") }

// Resolve returns the pair to use for text given the configured pair. Without
// a language detector, text that is not Chinese, Japanese or Korean keeps the
// "detect" source and lets the engine report the language.
func Resolve(text, source, target string) Pair {
	configured := Pair{source, target}
	text = strings.TrimSpace(text)
	if text == "" {
		return configured
	}
	s := scan(text)
	if !s.kana && !s.hangul && (s.han || s.bopomofo) {
		back := "英语"
		if source != engine.Auto && source != "" && !isChineseLabel(source) {
			back = source
		}
		return Pair{engine.Auto, back}
	}
	if !s.letters {
		return configured
	}
	src := engine.Auto
	switch {
	case s.kana:
		src = "日语"
	case s.hangul:
		src = "韩语"
	}
	tgt := "简体中文"
	if isChineseLabel(target) {
		tgt = target
	}
	return Pair{src, tgt}
}

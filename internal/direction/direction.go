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

type scripts struct {
	han, kana, hangul, bopomofo, letters bool
	// nHan and nOther count Han characters and other letters (Latin,
	// Cyrillic…), to tell Chinese text from, say, English with a Chinese name in it.
	nHan, nOther int
}

func scan(text string) scripts {
	var s scripts
	for _, r := range text {
		switch {
		case unicode.Is(unicode.Han, r):
			s.han, s.letters = true, true
			s.nHan++
		case unicode.Is(unicode.Hiragana, r), unicode.Is(unicode.Katakana, r):
			s.kana, s.letters = true, true
		case unicode.Is(unicode.Hangul, r):
			s.hangul, s.letters = true, true
		case unicode.Is(unicode.Bopomofo, r):
			s.bopomofo, s.letters = true, true
		case unicode.IsLetter(r):
			s.letters = true
			s.nOther++
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
	// A Han character is about a word, a Latin word about five letters: text
	// counts as Chinese when its Han characters are not swamped by other
	// letters, so "Meeting with 王总 at 3pm" still goes into Chinese.
	chinese := s.bopomofo || (s.han && s.nHan*4 >= s.nOther)
	if !s.kana && !s.hangul && chinese {
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

// Other is the target to use when text turned out to be in target already:
// Chinese goes to the configured source (English when that is automatic or
// Chinese), anything else into Chinese.
func Other(target, source string) string {
	if isChineseLabel(target) {
		if source != engine.Auto && source != "" && !isChineseLabel(source) {
			return source
		}
		return "英语"
	}
	return "简体中文"
}

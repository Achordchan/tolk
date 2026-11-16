// Package direction reads the language off the text itself: which pair the
// Mini bubble uses (as bagayalu's MiniTranslationDirectionResolver does) and
// which source language mixed text is in (Detect). Both go by the same
// script count (classify). Language labels are the legacy Chinese ones.
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
	// nHan, nKana, nHangul and nOther count characters of each script
	// (other being Latin, Cyrillic…), to tell Chinese text from, say, English
	// with a Chinese name in it.
	nHan, nKana, nHangul, nOther int
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
			s.nKana++
		case unicode.Is(unicode.Hangul, r):
			s.hangul, s.letters = true, true
			s.nHangul++
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

// Resolve returns the pair to use for text given the configured pair: Chinese
// goes out of Chinese, anything else into it. Japanese and Korean are named
// as the source; other languages keep "detect" and are left to the engine
// (and to Detect).
func Resolve(text, source, target string) Pair {
	configured := Pair{source, target}
	src := engine.Auto
	switch classify(scan(strings.TrimSpace(text))) {
	case scriptNone:
		return configured
	case scriptChinese:
		back := "英语"
		if source != engine.Auto && source != "" && !isChineseLabel(source) {
			back = source
		}
		return Pair{engine.Auto, back}
	case scriptJapanese:
		src = "日语"
	case scriptKorean:
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

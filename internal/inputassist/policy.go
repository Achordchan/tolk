package inputassist

import (
	"strings"
	"unicode"
)

// ScriptFlags says which writing systems a text contains.
type ScriptFlags struct{ Han, Kana, Hangul, Bopomofo bool }

// ScanScripts reports the writing systems present in text.
func ScanScripts(text string) ScriptFlags {
	var f ScriptFlags
	for _, r := range text {
		switch {
		case unicode.Is(unicode.Han, r):
			f.Han = true
		case unicode.Is(unicode.Hiragana, r), unicode.Is(unicode.Katakana, r):
			f.Kana = true
		case unicode.Is(unicode.Hangul, r):
			f.Hangul = true
		case unicode.Is(unicode.Bopomofo, r):
			f.Bopomofo = true
		}
	}
	return f
}

// SelectionSourceLanguage is the old app's selectionSourceLanguageCode: for a
// text with Han characters it does not trust a general language detector over
// the scripts. detected is the detector's code ("" when it has none); the
// result is "" when the language is unknown.
func SelectionSourceLanguage(s ScriptFlags, detected string) string {
	// A detector that says Chinese is believed: only it can tell Simplified
	// from Traditional.
	if detected == "zh-CN" || detected == "zh-TW" {
		return detected
	}
	// Kana and Hangul are decisive: Chinese never contains them.
	switch {
	case s.Kana:
		return "ja"
	case s.Hangul:
		return "ko"
	case s.Bopomofo:
		return "zh-TW"
	}
	if !s.Han {
		return detected
	}
	// Han only, and the detector says Japanese or Korean: keep that.
	if detected == "ja" || detected == "ko" {
		return detected
	}
	return "zh-CN"
}

// Detect is a small language detector for the languages whose script or most
// common words give them away. It has no machine-learned model: whatever it
// cannot tell it leaves as "" (the engine then detects the language, and no
// target is hidden). Returned codes are Tolk's.
func Detect(text string) string {
	s := ScanScripts(text)
	switch {
	case s.Kana, s.Hangul, s.Bopomofo:
		return SelectionSourceLanguage(s, "")
	case s.Han:
		if traditionalScore(text) > 0 {
			return "zh-TW"
		}
		return "zh-CN"
	}
	counts := map[string]int{}
	letters := 0
	for _, r := range text {
		if !unicode.IsLetter(r) {
			continue
		}
		letters++
		switch {
		case unicode.Is(unicode.Greek, r):
			counts["el"]++
		case unicode.Is(unicode.Thai, r):
			counts["th"]++
		case unicode.Is(unicode.Hebrew, r):
			counts["he"]++
		case unicode.Is(unicode.Latin, r) && r < 0x250:
			counts["latin"]++
		}
	}
	if letters == 0 {
		return ""
	}
	for _, c := range []string{"el", "th", "he"} {
		if counts[c]*2 > letters {
			return c
		}
	}
	if counts["latin"]*2 > letters && looksEnglish(text) {
		return "en"
	}
	return ""
}

var englishWords = map[string]bool{
	"the": true, "and": true, "is": true, "are": true, "was": true, "were": true, "to": true, "of": true,
	"in": true, "it": true, "that": true, "this": true, "for": true, "with": true, "you": true, "i": true,
	"have": true, "not": true, "be": true, "on": true, "at": true, "as": true, "we": true, "my": true,
	"can": true, "will": true, "do": true, "if": true, "from": true, "your": true, "hello": true, "thanks": true,
	"please": true, "what": true, "how": true, "but": true, "or": true, "an": true, "so": true, "me": true,
}

// looksEnglish is true for text made of ASCII words, a good share of them
// common English ones. Short greetings count; a single unknown word does not.
func looksEnglish(text string) bool {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && r != '\'' })
	if len(words) == 0 {
		return false
	}
	hits := 0
	for _, w := range words {
		for _, r := range w {
			if r > unicode.MaxASCII {
				return false // accents: French, Spanish, German…
			}
		}
		if englishWords[strings.Trim(w, "'")] {
			hits++
		}
	}
	return hits > 0 && hits*4 >= len(words)
}

// traditionalOnly and simplifiedOnly are common characters that exist in one
// script only; their balance tells Traditional from Simplified when a plain
// Han text carries no other hint.
const (
	traditionalOnly = "這們個為說與時來國會學經過對於沒還開關話車門書東馬鳥魚頭體電見現問題長點幾區當總義幫謝讓區處號買賣機氣習"
	simplifiedOnly  = "这们个为说与时来国会学经过对于没还开关话车门书东马鸟鱼头体电见现问题长点几区当总义帮谢让处号买卖机气习"
)

func traditionalScore(text string) int {
	score := 0
	for _, r := range text {
		if strings.ContainsRune(traditionalOnly, r) && !strings.ContainsRune(simplifiedOnly, r) {
			score++
		} else if strings.ContainsRune(simplifiedOnly, r) && !strings.ContainsRune(traditionalOnly, r) {
			score--
		}
	}
	return score
}

// SanitizeCodes drops empty and repeated codes (en and EN_ are the same) and
// keeps the first MaxTargets, in order.
func SanitizeCodes(codes []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range codes {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		n := strings.ToLower(strings.ReplaceAll(c, "_", "-"))
		if seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, c)
		if len(out) == MaxTargets {
			break
		}
	}
	return out
}

// VisibleTargets hides the targets that are the source language. An unknown
// source (empty, or auto) hides none.
func VisibleTargets(targets []string, source string) []string {
	if source == "" || source == AutoCode {
		return targets
	}
	var out []string
	for _, t := range targets {
		if !SameLanguage(t, source) {
			out = append(out, t)
		}
	}
	return out
}

// SameLanguage reports whether two codes name one language. With the same
// base language, only two differing region or script subtags make them
// different, so zh-CN and zh-TW differ while en and en-US do not.
func SameLanguage(a, b string) bool {
	ab, av := subtags(a)
	bb, bv := subtags(b)
	if ab != bb {
		return false
	}
	if av == "" || bv == "" {
		return true
	}
	return av == bv
}

func subtags(code string) (base, variant string) {
	parts := strings.FieldsFunc(strings.ToLower(strings.TrimSpace(code)), func(r rune) bool { return r == '-' || r == '_' })
	if len(parts) == 0 {
		return "", ""
	}
	if len(parts) > 1 {
		variant = parts[1]
	}
	return parts[0], variant
}

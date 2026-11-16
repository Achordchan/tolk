package direction

import (
	"strings"
	"unicode"

	"github.com/abadojack/whatlanggo"

	"tolk/internal/engine"
)

// script is the writing system that carries a text.
type script int

const (
	scriptNone     script = iota // no letters: digits, punctuation, empty
	scriptChinese                // Han (or Bopomofo), with too few other letters to matter
	scriptJapanese               // Han with kana in it, or kana alone
	scriptKorean                 // Hangul
	scriptOther                  // Latin, Cyrillic, Arabic…: Han is only a name or two in it
)

// classify says which script carries the text. A Han character is about a
// word and a Latin word about five letters, so CJK text counts as such while
// its characters are not swamped by other letters: "Meeting with 王总 at 3pm"
// is English, "我用 iPhone 拍的照片" Chinese.
func classify(s scripts) script {
	cjk := s.nHan + s.nKana + s.nHangul
	switch {
	case !s.letters:
		return scriptNone
	case s.bopomofo:
		return scriptChinese
	case cjk == 0 || cjk*4 < s.nOther:
		return scriptOther
	// Chinese has no kana and no Hangul, so even one makes CJK text Japanese
	// ("東京都内の不動産売買情報") or Korean. A Japanese word in English
	// never gets here: the check above already found it swamped.
	case s.nKana > 0:
		return scriptJapanese
	case s.nHangul > 0:
		return scriptKorean
	}
	return scriptChinese
}

// Guess is what the text itself says about its language, before any engine
// sees it. Engines detect a whole text as one language, and mixed text fools
// them: English that names a few Chinese companies came back from Google as
// "Chinese" and untranslated. Only mixed text gets a guess; text in one
// script is left to the engine, which reads it well.
type Guess struct {
	// Label is the language, or "" when there is nothing to add.
	Label string
	// Sure means Label can be sent to the engine as the source language. A
	// guess that is not sure only corrects an engine that got it wrong.
	Sure bool
	// CJK says Chinese, Japanese or Korean carries the text.
	CJK bool
}

// Detect guesses the language of mixed text: Chinese, Japanese or Korean
// with other letters in it, or the other way round.
func Detect(text string) Guess {
	s := scan(text)
	if s.nHan+s.nKana+s.nHangul == 0 || s.nOther == 0 {
		return Guess{}
	}
	switch classify(s) {
	// Engines read these well even with English words in them, and kanji
	// alone cannot tell Japanese from Chinese: only a fallback.
	case scriptJapanese:
		return Guess{Label: "日语", CJK: true}
	case scriptKorean:
		return Guess{Label: "韩语", CJK: true}
	case scriptChinese:
		return Guess{Label: chineseLabel(text), CJK: true}
	}
	// Other letters carry the text: name the language from them alone.
	rest := strings.Map(func(r rune) rune {
		if isCJK(r) {
			return ' '
		}
		return r
	}, text)
	info := whatlanggo.DetectWithOptions(rest, whatlanggo.Options{Whitelist: known})
	if info.Lang >= 0 && info.IsReliable() {
		if label := engine.LabelForCode(info.Lang.Iso6391()); label != "" {
			return Guess{Label: label, Sure: true}
		}
	}
	// Too little text to tell, say "Meeting with 王总 at 3pm" (it looks
	// Indonesian to the detector): fall back on the script's commonest language.
	switch info.Script {
	case unicode.Latin:
		return Guess{Label: "英语"}
	case unicode.Cyrillic:
		return Guess{Label: "俄语"}
	}
	return Guess{}
}

// Misread says the engine got the language of text wrong, so translating to
// target again from g.Label is worth it. detected is what the engine
// reported, a code or a label.
//
// Text read as Chinese, Japanese or Korean while other letters carry it is
// always wrong. Text handed back unchanged is wrong only when the engine's
// language contradicts the script: unchanged text can simply be in target
// already ("東京大学 ABC" into Japanese, which kanji alone would guess as
// Chinese). With no language reported, a CJK guess does not overrule a CJK
// target for the same reason.
func (g Guess) Misread(text, out, detected, target string) bool {
	if g.Label == "" {
		return false
	}
	l := detected
	if engine.CodeForLabel(l) == "" {
		l = engine.LabelForCode(detected)
	}
	if strings.Join(strings.Fields(out), " ") == strings.Join(strings.Fields(text), " ") {
		if l == "" {
			return !(g.CJK && IsCJKLabel(target))
		}
		return IsCJKLabel(l) != g.CJK
	}
	return !g.CJK && IsCJKLabel(l)
}

// IsCJKLabel says whether a language label is Chinese, Japanese or Korean.
func IsCJKLabel(l string) bool { return isChineseLabel(l) || l == "日语" || l == "韩语" }

func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r) ||
		unicode.Is(unicode.Bopomofo, r)
}

// known limits detection to the languages the pickers offer, which also makes
// it surer: Spanish no longer has to beat Galician and Catalan.
var known = func() map[whatlanggo.Lang]bool {
	m := map[whatlanggo.Lang]bool{}
	for l := range whatlanggo.Langs {
		if engine.LabelForCode(l.Iso6391()) != "" {
			m[l] = true
		}
	}
	return m
}()

// chineseLabel tells Traditional from Simplified Chinese by characters that
// differ between the two; text with none of them is taken as Simplified.
func chineseLabel(text string) string {
	simp, trad := 0, 0
	for _, r := range text {
		if strings.ContainsRune(simplifiedOnly, r) {
			simp++
		} else if strings.ContainsRune(traditionalOnly, r) {
			trad++
		}
	}
	if trad > simp {
		return "繁体中文"
	}
	return "简体中文"
}

// Common characters written differently in the two scripts, pair by pair.
const (
	simplifiedOnly  = "这个来说时会为对发见国过还学后与东车长开问关点电体书门们业务实现经应该网络软语读写请谢让认识页单价钱买卖号码图标设计显钟头脑办"
	traditionalOnly = "這個來說時會為對發見國過還學後與東車長開問關點電體書門們業務實現經應該網絡軟語讀寫請謝讓認識頁單價錢買賣號碼圖標設計顯鐘頭腦辦"
)

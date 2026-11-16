package direction

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// The email Google read as Chinese and returned untranslated.
const mixedEmail = `Hi Harshit,

Thank you for your help. Attached are the documents for advertiser verification of account 337-987-4446 (ticket 3-6620000042341):

1. Service agreement between 徐州超拓弹簧制造有限公司 and Achord Chan (signed and stamped)
2. Agency contract BNW2026082601 with BLUE NETWORK TECHNOLOGY LIMITED
3. Invoice from 深圳蓝粉网络科技有限公司 for advertising fees
4. Domain ownership certificate for norenspring.com (registered to our company)
5. Business license of 徐州超拓弹簧制造有限公司 (credit code 91320312MAKEYTFX6J),Note: our registered legal name is 徐州超拓弹簧制造有限公司 (pinyin: Xuzhou Chaotuo). "Xuzhou Apex Spring Manufacturing Co., Ltd." on our website is our English trade name, and NorenSpring is our brand. They are the same company.`

func TestDetect(t *testing.T) {
	sure := func(l string) Guess { return Guess{Label: l, Sure: true} }
	weak := func(l string) Guess { return Guess{Label: l} }
	cjk := func(l string) Guess { return Guess{Label: l, CJK: true} }
	cases := []struct {
		name, text string
		want       Guess
	}{
		{"english email naming chinese companies", mixedEmail, sure("英语")},
		{"english sentence with a chinese name", "Please send the signed contract to 王总 before Friday, thanks.", sure("英语")},
		{"short english with a chinese name is a weak guess", "Meeting with 王总 at 3pm", weak("英语")},
		{"french with a chinese name", "Merci beaucoup pour votre aide, je vous envoie le contrat signé par 王总 demain matin.", sure("法语")},
		{"german with a chinese company", "Wir haben den Vertrag mit 深圳蓝粉网络科技有限公司 gestern unterschrieben und schicken ihn morgen zurück.", sure("德语")},
		{"short russian with a chinese name is a weak guess", "Пожалуйста, отправьте договор господину 王总 до пятницы, спасибо.", weak("俄语")},
		{"arabic with a chinese name", "شكرا جزيلا على مساعدتك يا 王总، سنرسل العقد غدا", sure("阿拉伯语")},
		{"english with a japanese word", "We visited Tokyo and ate at a place called すし every night of the trip.", sure("英语")},
		{"chinese with a brand name", "我用 iPhone 拍的照片", cjk("简体中文")},
		{"chinese with code words", "这个 PR 修复了 React useEffect 的 cleanup 问题", cjk("简体中文")},
		{"traditional chinese with a brand name", "這個 iPhone 的電池還能用嗎？我們明天再說。", cjk("繁体中文")},
		{"japanese with english", "このプロジェクトは React と TypeScript で書かれています", cjk("日语")},
		{"kanji-heavy japanese with english", "東京都内の不動産売買情報 2026 Spring Edition", cjk("日语")},
		{"korean with english", "이 프로젝트는 React로 작성되었습니다", cjk("韩语")},
		{"plain english is left to the engine", "Hello, how are you today?", Guess{}},
		{"plain chinese is left to the engine", "你好，今天天气不错", Guess{}},
		{"digits only", "12345 678", Guess{}},
		{"empty", "", Guess{}},
	}
	for _, c := range cases {
		if got := Detect(c.text); got != c.want {
			t.Errorf("%s: got %+v want %+v", c.name, got, c.want)
		}
	}
}

func TestMisread(t *testing.T) {
	en := Guess{Label: "英语"}
	zh := Guess{Label: "简体中文", CJK: true}
	ja := Guess{Label: "简体中文", CJK: true} // what kanji-only Japanese guesses
	cases := []struct {
		name                        string
		g                           Guess
		text, out, detected, target string
		want                        bool
	}{
		{"handed back unchanged, no language reported", en, "Meeting with 王总", " Meeting  with 王总 ", "", "简体中文", true},
		{"latin text read as chinese", en, "Meeting with 王总", "与王总会面", "zh-CN", "简体中文", true},
		{"latin text read as chinese, label reported", en, "Meeting with 王总", "与王总会面", "简体中文", "简体中文", true},
		{"translated and read as english", en, "Meeting with 王总", "与王总会面", "en", "简体中文", false},
		{"chinese text read as english but translated", zh, "我用 iPhone 拍的照片", "Photos I took with my iPhone", "en", "英语", false},
		{"chinese text read as english and handed back", zh, "我用 iPhone 拍的照片", "我用 iPhone 拍的照片", "en", "英语", true},
		{"japanese already in the target is kept", ja, "東京大学 ABC", "東京大学 ABC", "ja", "日语", false},
		{"japanese already in the target, no language reported", ja, "東京大学 ABC", "東京大学 ABC", "", "日语", false},
		{"chinese handed back with no language reported", zh, "我用 iPhone 拍的照片", "我用 iPhone 拍的照片", "", "英语", true},
		{"no guess", Guess{}, "hello", "hello", "", "英语", false},
	}
	for _, c := range cases {
		if got := c.g.Misread(c.text, c.out, c.detected, c.target); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestChineseCharacterPairs(t *testing.T) {
	if a, b := utf8.RuneCountInString(simplifiedOnly), utf8.RuneCountInString(traditionalOnly); a != b {
		t.Fatalf("lists differ in length: %d simplified, %d traditional", a, b)
	}
	for _, r := range simplifiedOnly {
		if strings.ContainsRune(traditionalOnly, r) {
			t.Errorf("%c is in both lists", r)
		}
	}
}

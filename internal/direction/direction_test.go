package direction

import "testing"

func TestResolve(t *testing.T) {
	cases := []struct {
		name, text, src, tgt string
		want                 Pair
	}{
		{"chinese goes to english", "你好，世界", "自动检测", "简体中文", Pair{"自动检测", "英语"}},
		{"chinese goes back to configured source", "你好", "法语", "简体中文", Pair{"自动检测", "法语"}},
		{"english goes to chinese", "hello world", "自动检测", "英语", Pair{"自动检测", "简体中文"}},
		{"keeps traditional target", "hello", "自动检测", "繁体中文", Pair{"自动检测", "繁体中文"}},
		{"japanese is not chinese", "こんにちは漢字", "自动检测", "简体中文", Pair{"日语", "简体中文"}},
		{"korean", "안녕하세요", "自动检测", "简体中文", Pair{"韩语", "简体中文"}},
		{"english with a chinese name goes to chinese", "Please send the contract to 王总 before Friday.", "自动检测", "简体中文", Pair{"自动检测", "简体中文"}},
		{"chinese with a brand name stays chinese", "我用 iPhone 拍的照片", "自动检测", "简体中文", Pair{"自动检测", "英语"}},
		{"digits keep the configured pair", "12345", "英语", "日语", Pair{"英语", "日语"}},
		{"empty keeps the configured pair", "  ", "英语", "日语", Pair{"英语", "日语"}},
	}
	for _, c := range cases {
		if got := Resolve(c.text, c.src, c.tgt); got != c.want {
			t.Errorf("%s: got %+v want %+v", c.name, got, c.want)
		}
	}
}

func TestOther(t *testing.T) {
	for _, c := range []struct{ tgt, src, want string }{
		{"英语", "自动检测", "简体中文"},
		{"简体中文", "自动检测", "英语"},
		{"繁体中文", "法语", "法语"},
		{"日语", "英语", "简体中文"},
	} {
		if got := Other(c.tgt, c.src); got != c.want {
			t.Errorf("Other(%q, %q) = %q, want %q", c.tgt, c.src, got, c.want)
		}
	}
}

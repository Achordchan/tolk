package inputassist

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAppFilter(t *testing.T) {
	wechat := &App{Exe: "WeChat.exe", Name: "微信"}
	term := &App{Exe: "WindowsTerminal.exe"}
	block := ParseList("windowsterminal\n 1Password.exe ")
	if AppAllowed(nil, ScopeGlobal, nil, nil) {
		t.Error("unknown app allowed")
	}
	if !AppAllowed(wechat, ScopeGlobal, block, nil) || AppAllowed(term, ScopeGlobal, block, nil) {
		t.Error("global scope with blocklist")
	}
	if AppAllowed(wechat, ScopeAllowlist, block, nil) || !AppAllowed(wechat, ScopeAllowlist, block, []string{"WECHAT"}) || !AppAllowed(wechat, ScopeAllowlist, nil, []string{"微信"}) {
		t.Error("allowlist scope")
	}
	if AppAllowed(term, ScopeAllowlist, block, []string{"WindowsTerminal"}) {
		t.Error("blocklist must beat the allowlist")
	}
	mac := App{ID: "com.apple.Terminal", Name: "Terminal"}
	if !AppMatches(mac, []string{"terminal.app"}) || !AppMatches(mac, []string{"COM.APPLE.TERMINAL"}) {
		t.Error("mac names")
	}
}

func TestSourceLanguage(t *testing.T) {
	cases := []struct {
		s    ScriptFlags
		det  string
		want string
	}{
		{ScriptFlags{Han: true}, "", "zh-CN"},
		{ScriptFlags{Han: true}, "en", "zh-CN"},
		{ScriptFlags{Han: true}, "zh-TW", "zh-TW"},
		{ScriptFlags{Han: true}, "ja", "ja"},
		{ScriptFlags{Han: true, Kana: true}, "zh-CN", "zh-CN"}, // the detector says Chinese: believed
		{ScriptFlags{Han: true, Kana: true}, "", "ja"},
		{ScriptFlags{Hangul: true}, "", "ko"},
		{ScriptFlags{Han: true, Bopomofo: true}, "", "zh-TW"},
		{ScriptFlags{}, "fr", "fr"},
		{ScriptFlags{}, "", ""},
	}
	for _, c := range cases {
		if got := SelectionSourceLanguage(c.s, c.det); got != c.want {
			t.Errorf("%+v %q: %q, want %q", c.s, c.det, got, c.want)
		}
	}
}

func TestDetect(t *testing.T) {
	for text, want := range map[string]string{
		"你好，世界": "zh-CN", "這個是我們的時間": "zh-TW", "这个是我们的时间": "zh-CN",
		"こんにちは": "ja", "안녕하세요": "ko", "Hello, how are you?": "en", "Please send me the file": "en",
		"Bonjour tout le monde": "", "Straße": "", "12345": "", "γειά σου": "el", "สวัสดี": "th",
	} {
		if got := Detect(text); got != want {
			t.Errorf("Detect(%q) = %q, want %q", text, got, want)
		}
	}
}

func TestVisibleTargets(t *testing.T) {
	all := []string{"zh-CN", "en", "zh-TW", "ja"}
	for _, c := range []struct {
		src  string
		want []string
	}{
		{"", all}, {AutoCode, all},
		{"en", []string{"zh-CN", "zh-TW", "ja"}}, {"en-US", []string{"zh-CN", "zh-TW", "ja"}},
		{"zh-CN", []string{"en", "zh-TW", "ja"}}, {"zh", []string{"en", "ja"}},
	} {
		if got := VisibleTargets(all, c.src); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q: %v, want %v", c.src, got, c.want)
		}
	}
	if !SameLanguage("en", "en-US") || SameLanguage("zh-CN", "zh-TW") || SameLanguage("en", "fr") || !SameLanguage("zh_cn", "ZH-CN") {
		t.Error("SameLanguage")
	}
}

func TestLooksTranslatable(t *testing.T) {
	for text, want := range map[string]bool{
		"好的": true, "OK": true, "Hello world": true, "": false, "   ": false, "12.5": false, "123 456": false, "!!!": false,
		"https://example.com": false, "www.example.com": false, "example.com/产品": false, "foo@bar.com": false,
		"visit example.com today": true, "a@b": true, "see you @ 5": true, "v1.2.3": true,
	} {
		if got := LooksTranslatable(text); got != want {
			t.Errorf("%q: %v", text, got)
		}
	}
}

func list3(committable ...int) *List {
	l := NewList([]string{"en", "ja", "ko"})
	for _, i := range committable {
		l.Update(l.Rows[i].Code, RowState{Kind: RowTranslated, Text: "x"})
	}
	return &l
}

func TestListState(t *testing.T) {
	l := list3()
	if l.Selected != 0 || l.Move(-1) || !l.Move(1) || !l.Move(1) || l.Move(1) || l.Selected != 2 {
		t.Fatal("Move stops at the ends")
	}
	if l.Select(2) || !l.Select(0) || l.Select(9) {
		t.Fatal("Select")
	}
	if !l.Update("ja", RowState{Kind: RowFailed, Message: "x"}) || l.Update("ja", RowState{Kind: RowFailed, Message: "x"}) || l.Update("zz", Loading()) {
		t.Fatal("Update reports changes")
	}
	if l.Committable(1) || l.Text(1) != "" {
		t.Fatal("failed row is not committable")
	}
	l.Update("en", RowState{Kind: RowTranslated, Text: "  "})
	if l.Committable(0) {
		t.Fatal("blank translation")
	}
	if l.Rows[0].Label != "EN" {
		t.Fatal(l.Rows[0].Label)
	}
}

func TestRoute(t *testing.T) {
	l := list3(0, 1)
	type tc struct {
		e    KeyEvent
		want Decision
		ok   bool
	}
	for i, c := range []tc{
		{KeyEvent{VK: VKEscape}, Decision{ActionDismiss, 0, true}, true},
		{KeyEvent{VK: VKUp}, Decision{ActionMoveUp, 0, true}, true},
		{KeyEvent{VK: VKDown}, Decision{ActionMoveDown, 0, true}, true},
		{KeyEvent{VK: VKReturn}, Decision{ActionCommit, 0, true}, true},
		{KeyEvent{VK: VKReturn, Shift: true}, Decision{ActionDismissPass, 0, false}, true},
		{KeyEvent{VK: VKLeft}, Decision{ActionDismissPass, 0, false}, true},
		{KeyEvent{VK: 'A'}, Decision{ActionDismissPass, 0, false}, true},
		{KeyEvent{VK: VKSpace}, Decision{ActionDismissPass, 0, false}, true},
		{KeyEvent{VK: '2', Ctrl: true}, Decision{ActionCommitIndex, 1, true}, true},
		{KeyEvent{VK: 0x61, Ctrl: true}, Decision{ActionCommitIndex, 0, true}, true},
		{KeyEvent{VK: '3', Ctrl: true}, Decision{}, false}, // row 3 has no translation yet
		{KeyEvent{VK: '4', Ctrl: true}, Decision{}, false}, // only three candidates: the app's own shortcut
		{KeyEvent{VK: '2', Ctrl: true, Shift: true}, Decision{ActionDismissPass, 0, false}, true},
		{KeyEvent{VK: 'C', Ctrl: true}, Decision{ActionCopy, 0, true}, true},
		{KeyEvent{VK: 'V', Ctrl: true}, Decision{ActionDismissPass, 0, false}, true},
		{KeyEvent{VK: VKUp, Alt: true}, Decision{ActionDismissPass, 0, false}, true},
		{KeyEvent{VK: VKLMenu, Alt: true, Shift: true}, Decision{}, false}, // the shortcut's own modifiers
		{KeyEvent{VK: VKRShift}, Decision{}, false},
	} {
		got, ok := Route(c.e, l)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("%d %+v: %+v %v, want %+v %v", i, c.e, got, ok, c.want, c.ok)
		}
	}
	// Enter must not be eaten while the highlighted row has nothing.
	if d, _ := Route(KeyEvent{VK: VKReturn}, list3()); d.Swallow || d.Action != ActionDismissPass {
		t.Errorf("Enter on a loading row: %+v", d)
	}
	if d, ok := Route(KeyEvent{VK: 'C', Ctrl: true}, list3()); ok {
		t.Errorf("Ctrl+C with nothing to copy: %+v", d)
	}
}

func TestLayout(t *testing.T) {
	if ReservedLines("") != 1 || ReservedLines("hello") != 1 || ReservedLines(strings.Repeat("a", 100)) != 3 ||
		ReservedLines(strings.Repeat("你", 100)) != SelectedMaxLines || ReservedLines("你好") != 1 {
		t.Fatal("ReservedLines")
	}
	if LinesFor(4, false) != 2 || LinesFor(4, true) != 4 || LinesFor(1, true) != 1 {
		t.Fatal("LinesFor")
	}
	g := Layout(3, 0, 1, false)
	// padding 6 + three rows of 28 + two gaps of 1 + padding 6
	if g.Height != 6+3*28+2+6 || g.Width != PanelWidth || !reflect.DeepEqual(g.Tops, []int{6, 35, 64}) {
		t.Fatalf("%+v", g)
	}
	if got := Layout(3, 0, 1, true).Height; got != g.Height+CopyNoticeHeight {
		t.Fatal(got)
	}
	if g.RowAt(5) != -1 || g.RowAt(6) != 0 || g.RowAt(34) != -1 || g.RowAt(35) != 1 || g.RowAt(91) != 2 || g.RowAt(g.Height) != -1 {
		t.Fatal("RowAt")
	}
	if Layout(0, 0, 1, false).Height != LineHeight+12 {
		t.Fatal("empty")
	}
	// The highlighted row grows with long source text.
	if Layout(2, 0, 4, false).Heights[0] <= Layout(2, 0, 4, false).Heights[1] {
		t.Fatal("selected row should be taller")
	}
}

func TestPanelOrigin(t *testing.T) {
	work := Rect{0, 0, 1920, 1040}
	anchor := Rect{100, 200, 80, 20}
	if x, y := PanelOrigin(anchor, 420, 100, work); x != 100 || y != 226 {
		t.Errorf("below: %d,%d", x, y)
	}
	// No room below: flips above.
	if _, y := PanelOrigin(Rect{100, 1000, 80, 20}, 420, 100, work); y != 1000-6-100 {
		t.Errorf("above: %d", y)
	}
	// No room either way: stays below, clamped.
	if _, y := PanelOrigin(Rect{100, 20, 80, 20}, 420, 1030, work); y != 8 {
		t.Errorf("clamped: %d", y)
	}
	// Near the right edge.
	if x, _ := PanelOrigin(Rect{1800, 200, 50, 20}, 420, 100, work); x != 1920-8-420 {
		t.Errorf("right: %d", x)
	}
	// Left of the work area on a second monitor.
	if x, _ := PanelOrigin(Rect{-2000, 200, 50, 20}, 420, 100, Rect{-1920, 0, 1920, 1040}); x != -1920+8 {
		t.Errorf("left: %d", x)
	}
}

func TestWindowsShortcut(t *testing.T) {
	for _, c := range []struct {
		k, m int
		want string
		bad  bool
	}{
		{49, ModOption | ModShift, "Alt+Shift+Space", false},
		{49, ModControl | ModOption, "Ctrl+Alt+Space", false},
		{17, ModControl | ModOption, "Ctrl+Alt+T", false},
		{38, ModCmd | ModOption, "Alt+Win+J", false},
		{49, 0, "", true}, {255, ModOption, "", true}, {10, ModOption, "", true},
	} {
		got, err := WindowsShortcut(c.k, c.m)
		if (err != nil) != c.bad || got != c.want {
			t.Errorf("%d/%d: %q %v", c.k, c.m, got, err)
		}
	}
}

func TestCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "cache.json")
	c := NewCache(path)
	k := CacheKey{"hi", "en", "ja", "google"}
	if _, ok := c.Get(k); ok {
		t.Fatal("hit on empty")
	}
	c.Put(k, "やあ")
	c.Put(CacheKey{"x", "en", "ja", "google"}, "  ")
	if v, ok := c.Get(k); !ok || v != "やあ" || c.Len() != 1 {
		t.Fatal("blank translations must not be stored")
	}
	if _, ok := c.Get(CacheKey{"hi", "en", "ja", "openai_compat:m@https://x"}); ok {
		t.Fatal("another engine must not hit")
	}
	c.Flush()
	if v, ok := NewCache(path).Get(k); !ok || v != "やあ" {
		t.Fatal("not persisted")
	}
	// A damaged file is an empty cache and goes away.
	if err := os.WriteFile(path, []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	d := NewCache(path)
	if d.Len() != 0 {
		t.Fatal("damaged file loaded")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("damaged file kept")
	}
}

func TestCacheEvictsLeastRecentlyUsed(t *testing.T) {
	c := NewCache("")
	c.maxCount = 3
	for i, w := range []string{"a", "b", "c"} {
		c.Put(CacheKey{Text: w}, w+"!")
		_ = i
	}
	c.Get(CacheKey{Text: "a"}) // a is now the freshest
	c.Put(CacheKey{Text: "d"}, "d!")
	if _, ok := c.Get(CacheKey{Text: "b"}); ok {
		t.Fatal("b should have been evicted")
	}
	for _, w := range []string{"a", "c", "d"} {
		if _, ok := c.Get(CacheKey{Text: w}); !ok {
			t.Fatalf("%s evicted", w)
		}
	}
}

// ---- translator ----

type fakeBackend struct {
	mu     sync.Mutex
	info   EngineInfo
	calls  []string
	fail   map[string]bool
	fp     string
	maxRun int
	run    int
	delay  time.Duration
}

func (f *fakeBackend) Engine() EngineInfo { return f.info }
func (f *fakeBackend) Translate(text, src, tgt string) (string, string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, src+">"+tgt)
	f.run++
	f.maxRun = max(f.maxRun, f.run)
	f.mu.Unlock()
	time.Sleep(f.delay)
	f.mu.Lock()
	f.run--
	f.mu.Unlock()
	if f.fail[tgt] {
		return "", "", errors.New("boom")
	}
	fp := f.fp
	if fp == "" {
		fp = f.info.Fingerprint
	}
	return tgt + ":" + text, fp, nil
}

type collector struct {
	mu   sync.Mutex
	rows map[string]RowState
	done chan struct{}
	want int
}

func newCollector(want int) *collector {
	return &collector{rows: map[string]RowState{}, done: make(chan struct{}, 64), want: want}
}
func (c *collector) update(code string, st RowState) {
	c.mu.Lock()
	c.rows[code] = st
	c.mu.Unlock()
	if st.Kind != RowLoading {
		c.done <- struct{}{}
	}
}
func (c *collector) wait(t *testing.T, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-c.done:
		case <-time.After(3 * time.Second):
			t.Fatal("timeout")
		}
	}
}

func TestTranslatorParallelAndCache(t *testing.T) {
	b := &fakeBackend{info: EngineInfo{"google", "Google 翻译"}, fail: map[string]bool{"日语": true}, delay: 20 * time.Millisecond}
	tr := &Translator{Backend: b, Cache: NewCache("")}
	req := Request{Text: "hi", Source: "en", Codes: []string{"zh-CN", "ja", "ko"}}
	col := newCollector(3)
	tr.Start(context.Background(), req, col.update)
	col.wait(t, 3)
	if b.maxRun < 2 {
		t.Errorf("languages should run in parallel, max %d", b.maxRun)
	}
	if st := col.rows["zh-CN"]; st.Kind != RowTranslated || st.Text != "简体中文:hi" || st.Cached || st.Engine != "Google 翻译" {
		t.Errorf("%+v", st)
	}
	if st := col.rows["ja"]; st.Kind != RowFailed || st.Message != "boom" {
		t.Errorf("one failure must stay on its row: %+v", st)
	}
	// Second round: successes come from the cache, the failure is asked again.
	b.fail = nil
	col = newCollector(3)
	before := len(b.calls)
	tr.Start(context.Background(), req, col.update)
	col.wait(t, 3)
	if st := col.rows["zh-CN"]; !st.Cached || st.Text != "简体中文:hi" {
		t.Errorf("not cached: %+v", st)
	}
	if got := len(b.calls) - before; got != 1 {
		t.Errorf("only the failed language should be asked again, %d calls", got)
	}
	// Retry of the failed row.
	col = newCollector(1)
	b.fail = map[string]bool{"韩语": true}
	req.Text = "yo"
	tr.Retry(context.Background(), req, "ko", col.update)
	col.wait(t, 1)
	if col.rows["ko"].Kind != RowFailed {
		t.Errorf("%+v", col.rows["ko"])
	}
}

func TestTranslatorUnknownEngineIsSerialAndUncached(t *testing.T) {
	b := &fakeBackend{info: EngineInfo{"", "Tolk"}, delay: 10 * time.Millisecond}
	tr := &Translator{Backend: b, Cache: NewCache("")}
	col := newCollector(2)
	tr.Start(context.Background(), Request{Text: "hi", Source: AutoCode, Codes: []string{"en", "ja"}}, col.update)
	col.wait(t, 2)
	if b.maxRun != 1 || tr.Cache.Len() != 0 {
		t.Errorf("run %d, cache %d", b.maxRun, tr.Cache.Len())
	}
	if !strings.HasPrefix(b.calls[0], "自动检测>") {
		t.Errorf("unknown source should be sent as auto: %v", b.calls)
	}
}

func TestTranslatorSkipsCacheWriteWhenEngineChanged(t *testing.T) {
	b := &fakeBackend{info: EngineInfo{"google", "G"}, fp: "microsoft"}
	tr := &Translator{Backend: b, Cache: NewCache("")}
	col := newCollector(1)
	tr.Start(context.Background(), Request{Text: "hi", Source: "en", Codes: []string{"ja"}}, col.update)
	col.wait(t, 1)
	if tr.Cache.Len() != 0 {
		t.Fatal("a translation made by another engine was cached under this one")
	}
}

func TestTranslatorCancelledSessionReportsNothing(t *testing.T) {
	b := &fakeBackend{info: EngineInfo{"google", "G"}, delay: 50 * time.Millisecond}
	tr := &Translator{Backend: b, Cache: NewCache("")}
	ctx, cancel := context.WithCancel(context.Background())
	col := newCollector(1)
	tr.Start(ctx, Request{Text: "hi", Source: "en", Codes: []string{"ja"}}, col.update)
	time.Sleep(10 * time.Millisecond)
	cancel()
	select {
	case <-col.done:
		t.Fatal("update after cancel")
	case <-time.After(150 * time.Millisecond):
	}
	if tr.Cache.Len() != 0 {
		t.Fatal("cancelled result cached")
	}
}

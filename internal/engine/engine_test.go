package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/OwO-Network/DeepLX/translate"
)

func TestGooglePrimary(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("tl") != "zh-CN" || r.URL.Query().Get("q") != "hello" {
			t.Errorf("bad query %s", r.URL.RawQuery)
		}
		w.Write([]byte(`[[["你好","hello",null,null,10]],null,"en"]`))
	}))
	defer srv.Close()
	g := &Google{Client: srv.Client(), PrimaryURL: srv.URL}
	res, err := g.Translate(context.Background(), "hello", Auto, "简体中文")
	if err != nil || res.Text != "你好" || res.Detected != "en" {
		t.Fatalf("got %+v, %v", res, err)
	}
}

func TestGoogleFallbackWhenPrimaryRejected(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(429) }))
	defer bad.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[["你好","en"]]`))
	}))
	defer good.Close()
	g := &Google{Client: good.Client(), PrimaryURL: bad.URL, FallbackURL: good.URL}
	res, err := g.Translate(context.Background(), "hello", Auto, "简体中文")
	if err != nil || res.Text != "你好" || res.Detected != "en" {
		t.Fatalf("got %+v, %v", res, err)
	}
}

func TestOpenAICompatScrubsKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-secret" {
			t.Errorf("auth header %q", r.Header.Get("Authorization"))
		}
		w.WriteHeader(401)
		w.Write([]byte(`bad key sk-secret`))
	}))
	defer srv.Close()
	o, err := NewOpenAICompat(srv.URL, "m", "sk-secret")
	if err != nil {
		t.Fatal(err)
	}
	_, err = o.Translate(context.Background(), "hi", Auto, "简体中文")
	if err == nil || strings.Contains(err.Error(), "sk-secret") || !strings.Contains(err.Error(), "401") {
		t.Fatalf("error %v", err)
	}
}

func TestOpenAICompatOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path %s", r.URL.Path)
		}
		w.Write([]byte(`{"choices":[{"message":{"content":" 你好 "}}]}`))
	}))
	defer srv.Close()
	o, _ := NewOpenAICompat(srv.URL+"/", "m", "k")
	res, err := o.Translate(context.Background(), "hi", "英语", "简体中文")
	if err != nil || res.Text != "你好" {
		t.Fatalf("got %+v, %v", res, err)
	}
}

func TestDeepLPlanAndTarget(t *testing.T) {
	d, _ := NewDeepL("abc:fx")
	if d.BaseURL != "https://api-free.deepl.com" {
		t.Fatal(d.BaseURL)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"translations":[{"text":"你好","detected_source_language":"EN"}]}`))
	}))
	defer srv.Close()
	d.BaseURL, d.Client = srv.URL, srv.Client()
	res, err := d.Translate(context.Background(), "hello", Auto, "简体中文")
	if err != nil || res.Text != "你好" || res.Detected != "EN" {
		t.Fatalf("got %+v, %v", res, err)
	}
	if _, err := d.Translate(context.Background(), "x", Auto, "火星语"); err == nil {
		t.Fatal("expected unsupported target error")
	}
}

func TestSplitAndJoinKeepsText(t *testing.T) {
	text := strings.Repeat("这是一句话。", 400)
	chunks := splitText(text, 1000)
	if len(chunks) < 2 {
		t.Fatalf("chunks %d", len(chunks))
	}
	var parts []chunkPart
	for _, c := range chunks {
		if len([]rune(c)) > 1000 {
			t.Fatalf("chunk too long: %d", len([]rune(c)))
		}
		parts = append(parts, chunkPart{"", c, ""})
	}
	if joinChunks(parts) != text {
		t.Fatal("CJK chunks must rejoin without extra spaces")
	}
	got := joinChunks([]chunkPart{{"", "First sentence.", ""}, {"", "Next one.", ""}})
	if got != "First sentence. Next one." {
		t.Fatalf("got %q", got)
	}
}

func TestParseWebPage(t *testing.T) {
	html := `x IG:"ABC123" y data-iid="translator.5024" z params_AbusePreventionHelper = [1700000000000,"tok-en",3600000];`
	ig, iid, key, tok, exp, err := parseWebPage(html)
	if err != nil || ig != "ABC123" || iid != "translator.5024" || key != 1700000000000 || tok != "tok-en" || exp.Hours() != 1 {
		t.Fatalf("got %v %v %v %v %v %v", ig, iid, key, tok, exp, err)
	}
	if _, _, _, _, _, err := parseWebPage("<html></html>"); err == nil {
		t.Fatal("expected error")
	}
}

func TestMicrosoftWebMode(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/translator":
			w.Write([]byte(`IG:"I" data-iid="D" params_AbusePreventionHelper = [5,"T",3600000];`))
		case "/ttranslatev3":
			r.ParseForm()
			calls++
			if r.PostForm.Get("token") != "T" || r.PostForm.Get("to") != "zh-Hans" || r.PostForm.Get("text") != "hello" {
				t.Errorf("form %v", r.PostForm)
			}
			w.Write([]byte(`[{"detectedLanguage":{"language":"en"},"translations":[{"text":"你好","to":"zh-Hans"}]}]`))
		}
	}))
	defer srv.Close()
	m := NewMicrosoft("", "")
	m.EntryURL = srv.URL + "/translator"
	res, err := m.Translate(context.Background(), "hello", Auto, "简体中文")
	if err != nil || res.Text != "你好" || res.Detected != "en" || calls != 1 {
		t.Fatalf("got %+v, %v, calls %d", res, err, calls)
	}
}

func TestMicrosoftAzureMode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Ocp-Apim-Subscription-Key") != "k" || r.Header.Get("Ocp-Apim-Subscription-Region") != "eastasia" {
			t.Errorf("headers %v", r.Header)
		}
		w.Write([]byte(`[{"translations":[{"text":"你好"}]}]`))
	}))
	defer srv.Close()
	m := NewMicrosoft("k", "eastasia")
	m.AzureURL = srv.URL
	res, err := m.Translate(context.Background(), "hello", "英语", "简体中文")
	if err != nil || res.Text != "你好" {
		t.Fatalf("got %+v, %v", res, err)
	}
}

func TestParseMicrosoftTokenExpired(t *testing.T) {
	_, expired, err := parseMicrosoft([]byte(`{"statusCode":205}`))
	if err == nil || !expired {
		t.Fatalf("expired=%v err=%v", expired, err)
	}
}

func TestBuiltinMapsLanguagesAndCoolsDown(t *testing.T) {
	old := builtinCall
	defer func() { builtinCall = old; builtinUntil = time.Time{} }()
	var gotSrc, gotTgt string
	builtinCall = func(s, tg, text, _, _, _ string) (translate.DeepLXTranslationResult, error) {
		gotSrc, gotTgt = s, tg
		return translate.DeepLXTranslationResult{Code: 200, Data: "你好", SourceLang: "EN"}, nil
	}
	r, err := NewBuiltin().Translate(context.Background(), "hello", "自动检测", "简体中文")
	if err != nil || r.Text != "你好" || r.Detected != "英语" || gotSrc != "auto" || gotTgt != "ZH" {
		t.Fatalf("got %+v %v %s %s", r, err, gotSrc, gotTgt)
	}
	NewBuiltin().Translate(context.Background(), "hi", "自动检测", "英语")
	if gotTgt != "EN-US" {
		t.Fatalf("target %s", gotTgt)
	}
	builtinCall = func(_, _, _, _, _, _ string) (translate.DeepLXTranslationResult, error) {
		return translate.DeepLXTranslationResult{Code: 429, Message: "Too many requests"}, nil
	}
	if _, err := NewBuiltin().Translate(context.Background(), "x", "自动检测", "简体中文"); err == nil {
		t.Fatal("want rate limit error")
	}
	calls := 0
	builtinCall = func(_, _, _, _, _, _ string) (translate.DeepLXTranslationResult, error) {
		calls++
		return translate.DeepLXTranslationResult{Code: 200, Data: "a"}, nil
	}
	if _, err := NewBuiltin().Translate(context.Background(), "x", "自动检测", "简体中文"); err == nil || calls != 0 {
		t.Fatalf("cooldown should block, calls=%d err=%v", calls, err)
	}
}

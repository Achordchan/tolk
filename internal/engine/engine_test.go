package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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

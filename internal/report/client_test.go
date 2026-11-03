package report

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSendFallsBackOnlyWhenRelayIsUnreachable(t *testing.T) {
	var got map[string]any
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Tolk-Report") != "1" {
			t.Error("missing client header")
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"ok":true,"number":7,"url":"https://github.com/x/y/issues/7"}`))
	}))
	defer good.Close()
	blocked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		_, _ = w.Write([]byte("<html>blocked</html>"))
	}))
	defer blocked.Close()
	refusing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"ok":false,"error":"missing fields"}`))
	}))
	defer refusing.Close()

	old := Endpoints
	defer func() { Endpoints = old }()
	r := Report{Kind: Manual, Title: "t", InstallID: "ab", Version: "2.2.3"}.Clean()

	Endpoints = []string{blocked.URL, good.URL}
	res, err := Send(context.Background(), nil, r)
	if err != nil || res.Number != 7 {
		t.Fatalf("fallback failed: %v %+v", err, res)
	}
	if got["body"] == "" || got["kind"] != Manual {
		t.Fatalf("payload %v", got)
	}

	Endpoints = []string{refusing.URL, good.URL}
	if _, err := Send(context.Background(), nil, r); !errors.Is(err, ErrRejected) {
		t.Fatalf("a refusal must not be retried elsewhere: %v", err)
	}
}

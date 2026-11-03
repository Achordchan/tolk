package report

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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
	var blockedID string
	blocked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p map[string]any
		_ = json.NewDecoder(r.Body).Decode(&p)
		blockedID, _ = p["submissionId"].(string)
		w.WriteHeader(403)
		_, _ = w.Write([]byte("<html>blocked</html>"))
	}))
	defer blocked.Close()
	refusing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"ok":false,"error":"missing fields"}`))
	}))
	defer refusing.Close()

	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(502)
		_, _ = w.Write([]byte(`{"error":"bad gateway"}`))
	}))
	defer gateway.Close()

	old := Endpoints
	defer func() { Endpoints = old }()
	r := Report{Kind: Manual, Title: "t", InstallID: "ab", Version: "2.2.3", Contact: "me@example.com"}.Clean()

	Endpoints = []string{blocked.URL, good.URL}
	res, err := Send(context.Background(), nil, r)
	if err != nil || res.Number != 7 {
		t.Fatalf("fallback failed: %v %+v", err, res)
	}
	if got["body"] == "" || got["kind"] != Manual {
		t.Fatalf("payload %v", got)
	}
	// Both endpoints see the same submission, so the relay can tell a repeat.
	if id, _ := got["submissionId"].(string); id == "" || id != blockedID {
		t.Fatalf("submission id %q vs %q", got["submissionId"], blockedID)
	}
	// The relay re-scrubs the body, so the contact must travel beside it.
	if got["contact"] != "me@example.com" || strings.Contains(got["body"].(string), "me@example.com") {
		t.Fatalf("contact handling: %v", got)
	}

	// A gateway's JSON error is not the relay's verdict: try the spare.
	Endpoints = []string{gateway.URL, good.URL}
	if res, err := Send(context.Background(), nil, r); err != nil || res.Number != 7 {
		t.Fatalf("gateway error must fall back: %v %+v", err, res)
	}

	Endpoints = []string{refusing.URL, good.URL}
	if _, err := Send(context.Background(), nil, r); !errors.Is(err, ErrRejected) {
		t.Fatalf("a refusal must not be retried elsewhere: %v", err)
	}
}

func TestPayloadFitsAfterEscaping(t *testing.T) {
	// Control characters become \u00XX in JSON: six bytes each.
	noisy := strings.Repeat("\x01\x02\\\"", 6000)
	r := Report{Kind: Crash, Title: "t", InstallID: "ab", Version: "2.2.3", Fingerprint: "ff", SubmissionID: "aa",
		Description: strings.Repeat("说明\\", 2000),
		Attachments: []Attachment{{Name: "a", Body: noisy}, {Name: "b", Body: noisy}, {Name: "c", Body: noisy, KeepHead: true}}}
	payload, err := encode(r)
	if err != nil || len(payload) > MaxPayload {
		t.Fatalf("payload %d bytes, %v", len(payload), err)
	}
	var p map[string]any
	if err := json.Unmarshal(payload, &p); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"attachments", "description", "env"} {
		if _, ok := p[k]; ok {
			t.Fatalf("%s sent twice", k)
		}
	}
	if !strings.Contains(p["body"].(string), "<summary>a</summary>") {
		t.Fatal("logs dropped although a shorter cut fits")
	}
}

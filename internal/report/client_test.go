package report

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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

func TestSendStopsWhileEarlierAttemptIsPending(t *testing.T) {
	calls := 0
	pending := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(409)
		_, _ = w.Write([]byte(`{"ok":false,"pending":true,"error":"上一次提交还在处理中"}`))
	}))
	defer pending.Close()
	old := Endpoints
	defer func() { Endpoints = old }()
	Endpoints = []string{pending.URL, pending.URL}
	r := Report{Kind: Manual, Title: "t", InstallID: "ab", Version: "2.2.3", SubmissionID: "aa"}.Clean()
	res, err := Send(context.Background(), nil, r)
	if !errors.Is(err, ErrPending) || !res.Pending || calls != 1 {
		t.Fatalf("err %v, res %+v, calls %d", err, res, calls)
	}
}

func TestUnsureOutcomeIsPendingNotFallback(t *testing.T) {
	hang := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body) // got the report, then no answer in time
		time.Sleep(300 * time.Millisecond)
	}))
	defer hang.Close()
	down := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	downURL := down.URL
	down.Close() // connection refused: nothing was sent
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"number":9,"url":"u"}`))
	}))
	defer good.Close()
	old := Endpoints
	defer func() { Endpoints = old }()
	client := &http.Client{Timeout: 100 * time.Millisecond}
	r := Report{Kind: Manual, Title: "t", InstallID: "ab", Version: "2.2.3", SubmissionID: "aa"}.Clean()

	Endpoints = []string{hang.URL, downURL}
	if res, err := Send(context.Background(), client, r); !errors.Is(err, ErrPending) || !res.Pending {
		t.Fatalf("a report that may have gone through must stay pending: %v %+v", err, res)
	}
	Endpoints = []string{downURL, downURL}
	if _, err := Send(context.Background(), client, r); err == nil || errors.Is(err, ErrPending) {
		t.Fatalf("an unreachable relay filed nothing: %v", err)
	}
	Endpoints = []string{hang.URL, good.URL}
	if res, err := Send(context.Background(), client, r); err != nil || res.Number != 9 {
		t.Fatalf("the spare address settles it: %v %+v", err, res)
	}
}

func TestTimeoutWhileWritingHasNoRace(t *testing.T) {
	// The server takes the connection but never reads, so the client gives
	// up mid-write and the transport reports the write from its own
	// goroutine, possibly after post returns. Run with -race. A write that
	// failed sent an incomplete request, which the relay cannot act on; one
	// not yet reported counts as unsure. Either way, never a success.
	release := make(chan struct{})
	stall := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	defer stall.Close()
	defer close(release)
	client := &http.Client{Timeout: 200 * time.Millisecond}
	big := bytes.Repeat([]byte("x"), 16<<20)
	for i := 0; i < 3; i++ {
		if _, err := post(context.Background(), client, stall.URL, big); err == nil {
			t.Fatal("a stalled request cannot succeed")
		}
	}
}

func TestUnreadableSuccessIsPending(t *testing.T) {
	cut := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"numb`)) // filed, answer cut short
	}))
	defer cut.Close()
	down := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	downURL := down.URL
	down.Close()
	old := Endpoints
	defer func() { Endpoints = old }()
	Endpoints = []string{cut.URL, downURL}
	r := Report{Kind: Manual, Title: "t", InstallID: "ab", Version: "2.2.3", SubmissionID: "aa"}.Clean()
	if res, err := Send(context.Background(), nil, r); !errors.Is(err, ErrPending) || !res.Pending {
		t.Fatalf("a cut-short success must stay pending: %v %+v", err, res)
	}
}

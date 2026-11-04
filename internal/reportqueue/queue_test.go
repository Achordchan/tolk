package reportqueue

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"tolk/internal/report"
)

func TestQueueRetainsFailureAndAcknowledgesSuccess(t *testing.T) {
	dir := t.TempDir()
	report.NewStore(dir).Get()
	r := report.Report{SubmissionID: "abc123", Title: "test", Description: "failure", Kind: report.Crash}
	if err := Enqueue(dir, r); err != nil {
		t.Fatal(err)
	}
	old := report.Endpoints
	defer func() { report.Endpoints = old }()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "offline", 503) }))
	defer server.Close()
	report.Endpoints = []string{server.URL}
	if Flush(context.Background(), dir) {
		t.Fatal("failed report removed")
	}
	if _, err := os.Stat(filepath.Join(dir, "report_queue", "abc123.json")); err != nil {
		t.Fatal(err)
	}
	success := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"url":"https://github.com/Achordchan/tolk/issues/1"}`))
	}))
	defer success.Close()
	report.Endpoints = []string{success.URL}
	if !Flush(context.Background(), dir) {
		t.Fatal("acknowledged report not removed")
	}
}
func TestConsentAndIDs(t *testing.T) {
	dir := t.TempDir()
	if Enabled(dir) {
		t.Fatal("missing consent file must fail closed")
	}
	store := report.NewStore(dir)
	store.Get()
	store.Update(func(s *report.State) { s.AutoDisabled = true })
	if Enabled(dir) {
		t.Fatal("disabled consent ignored")
	}
	if err := Enqueue(dir, report.Report{SubmissionID: "../escape"}); err == nil {
		t.Fatal("unsafe id accepted")
	}
}

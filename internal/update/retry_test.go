package update

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestGetRetriesServerErrors(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			http.Error(w, "busy", http.StatusBadGateway)
			return
		}
		w.Write([]byte("ok"))
	}))
	defer srv.Close()
	c := &Checker{Client: &http.Client{Timeout: 5 * time.Second}}
	b, err := c.get(context.Background(), srv.URL, 10)
	if err != nil || string(b) != "ok" || n.Load() != 2 {
		t.Fatalf("got %q, %v after %d requests", b, err, n.Load())
	}
}

func TestGetDoesNotRetryRefusals(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		http.Error(w, "no", http.StatusNotFound)
	}))
	defer srv.Close()
	c := &Checker{Client: &http.Client{Timeout: 5 * time.Second}}
	if _, err := c.get(context.Background(), srv.URL, 10); err == nil || n.Load() != 1 {
		t.Fatalf("err %v after %d requests", err, n.Load())
	}
}

var (
	dialer     net.Dialer
	errRefused = errors.New("refused")
)

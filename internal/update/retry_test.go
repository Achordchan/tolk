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

func TestGetAssetFallsBackToProxy(t *testing.T) {
	var asked string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.Path
		w.Write([]byte("sig"))
	}))
	defer proxy.Close()
	old := GitHubProxy
	GitHubProxy = proxy.URL + "/"
	defer func() { GitHubProxy = old }()
	// github.com itself cannot be reached; the proxy can.
	direct := "https://github.com/x/y/releases/download/v1/a.sig.json"
	c := &Checker{Client: &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if addr == "github.com:443" {
				return nil, errRefused
			}
			return dialer.DialContext(ctx, network, addr)
		},
	}}}
	b, err := c.getAsset(context.Background(), direct, 10)
	if err != nil || string(b) != "sig" || asked != "/"+direct {
		t.Fatalf("got %q, %v; proxy asked for %q", b, err, asked)
	}
}

func TestProxiedOnlyGitHub(t *testing.T) {
	if proxied("https://gitee.com/a/b") != "" {
		t.Fatal("gitee links must not go through the GitHub proxy")
	}
	if got := proxied("https://github.com/a/b"); got != GitHubProxy+"https://github.com/a/b" {
		t.Fatalf("got %q", got)
	}
}

var (
	dialer     net.Dialer
	errRefused = errors.New("refused")
)

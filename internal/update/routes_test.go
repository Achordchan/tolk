package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A proxy stands in front of the origin: /<origin URL> serves what the origin
// would, as the public GitHub proxies do.
func proxyOf(t *testing.T, origin http.Handler, delay time.Duration) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(delay)
		u := strings.TrimPrefix(r.URL.Path, "/")
		i := strings.Index(u, "://")
		if i < 0 {
			http.NotFound(w, r)
			return
		}
		rest := u[i+3:]
		r2 := r.Clone(r.Context())
		r2.URL.Path = rest[strings.Index(rest, "/"):]
		origin.ServeHTTP(w, r2)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestLatestTakesNewestAnswer(t *testing.T) {
	release := func(tag string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{"tag_name": tag})
		})
	}
	origin := httptest.NewServer(release("v2.0.1"))
	defer origin.Close()
	stale := proxyOf(t, release("v2.0.0"), 0) // a proxy still holding the old copy answers first
	c := &Checker{Client: &http.Client{Timeout: 5 * time.Second}, Origin: origin.URL + "/", Mirrors: []string{stale.URL + "/"}}
	c.Latest = origin.URL + "/latest.json"
	r, from, err := c.latest(context.Background())
	if err != nil || r.Tag != "v2.0.1" || from != "GitHub" {
		t.Fatalf("%+v %q %v", r, from, err)
	}
}

func TestFetchUsesMirrorWhenOriginFails(t *testing.T) {
	good := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("sig")) })
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "no", 403) }))
	defer origin.Close()
	mirror := proxyOf(t, good, 0)
	c := &Checker{Client: &http.Client{Timeout: 5 * time.Second}, Origin: origin.URL + "/", Mirrors: []string{mirror.URL + "/"}}
	b, err := c.fetch(context.Background(), origin.URL+"/x/a.sig.json", 10, nil)
	if err != nil || string(b) != "sig" {
		t.Fatalf("%q %v", b, err)
	}
}

func TestRankPrefersFasterRoute(t *testing.T) {
	body := strings.Repeat("x", 64<<10)
	slowOrigin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body[:1024]))
		w.(http.Flusher).Flush()
		time.Sleep(time.Second)
		w.Write([]byte(body[1024:]))
	}))
	defer slowOrigin.Close()
	fast := proxyOf(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }), 0)
	broken := proxyOf(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "x", 502) }), 0)
	c := &Checker{Client: &http.Client{Timeout: 5 * time.Second}, Origin: slowOrigin.URL + "/", Mirrors: []string{broken.URL + "/", fast.URL + "/"}}
	u := slowOrigin.URL + "/pkg.zip"
	got := c.rank(context.Background(), c.routes(u))
	want := []string{fast.URL + "/" + u, u, broken.URL + "/" + u}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("got %v\nwant %v", got, want)
	}
}

func TestRoutesOnlyForOrigin(t *testing.T) {
	c := &Checker{Origin: "https://github.com/", Mirrors: []string{"https://m/"}}
	if r := c.routes("https://example.com/a"); len(r) != 1 {
		t.Fatal(r)
	}
	if r := c.routes("https://github.com/a"); len(r) != 2 || r[1] != "https://m/https://github.com/a" {
		t.Fatal(r)
	}
}

func TestFetchSkipsInvalidAnswer(t *testing.T) {
	// The proxy answers 200 at once with a challenge page; the origin is slower
	// but right.
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer origin.Close()
	page := proxyOf(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("<html>verify</html>")) }), 0)
	c := &Checker{Client: &http.Client{Timeout: 5 * time.Second}, Origin: origin.URL + "/", Mirrors: []string{page.URL + "/"}}
	isJSON := func(b []byte) error {
		var v map[string]any
		return json.Unmarshal(b, &v)
	}
	b, err := c.fetch(context.Background(), origin.URL+"/a.json", 100, isJSON)
	if err != nil || string(b) != `{"ok":true}` {
		t.Fatalf("%q %v", b, err)
	}
}

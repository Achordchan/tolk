package update

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
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

func TestDownloadMovesOnWhenPackageFailsVerify(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("baad")) }))
	t.Cleanup(bad.Close)
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond) // slower, so it is ranked after the bad one
		w.Write([]byte("good"))
	}))
	t.Cleanup(good.Close)
	c := NewChecker("windows", "1.0.0")
	c.Origin, c.Mirrors = bad.URL+"/", []string{good.URL + "/"}
	c.verify = func(_ *Info, path string) error {
		b, _ := os.ReadFile(path)
		if string(b) != "good" {
			return errors.New("bad package")
		}
		return nil
	}
	info := &Info{name: "p.zip", url: bad.URL + "/pkg", wantSize: 4}
	path, err := c.Download(context.Background(), info, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != "good" {
		t.Fatalf("got %q", b)
	}
	c.Mirrors = nil
	if _, err := c.Download(context.Background(), info, t.TempDir(), nil); err == nil {
		t.Fatal("a package that fails verification on every route must fail")
	}
}

func TestSignatureFromMirrorMustVerify(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	_, other, _ := ed25519.GenerateKey(nil)
	name := WindowsAssetName("2.0.0")
	sig := func(k ed25519.PrivateKey) []byte {
		p := Payload{AppVersion: "2.0.0", Filename: name, PackageType: "windows_full_update", Platform: "windows", SHA256: "ab", SizeBytes: 4}
		b, _ := json.Marshal(SigJSON{Payload: p, Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(k, p.Canonical()))})
		return b
	}
	// The origin answers first with metadata signed by someone else; the
	// slower mirror has the real one.
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(sig(other)) }))
	t.Cleanup(origin.Close)
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Write(sig(priv))
	}))
	t.Cleanup(mirror.Close)
	c := NewChecker("windows", "1.0.0")
	c.Origin, c.Mirrors = origin.URL+"/", []string{mirror.URL + "/"}
	c.windowsKey = base64.StdEncoding.EncodeToString(pub)
	r := &ghRelease{Tag: "v2.0.0", Assets: []ghAsset{
		{Name: name, Size: 4, URL: origin.URL + "/pkg"},
		{Name: name + ".sig.json", URL: origin.URL + "/sig"},
	}}
	info, err := c.windowsInfo(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyPayload(c.windowsKey, Payload{AppVersion: "2.0.0", Filename: name, PackageType: "windows_full_update", Platform: "windows", SHA256: "ab", SizeBytes: 4}, info.sig); err != nil {
		t.Fatal("took the badly signed metadata:", err)
	}
}

func TestLatestPrefersGitHubOverMirrorClaims(t *testing.T) {
	release := func(tag string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{"tag_name": tag})
		})
	}
	origin := httptest.NewServer(release("v2.0.0"))
	defer origin.Close()
	liar := httptest.NewServer(release("v9.9.9")) // a mirror claiming a version that does not exist
	defer liar.Close()
	c := &Checker{Client: &http.Client{Timeout: 5 * time.Second}, Origin: origin.URL + "/", Mirrors: []string{liar.URL + "/"}}
	c.Latest = origin.URL + "/latest.json"
	r, from, err := c.latest(context.Background())
	if err != nil || r.Tag != "v2.0.0" || from != "GitHub" {
		t.Fatalf("%+v %q %v", r, from, err)
	}
}

func TestStaleMirrorAppcastLosesToDirect(t *testing.T) {
	appcast := func(v string) []byte {
		return []byte(`<?xml version="1.0"?><rss xmlns:sparkle="http://www.andymatuschak.org/xml-namespaces/sparkle" version="2.0"><channel><item>
<sparkle:shortVersionString>` + v + `</sparkle:shortVersionString>
<enclosure url="https://example.com/dl/Tolk-` + v + `-macos.zip" length="1" sparkle:edSignature="c2ln"/></item></channel></rss>`)
	}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Write(appcast("2.0.0"))
	}))
	t.Cleanup(origin.Close)
	stale := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(appcast("1.9.0")) }))
	t.Cleanup(stale.Close)
	c := NewChecker("darwin", "1.0.0")
	c.OSVersion = ""
	c.Origin, c.Mirrors = origin.URL+"/", []string{stale.URL + "/"}
	info, err := c.macInfo(context.Background(), &ghRelease{Tag: "v2.0.0", Assets: []ghAsset{{Name: "appcast.xml", URL: origin.URL + "/appcast.xml"}}})
	if err != nil || info.Version != "2.0.0" {
		t.Fatalf("%+v %v", info, err)
	}
}

func TestAppcastFromGitHubWinsOverFasterMirror(t *testing.T) {
	appcast := func(sig string) []byte {
		return []byte(`<?xml version="1.0"?><rss xmlns:sparkle="http://www.andymatuschak.org/xml-namespaces/sparkle" version="2.0"><channel><item>
<sparkle:shortVersionString>2.0.0</sparkle:shortVersionString>
<enclosure url="https://example.com/dl/Tolk-2.0.0-macos.zip" length="1" sparkle:edSignature="` + sig + `"/></item></channel></rss>`)
	}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Write(appcast("good"))
	}))
	t.Cleanup(origin.Close)
	liar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(appcast("forged")) }))
	t.Cleanup(liar.Close)
	c := NewChecker("darwin", "1.0.0")
	c.OSVersion = ""
	c.Origin, c.Mirrors = origin.URL+"/", []string{liar.URL + "/"}
	info, err := c.macInfo(context.Background(), &ghRelease{Tag: "v2.0.0", Assets: []ghAsset{{Name: "appcast.xml", URL: origin.URL + "/appcast.xml"}}})
	if err != nil || info.sig != "good" {
		t.Fatalf("%+v %v", info, err)
	}
}

func TestDownloadReloadsSignatureWhenPackageFailsIt(t *testing.T) {
	pkg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("good")) }))
	t.Cleanup(pkg.Close)
	c := NewChecker("darwin", "1.0.0")
	c.Origin, c.Mirrors = "", nil
	c.verify = func(info *Info, _ string) error {
		if info.sig != "right" {
			return errors.New("bad signature")
		}
		return nil
	}
	info := &Info{name: "p.zip", url: pkg.URL + "/pkg", wantSize: 4, sig: "forged"}
	info.reload = func(context.Context) bool { info.sig = "right"; return true }
	if _, err := c.Download(context.Background(), info, t.TempDir(), nil); err != nil {
		t.Fatal(err)
	}
}

func TestDownloadReloadsSizeWhenPackageDiffers(t *testing.T) {
	pkg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("good")) }))
	t.Cleanup(pkg.Close)
	c := NewChecker("darwin", "1.0.0")
	c.Origin, c.Mirrors, c.verify = "", nil, nil
	info := &Info{name: "p.zip", url: pkg.URL + "/pkg", wantSize: 99, sig: "s"}
	info.reload = func(context.Context) bool { info.wantSize = 4; return true }
	if _, err := c.Download(context.Background(), info, t.TempDir(), nil); err != nil {
		t.Fatal(err)
	}
}

func TestAppcastFallsBackToOlderItemThisMacCanRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<?xml version="1.0"?><rss xmlns:sparkle="http://www.andymatuschak.org/xml-namespaces/sparkle" version="2.0"><channel>
<item><sparkle:shortVersionString>2.0.0</sparkle:shortVersionString><sparkle:minimumSystemVersion>99.0</sparkle:minimumSystemVersion>
<enclosure url="https://example.com/dl/Tolk-2.0.0-macos.zip" length="1" sparkle:edSignature="a"/></item>
<item><sparkle:shortVersionString>1.9.0</sparkle:shortVersionString><sparkle:minimumSystemVersion>13.0</sparkle:minimumSystemVersion>
<enclosure url="https://example.com/dl/Tolk-1.9.0-macos.zip" length="1" sparkle:edSignature="b"/></item>
</channel></rss>`))
	}))
	t.Cleanup(srv.Close)
	c := NewChecker("darwin", "1.0.0")
	c.OSVersion, c.Origin, c.Mirrors = "14.0", "", nil
	info, err := c.macInfo(context.Background(), &ghRelease{Tag: "v2.0.0", Assets: []ghAsset{{Name: "appcast.xml", URL: srv.URL + "/appcast.xml"}}})
	if err != nil || info.Version != "1.9.0" {
		t.Fatalf("%+v %v", info, err)
	}
}

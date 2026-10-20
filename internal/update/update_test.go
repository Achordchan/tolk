package update

import (
	"archive/zip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func makeZip(t *testing.T, files map[string]string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "pkg.zip")
	f, _ := os.Create(p)
	zw := zip.NewWriter(f)
	for name, body := range files {
		w, _ := zw.Create(name)
		w.Write([]byte(body))
	}
	zw.Close()
	f.Close()
	return p
}

func TestExtractZipRejectsEscapes(t *testing.T) {
	p := makeZip(t, map[string]string{"../evil.txt": "x"})
	if err := ExtractZip(p, t.TempDir()); err == nil {
		t.Fatal("path escape must be rejected")
	}
}

func TestPrepareWindowsChecksManifestAndRenamesExe(t *testing.T) {
	files := map[string]string{
		LegacyExeName:          "exe",
		"update_manifest.json": `{"app_version":"2.0.0","package_type":"windows_full_update"}`,
	}
	plan, err := PrepareWindows(makeZip(t, files), t.TempDir(), "2.0.0", "Tolk.exe")
	if err != nil {
		t.Fatal(err)
	}
	if plan.ExeName != "Tolk.exe" {
		t.Fatal(plan.ExeName)
	}
	if _, err := os.Stat(filepath.Join(plan.SourceDir, "Tolk.exe")); err != nil {
		t.Fatal("exe not renamed")
	}
	if _, err := PrepareWindows(makeZip(t, files), t.TempDir(), "2.0.1", ""); err == nil {
		t.Fatal("version mismatch must fail")
	}
	delete(files, "update_manifest.json")
	if _, err := PrepareWindows(makeZip(t, files), t.TempDir(), "2.0.0", ""); err == nil {
		t.Fatal("missing manifest must fail")
	}
}

// release server serving a signed Windows package.
func windowsServer(t *testing.T, priv ed25519.PrivateKey, tamper bool) (*httptest.Server, *Checker, []byte) {
	t.Helper()
	pkg := []byte("zip-bytes")
	mux := http.NewServeMux()
	var srv *httptest.Server
	name := WindowsAssetName("2.0.0")
	mux.HandleFunc("/release", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"tag_name": "v2.0.0",
			"body":     "# 更新日志\n- 新功能\nDZFYQ-SIG-WINDOWS:abc\nupdate=1\n",
			"assets": []map[string]any{
				{"name": name, "size": len(pkg), "browser_download_url": srv.URL + "/pkg"},
				{"name": name + ".sig.json", "size": 10, "browser_download_url": srv.URL + "/sig"},
			},
		})
	})
	mux.HandleFunc("/pkg", func(w http.ResponseWriter, r *http.Request) {
		if tamper {
			w.Write([]byte("zip-bytez"))
			return
		}
		w.Write(pkg)
	})
	mux.HandleFunc("/sig", func(w http.ResponseWriter, r *http.Request) {
		sum := fmt.Sprintf("%x", sha256Sum(pkg))
		p := Payload{AppVersion: "2.0.0", Filename: name, PackageType: "windows_full_update", Platform: "windows", SHA256: sum, SizeBytes: int64(len(pkg))}
		s := SigJSON{Payload: p, Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(priv, p.Canonical()))}
		json.NewEncoder(w).Encode(s)
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := NewChecker("windows", "1.2.11")
	c.Sources = []Source{{"Test", srv.URL + "/release"}}
	return srv, c, pkg
}

func TestWindowsCheckDownloadVerify(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	_, c, _ := windowsServer(t, priv, false)
	info, err := c.Check(context.Background(), "1.2.11")
	if err != nil || info == nil {
		t.Fatalf("check: %v %v", info, err)
	}
	if info.Version != "2.0.0" || !info.Force || strings.Contains(info.Notes, "DZFYQ") || strings.Contains(info.Notes, "update=1") {
		t.Fatalf("info %+v", info)
	}
	if again, _ := c.Check(context.Background(), "2.0.0"); again != nil {
		t.Fatal("up to date must return nil")
	}
	path, err := c.Download(context.Background(), info, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	// Verify uses the embedded key, so swap in the test key through the payload check.
	sum, size, _ := SHA256File(path)
	if err := VerifyPayload(base64.StdEncoding.EncodeToString(pub), Payload{AppVersion: "2.0.0", Filename: info.name, PackageType: "windows_full_update", Platform: "windows", SHA256: sum, SizeBytes: size}, info.sig); err != nil {
		t.Fatal(err)
	}
	if err := Verify(info, path); err == nil {
		t.Fatal("a test-key signature must not pass the embedded production key")
	}
}

func TestDownloadRejectsWrongSize(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(nil)
	_, c, _ := windowsServer(t, priv, true)
	info, err := c.Check(context.Background(), "1.2.11")
	if err != nil {
		t.Fatal(err)
	}
	// tampered body is the same length, so the download passes the size gate and
	// must then fail the hash comparison in Verify.
	path, err := c.Download(context.Background(), info, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(info, path); err == nil {
		t.Fatal("tampered package must fail verification")
	}
}

func TestMacCheckParsesAppcast(t *testing.T) {
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/release", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"tag_name": "v2.0.0", "body": "x", "assets": []map[string]any{
			{"name": "appcast.xml", "size": 1, "browser_download_url": srv.URL + "/appcast.xml"}}})
	})
	mux.HandleFunc("/appcast.xml", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<?xml version="1.0"?><rss xmlns:sparkle="http://www.andymatuschak.org/xml-namespaces/sparkle" version="2.0"><channel><item>
<sparkle:version>200</sparkle:version><sparkle:shortVersionString>2.0.0</sparkle:shortVersionString>
<enclosure url="https://example.com/dl/Tolk-2.0.0-macos.zip" length="1234" type="application/octet-stream" sparkle:edSignature="c2ln"/></item></channel></rss>`)
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()
	c := NewChecker("darwin", "1.4.1")
	c.Sources = []Source{{"Test", srv.URL + "/release"}}
	info, err := c.Check(context.Background(), "1.4.1")
	if err != nil || info == nil {
		t.Fatalf("%v %v", info, err)
	}
	if info.Version != "2.0.0" || info.Size != 1234 || info.url != "https://example.com/dl/Tolk-2.0.0-macos.zip" || info.sig != "c2ln" {
		t.Fatalf("%+v", info)
	}
}

func TestFallsBackToMirrorSource(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "no", 403) }))
	defer bad.Close()
	_, priv, _ := ed25519.GenerateKey(nil)
	_, c, _ := windowsServer(t, priv, false)
	c.Sources = append([]Source{{"Blocked", bad.URL}}, c.Sources...)
	info, err := c.Check(context.Background(), "1.2.11")
	if err != nil || info == nil || info.Source != "Test" {
		t.Fatalf("%+v %v", info, err)
	}
}

func TestMacApplyScript(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	root := t.TempDir()
	stubs := filepath.Join(root, "bin")
	os.MkdirAll(stubs, 0o755)
	stub := func(name, body string) {
		os.WriteFile(filepath.Join(stubs, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755)
	}
	stub("ditto", `cp -R "$1" "$2"`)
	stub("xattr", `exit 0`)
	stub("open", `echo "$1" >> "$OPENED"`)
	run := func(codesignOK bool) (target, result, opened string) {
		dir := t.TempDir()
		target = filepath.Join(dir, "App.app")
		newApp := filepath.Join(dir, "new", "App.app")
		os.MkdirAll(target, 0o755)
		os.MkdirAll(newApp, 0o755)
		os.WriteFile(filepath.Join(target, "v"), []byte("old"), 0o644)
		os.WriteFile(filepath.Join(newApp, "v"), []byte("new"), 0o644)
		if codesignOK {
			stub("codesign", `exit 0`)
		} else {
			stub("codesign", `exit 1`)
		}
		script := filepath.Join(dir, "apply.sh")
		os.WriteFile(script, []byte(macApplyScript), 0o700)
		cmd := exec.Command("sh", script)
		cmd.Env = append(os.Environ(), "PATH="+stubs+":"+os.Getenv("PATH"),
			"TOLK_UPDATE_PID=999999", "TOLK_UPDATE_NEW="+newApp, "TOLK_UPDATE_TARGET="+target,
			"TOLK_UPDATE_BACKUP="+filepath.Join(dir, "backup", "App.app"),
			"TOLK_UPDATE_LOG="+filepath.Join(dir, "log"), "TOLK_UPDATE_RESULT="+filepath.Join(dir, "result.json"),
			"TOLK_UPDATE_VERSION=2.0.0", "OPENED="+filepath.Join(dir, "opened"))
		os.MkdirAll(filepath.Join(dir, "backup"), 0o755)
		cmd.Run()
		b, _ := os.ReadFile(filepath.Join(target, "v"))
		r, _ := os.ReadFile(filepath.Join(dir, "result.json"))
		o, _ := os.ReadFile(filepath.Join(dir, "opened"))
		return string(b), string(r), string(o)
	}
	if v, res, opened := run(true); v != "new" || !strings.Contains(res, `"success"`) || !strings.Contains(opened, "App.app") {
		t.Fatalf("success path: v=%s res=%s opened=%s", v, res, opened)
	}
	if v, res, opened := run(false); v != "old" || !strings.Contains(res, `"failed"`) || !strings.Contains(opened, "App.app") {
		t.Fatalf("rollback path: v=%s res=%s opened=%s", v, res, opened)
	}
}

func sha256Sum(b []byte) [32]byte { return sha256.Sum256(b) }

func macAppcastServer(t *testing.T, minOS string) (*httptest.Server, string) {
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/release", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"tag_name": "v2.0.0", "body": "x", "assets": []map[string]any{
			{"name": "appcast.xml", "size": 1, "browser_download_url": srv.URL + "/appcast.xml"},
			{"name": "Tolk-2.0.0-macos.zip", "size": 1234, "browser_download_url": srv.URL + "/mirror/Tolk-2.0.0-macos.zip"}}})
	})
	mux.HandleFunc("/appcast.xml", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<?xml version="1.0"?><rss xmlns:sparkle="http://www.andymatuschak.org/xml-namespaces/sparkle" version="2.0"><channel><item>
<sparkle:version>200</sparkle:version><sparkle:shortVersionString>2.0.0</sparkle:shortVersionString><sparkle:minimumSystemVersion>%s</sparkle:minimumSystemVersion>
<enclosure url="https://github.example/dl/Tolk-2.0.0-macos.zip" length="1234" sparkle:edSignature="c2ln"/></item></channel></rss>`, minOS)
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, srv.URL + "/release"
}

func TestMacCheckSkipsReleaseNeedingNewerOS(t *testing.T) {
	_, rel := macAppcastServer(t, "15.0")
	c := NewChecker("darwin", "1.4.1")
	c.Sources, c.OSVersion = []Source{{"Test", rel}}, "14.5"
	if info, err := c.Check(context.Background(), "1.4.1"); info != nil || err != nil {
		t.Fatalf("an incompatible release must not be offered: %+v %v", info, err)
	}
	c.OSVersion = "15.1"
	if info, err := c.Check(context.Background(), "1.4.1"); err != nil || info == nil {
		t.Fatalf("compatible release expected: %+v %v", info, err)
	}
}

func TestMacCheckPrefersArchiveAttachedToSameRelease(t *testing.T) {
	srv, rel := macAppcastServer(t, "13.0")
	c := NewChecker("darwin", "1.4.1")
	c.Sources, c.OSVersion = []Source{{"Mirror", rel}}, "14.5"
	info, err := c.Check(context.Background(), "1.4.1")
	if err != nil || info == nil || info.url != srv.URL+"/mirror/Tolk-2.0.0-macos.zip" {
		t.Fatalf("%+v %v", info, err)
	}
}

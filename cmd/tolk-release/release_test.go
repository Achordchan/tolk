package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tolk/internal/update"
)

func TestWinpackSignNotesRoundTrip(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "tolk.exe")
	os.WriteFile(exe, []byte("MZ-fake"), 0o755)
	if err := winpack([]string{"-single-executable", "-exe", exe, "-version", "v2.0.0", "-out", dir}); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(dir, "Tolk-update-windows_2.0.0.zip")

	// The package must pass the same checks the client runs.
	if _, err := update.PrepareWindows(zipPath, t.TempDir(), "2.0.0", ""); err != nil {
		t.Fatal(err)
	}

	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	der, _ := x509.MarshalPKCS8PrivateKey(priv)
	keyFile := filepath.Join(dir, "key.pem")
	os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600)
	pubB64 := base64.StdEncoding.EncodeToString(pub)
	if err := winsign([]string{"-zip", zipPath, "-version", "2.0.0", "-key", keyFile, "-pub", pubB64}); err != nil {
		t.Fatal(err)
	}
	// The embedded production key must refuse this test signature.
	if err := winsign([]string{"-zip", zipPath, "-version", "2.0.0", "-key", keyFile}); err == nil {
		t.Fatal("wrong key must be rejected against the embedded public key")
	}
	raw, _ := os.ReadFile(zipPath + ".sig.json")
	var s update.SigJSON
	json.Unmarshal(raw, &s)
	if err := update.VerifyPayload(pubB64, s.Payload, s.Signature); err != nil {
		t.Fatal(err)
	}

	cl := filepath.Join(dir, "CHANGELOG.md")
	os.WriteFile(cl, []byte("# 更新日志\n\n## 2.0.0 - 2026-10-20\n\n- 新版本\n\n## 1.0.0\n\n- 旧\n"), 0o644)
	out := filepath.Join(dir, "notes.md")
	if err := notes([]string{"-version", "2.0.0", "-changelog", cl, "-out", out, "-force"}); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(out)
	text := string(body)
	for _, want := range []string{"- 新版本", "update=1"} {
		if !strings.Contains(text, want) {
			t.Errorf("notes missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "旧") {
		t.Error("notes must only hold the requested version")
	}
}

func TestAppcastSignsWithSparkleKey(t *testing.T) {
	dir := t.TempDir()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	zipPath := filepath.Join(dir, "Tolk-2.0.0-macos.zip")
	os.WriteFile(zipPath, []byte("zip"), 0o644)
	t.Setenv("SPARKLE_PRIVATE_KEY", base64.StdEncoding.EncodeToString(priv.Seed()))
	out := filepath.Join(dir, "appcast.xml")
	args := []string{"-zip", zipPath, "-version", "2.0.0", "-build", "20000", "-url", "https://x/Tolk-2.0.0-macos.zip", "-out", out}
	if err := appcast(append(args, "-pub", base64.StdEncoding.EncodeToString(pub))); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(out)
	for _, want := range []string{"<sparkle:version>20000</sparkle:version>", "<sparkle:shortVersionString>2.0.0</sparkle:shortVersionString>", `length="3"`, "sparkle:edSignature="} {
		if !strings.Contains(string(b), want) {
			t.Errorf("appcast missing %s", want)
		}
	}
	if err := appcast(args); err == nil {
		t.Fatal("a key that does not match SUPublicEDKey must be rejected")
	}
}

func TestKeygenWritesPrivateFilesAndOnlyPrintsPublicKeys(t *testing.T) {
	dir := t.TempDir()
	if err := keygen([]string{"-out", dir}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "tolk-windows-update-private.pem"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loadEd25519(b); err != nil {
		t.Fatal(err)
	}
	seed, _ := os.ReadFile(filepath.Join(dir, "tolk-sparkle-private.txt"))
	if _, err := sparklePrivateKey(string(seed)); err != nil {
		t.Fatal(err)
	}
	if err := keygen([]string{"-out", dir}); err == nil {
		t.Fatal("must not overwrite existing keys")
	}
}

func TestLatestDescribesRelease(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Tolk-update-windows_2.0.0.zip"), []byte("zip"), 0o644)
	os.WriteFile(filepath.Join(dir, "notes.md"), []byte("- 新\nupdate=1\n"), 0o644)
	out := filepath.Join(dir, "latest.json")
	if err := latest([]string{"-version", "2.0.0", "-dir", dir, "-notes", filepath.Join(dir, "notes.md"), "-out", out}); err != nil {
		t.Fatal(err)
	}
	var r struct {
		Tag    string `json:"tag_name"`
		Body   string `json:"body"`
		Assets []struct {
			Name string `json:"name"`
			Size int64  `json:"size"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	b, _ := os.ReadFile(out)
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	if r.Tag != "v2.0.0" || !strings.Contains(r.Body, "update=1") || len(r.Assets) != 2 {
		t.Fatalf("%s", b)
	}
	a := r.Assets[0]
	if a.Name != "Tolk-update-windows_2.0.0.zip" || a.Size != 3 || a.URL != "https://github.com/Achordchan/tolk/releases/download/v2.0.0/Tolk-update-windows_2.0.0.zip" {
		t.Fatalf("%+v", a)
	}
}

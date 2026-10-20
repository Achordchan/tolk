package update

import (
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestCanonicalMatchesPythonSigner(t *testing.T) {
	p := Payload{AppVersion: "v2.0.0", Filename: "dazuofanyiguan_full.for.windows_2.0.0.zip", PackageType: "windows_full_update", Platform: "windows", SHA256: "ABCDEF", SizeBytes: 12}
	// json.dumps(..., separators=(",", ":"), sort_keys=True, ensure_ascii=False)
	want := `{"app_version":"2.0.0","filename":"dazuofanyiguan_full.for.windows_2.0.0.zip","package_type":"windows_full_update","platform":"windows","sha256":"abcdef","size_bytes":12}`
	if got := string(p.Canonical()); got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestVerifyPayloadAndFile(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	pubB64 := base64.StdEncoding.EncodeToString(pub)
	p := Payload{AppVersion: "2.0.0", Filename: "a.zip", PackageType: "windows_full_update", Platform: "windows", SHA256: "00", SizeBytes: 1}
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, p.Canonical()))
	if err := VerifyPayload(pubB64, p, sig); err != nil {
		t.Fatal(err)
	}
	p.SizeBytes = 2
	if err := VerifyPayload(pubB64, p, sig); err == nil {
		t.Fatal("tampered payload must fail")
	}
	f := filepath.Join(t.TempDir(), "x.zip")
	os.WriteFile(f, []byte("data"), 0o600)
	fs := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, []byte("data")))
	if err := VerifyFileBytes(pubB64, f, fs); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(f, []byte("datA"), 0o600)
	if err := VerifyFileBytes(pubB64, f, fs); err == nil {
		t.Fatal("tampered file must fail")
	}
}

func TestEmbeddedKeysAreValid(t *testing.T) {
	for _, k := range []string{WindowsPublicKeyB64, SparklePublicKeyB64} {
		if _, err := publicKey(k); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{{"2.0.0", "1.2.11", 1}, {"1.2.11", "1.2.2", 1}, {"v1.0", "1.0.0", 0}, {"1.4.1", "2.0.0", -1}}
	for _, c := range cases {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("%s vs %s = %d", c.a, c.b, got)
		}
	}
}

// Signed by the real Python implementation (src/gongju/update_trust.py of
// dazuo-win) with a throwaway key: proves Go and Python agree byte for byte.
func TestVerifiesSignatureMadeByPythonSigner(t *testing.T) {
	p := Payload{AppVersion: "2.0.0", Filename: "dazuofanyiguan_full.for.windows_2.0.0.zip", PackageType: "windows_full_update", Platform: "windows", SizeBytes: 1234,
		SHA256: "abababababababababababababababababababababababababababababababab"}
	pub := "nYGUdQgLfsDH8VXIiv2PplFeGLwfEinGChWThe4XSdU="
	sig := "nhCjPqwrMkRAVpOT2dRgdDuG0ukJ2hgjzrNU5/x6h/c3U3JeWKAW+GA8we5ix0vl0kU5qTwxsI4q5ZLQAn6KDw=="
	if err := VerifyPayload(pub, p, sig); err != nil {
		t.Fatal(err)
	}
}

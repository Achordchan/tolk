// Package update checks for, verifies and installs Tolk releases.
//
// Windows keeps the signed full-ZIP format of dazuo-win (Ed25519 over a
// canonical JSON payload, same public key), so the old client, the bridge
// release and Tolk's own updates share one signing and verification path.
// macOS keeps the Sparkle appcast format (Ed25519 over the archive bytes, same
// SUPublicEDKey) for the same reason.
package update

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// Public keys of the existing release channels. The private halves stay in
// the maintainer's CI secrets.
const (
	WindowsPublicKeyB64 = "RUYCZIcVqIzgPf56cREkoGvQnYYiujdI/nG7EGiGPWA="
	SparklePublicKeyB64 = "uYKsIloVV+bt+R3StPtVAxMd2CPIHvmS5LZlbYmxQ0Y="
)

// Payload is what the Windows signature covers. Field order is the
// alphabetical key order of the Python signer (json.dumps sort_keys=True).
type Payload struct {
	AppVersion  string `json:"app_version"`
	Filename    string `json:"filename"`
	PackageType string `json:"package_type"`
	Platform    string `json:"platform"`
	SHA256      string `json:"sha256"`
	SizeBytes   int64  `json:"size_bytes"`
}

// NormalizeVersion strips a leading v/V and spaces.
func NormalizeVersion(v string) string {
	return strings.TrimLeft(strings.TrimSpace(v), "vV")
}

// Canonical is the exact byte string that gets signed.
func (p Payload) Canonical() []byte {
	p.AppVersion = NormalizeVersion(p.AppVersion)
	p.SHA256 = strings.ToLower(strings.TrimSpace(p.SHA256))
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(p)
	return bytes.TrimSuffix(b.Bytes(), []byte("\n"))
}

// SHA256File hashes a file.
func SHA256File(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func publicKey(b64 string) (ed25519.PublicKey, error) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil, errors.New("内置的更新公钥无效")
	}
	return ed25519.PublicKey(raw), nil
}

// VerifyPayload checks a Windows package signature against pubB64.
func VerifyPayload(pubB64 string, p Payload, sigB64 string) error {
	pub, err := publicKey(pubB64)
	if err != nil {
		return err
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(sigB64))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return errors.New("更新签名格式无效")
	}
	if !ed25519.Verify(pub, p.Canonical(), sig) {
		return errors.New("更新签名校验失败")
	}
	return nil
}

// VerifyFileBytes checks a Sparkle archive signature: Ed25519 over the file.
func VerifyFileBytes(pubB64, path, sigB64 string) error {
	pub, err := publicKey(pubB64)
	if err != nil {
		return err
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(sigB64))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return errors.New("更新签名格式无效")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !ed25519.Verify(pub, data, sig) {
		return errors.New("更新签名校验失败")
	}
	return nil
}

// SigJSON is the <package>.sig.json written next to a Windows package.
type SigJSON struct {
	Payload
	Signature string `json:"signature"`
}

func (s SigJSON) check(pubB64, name string, size int64, sum string) error {
	if s.Filename != name {
		return fmt.Errorf("签名文件名不匹配：%s", name)
	}
	if s.PackageType != "windows_full_update" || s.Platform != "windows" {
		return errors.New("签名元数据的包类型或平台不匹配")
	}
	if s.SizeBytes != size || !strings.EqualFold(s.SHA256, sum) {
		return errors.New("更新包大小或校验值与签名不一致")
	}
	return VerifyPayload(pubB64, s.Payload, s.Signature)
}

// CompareVersions returns -1, 0 or 1 comparing dotted numeric versions.
func CompareVersions(a, b string) int {
	pa, pb := parts(a), parts(b)
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			if pa[i] < pb[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

func parts(v string) [3]int {
	var out [3]int
	for i, s := range strings.SplitN(NormalizeVersion(v), ".", 4) {
		if i >= 3 {
			break
		}
		n := 0
		for _, c := range s {
			if c < '0' || c > '9' {
				break
			}
			n = n*10 + int(c-'0')
		}
		out[i] = n
	}
	return out
}

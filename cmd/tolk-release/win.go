package main

import (
	"archive/zip"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"tolk/internal/update"
)

// winpack writes the full-update ZIP in the layout the old updater verifies:
// the program under its legacy name, update_manifest.json and the resource
// directory marker src/ziyuan/logo.ico.
func winpack(args []string) error {
	fs := flag.NewFlagSet("winpack", flag.ExitOnError)
	exe := fs.String("exe", "", "新版 Windows 可执行文件")
	icon := fs.String("icon", "", "logo.ico")
	version := fs.String("version", "", "版本号")
	out := fs.String("out", "dist", "输出目录")
	extra := fs.String("extra", "", "额外文件，逗号分隔，放在包根目录（许可证等）")
	fs.Parse(args)
	if *exe == "" || *icon == "" || *version == "" {
		return errors.New("需要 -exe -icon -version")
	}
	v := update.NormalizeVersion(*version)
	if err := os.MkdirAll(*out, 0o755); err != nil {
		return err
	}
	name := filepath.Join(*out, update.WindowsAssetName(v))
	f, err := os.Create(name)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	manifest, _ := json.MarshalIndent(map[string]string{
		"app_version":  v,
		"package_type": "windows_full_update",
		"created_at":   time.Now().UTC().Format(time.RFC3339),
	}, "", "  ")
	files := []struct {
		name string
		src  string
		body []byte
	}{
		{update.LegacyExeName, *exe, nil},
		{"update_manifest.json", "", append(manifest, '\n')},
		{"src/ziyuan/logo.ico", *icon, nil},
	}
	for _, e := range strings.Split(*extra, ",") {
		if e = strings.TrimSpace(e); e != "" {
			files = append(files, struct {
				name string
				src  string
				body []byte
			}{filepath.Base(e), e, nil})
		}
	}
	for _, e := range files {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: e.name, Method: zip.Deflate})
		if err != nil {
			return err
		}
		if e.src == "" {
			_, err = w.Write(e.body)
		} else {
			err = copyFile(w, e.src)
		}
		if err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	fmt.Println(name)
	return nil
}

func copyFile(w io.Writer, path string) error {
	r, err := os.Open(path)
	if err != nil {
		return err
	}
	defer r.Close()
	_, err = io.Copy(w, r)
	return err
}

// loadEd25519 reads a PKCS#8 PEM private key.
func loadEd25519(pemBytes []byte) (ed25519.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("私钥不是 PEM 格式")
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("读取私钥失败：%w", err)
	}
	priv, ok := k.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("私钥不是 Ed25519")
	}
	return priv, nil
}

func winsign(args []string) error {
	fs := flag.NewFlagSet("winsign", flag.ExitOnError)
	zipPath := fs.String("zip", "", "全量包 zip")
	version := fs.String("version", "", "版本号")
	keyFile := fs.String("key", "", "私钥 PEM 文件（默认读环境变量 DZFYQ_UPDATE_PRIVATE_KEY_PEM）")
	pub := fs.String("pub", "", "用于自检的公钥（默认是 Tolk 内置公钥；-legacy 时是旧版客户端的公钥）")
	legacy := fs.Bool("legacy", false, "给旧版客户端的桥接版本签名：用旧版公钥自检")
	fs.Parse(args)
	if *pub == "" {
		*pub = update.WindowsPublicKeyB64
		if *legacy {
			*pub = update.LegacyWindowsPublicKeyB64
		}
	}
	if *zipPath == "" || *version == "" {
		return errors.New("需要 -zip -version")
	}
	var pemBytes []byte
	if *keyFile != "" {
		b, err := os.ReadFile(*keyFile)
		if err != nil {
			return err
		}
		pemBytes = b
	} else if v := strings.TrimSpace(os.Getenv("DZFYQ_UPDATE_PRIVATE_KEY_PEM")); v != "" {
		pemBytes = []byte(v)
	} else {
		return errors.New("缺少私钥：设置环境变量 DZFYQ_UPDATE_PRIVATE_KEY_PEM 或传 -key")
	}
	priv, err := loadEd25519(pemBytes)
	if err != nil {
		return err
	}
	sum, size, err := update.SHA256File(*zipPath)
	if err != nil {
		return err
	}
	p := update.Payload{
		AppVersion: update.NormalizeVersion(*version), Filename: filepath.Base(*zipPath),
		PackageType: "windows_full_update", Platform: "windows", SHA256: sum, SizeBytes: size,
	}
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, p.Canonical()))
	// A wrong private key fails here, not on users' machines.
	if err := update.VerifyPayload(*pub, p, sig); err != nil {
		return fmt.Errorf("签名与客户端内置公钥不匹配，已拒绝输出：%w", err)
	}
	markers := []string{"DZFYQ-SIG-WINDOWS:" + sig, "DZFYQ-SIG:" + sig}
	out := map[string]any{
		"app_version": p.AppVersion, "package_type": p.PackageType, "platform": p.Platform,
		"filename": p.Filename, "size_bytes": p.SizeBytes, "sha256": p.SHA256, "signature": sig,
		"release_notes_marker": markers[0], "release_notes_markers": markers,
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	dest := *zipPath + ".sig.json"
	if err := os.WriteFile(dest, append(b, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Println(dest)
	return nil
}

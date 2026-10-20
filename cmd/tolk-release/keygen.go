package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

// keygen makes Tolk's own update signing keys. The private halves go to files
// in -out (mode 0600) and are never printed; only the public keys are, so they
// can be pasted into internal/update/sig.go or shared.
func keygen(args []string) error {
	fs := flag.NewFlagSet("keygen", flag.ExitOnError)
	out := fs.String("out", "", "保存私钥文件的目录（不要放在仓库里）")
	fs.Parse(args)
	if *out == "" {
		return errors.New("需要 -out 目录")
	}
	if err := os.MkdirAll(*out, 0o700); err != nil {
		return err
	}
	winPath := filepath.Join(*out, "tolk-windows-update-private.pem")
	macPath := filepath.Join(*out, "tolk-sparkle-private.txt")
	for _, p := range []string{winPath, macPath} {
		if _, err := os.Stat(p); err == nil {
			return fmt.Errorf("%s 已存在，不覆盖", p)
		}
	}
	winPub, winPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	der, err := x509.MarshalPKCS8PrivateKey(winPriv)
	if err != nil {
		return err
	}
	if err := os.WriteFile(winPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		return err
	}
	macPub, macPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	if err := os.WriteFile(macPath, []byte(base64.StdEncoding.EncodeToString(macPriv.Seed())+"\n"), 0o600); err != nil {
		return err
	}
	fmt.Println("私钥已写入：")
	fmt.Println("  ", winPath, "  → GitHub Secret TOLK_UPDATE_PRIVATE_KEY_PEM（整个文件内容）")
	fmt.Println("  ", macPath, "  → GitHub Secret TOLK_SPARKLE_PRIVATE_KEY（文件里那一行）")
	fmt.Println()
	fmt.Println("公钥（可以公开，发给我写进应用里）：")
	fmt.Println("  WindowsPublicKeyB64 =", base64.StdEncoding.EncodeToString(winPub))
	fmt.Println("  SparklePublicKeyB64 =", base64.StdEncoding.EncodeToString(macPub))
	return nil
}

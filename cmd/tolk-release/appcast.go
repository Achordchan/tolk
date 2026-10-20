package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"
	"time"

	"tolk/internal/update"
)

// sparklePrivateKey accepts what Sparkle's generate_keys exports: base64 of
// the 32-byte seed, or of seed followed by the public key.
func sparklePrivateKey(b64 string) (ed25519.PrivateKey, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil {
		return nil, errors.New("Sparkle 私钥不是 base64")
	}
	switch len(raw) {
	case ed25519.SeedSize, ed25519.PrivateKeySize:
		return ed25519.NewKeyFromSeed(raw[:ed25519.SeedSize]), nil
	}
	return nil, fmt.Errorf("Sparkle 私钥长度异常：%d", len(raw))
}

func appcast(args []string) error {
	fs := flag.NewFlagSet("appcast", flag.ExitOnError)
	zipPath := fs.String("zip", "", "macOS 更新 zip（里面是 .app）")
	version := fs.String("version", "", "显示版本号，如 2.0.0")
	build := fs.String("build", "", "Sparkle 构建号（纯数字，必须大于线上已有的构建号）")
	url := fs.String("url", "", "zip 的下载地址")
	notes := fs.String("notes", "", "发布说明（markdown 文件，原样放进 description）")
	minOS := fs.String("min-os", "13.0", "最低系统版本")
	out := fs.String("out", "appcast.xml", "输出")
	title := fs.String("title", "Tolk", "频道标题")
	pub := fs.String("pub", "", "用于自检的公钥（默认是 Tolk 内置公钥；-legacy 时是 bagayalu 的 SUPublicEDKey）")
	legacy := fs.Bool("legacy", false, "给旧版 bagayalu 的桥接版本签名：用旧版公钥自检")
	fs.Parse(args)
	if *pub == "" {
		*pub = update.SparklePublicKeyB64
		if *legacy {
			*pub = update.LegacySparklePublicKeyB64
		}
	}
	if *zipPath == "" || *version == "" || *build == "" || *url == "" {
		return errors.New("需要 -zip -version -build -url")
	}
	priv, err := sparklePrivateKey(os.Getenv("SPARKLE_PRIVATE_KEY"))
	if err != nil {
		return err
	}
	data, err := os.ReadFile(*zipPath)
	if err != nil {
		return err
	}
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, data))
	if err := update.VerifyFileBytes(*pub, *zipPath, sig); err != nil {
		return fmt.Errorf("签名与应用内置的 SUPublicEDKey 不匹配，已拒绝输出：%w", err)
	}
	desc := ""
	if *notes != "" {
		b, err := os.ReadFile(*notes)
		if err != nil {
			return err
		}
		desc = "\n      <description><![CDATA[<pre>" + strings.ReplaceAll(html.EscapeString(string(b)), "]]>", "]]]]><![CDATA[>") + "</pre>]]></description>"
	}
	x := fmt.Sprintf(`<?xml version="1.0" standalone="yes"?>
<rss xmlns:sparkle="http://www.andymatuschak.org/xml-namespaces/sparkle" xmlns:dc="http://purl.org/dc/elements/1.1/" version="2.0">
  <channel>
    <title>%s</title>
    <item>
      <title>%s</title>
      <pubDate>%s</pubDate>
      <sparkle:version>%s</sparkle:version>
      <sparkle:shortVersionString>%s</sparkle:shortVersionString>
      <sparkle:minimumSystemVersion>%s</sparkle:minimumSystemVersion>%s
      <enclosure url="%s" length="%d" type="application/octet-stream" sparkle:edSignature="%s"/>
    </item>
  </channel>
</rss>
`, html.EscapeString(*title), html.EscapeString(*version), time.Now().UTC().Format(time.RFC1123Z),
		html.EscapeString(*build), html.EscapeString(update.NormalizeVersion(*version)), html.EscapeString(*minOS), desc,
		html.EscapeString(*url), len(data), sig)
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		return err
	}
	return os.WriteFile(*out, []byte(x), 0o644)
}

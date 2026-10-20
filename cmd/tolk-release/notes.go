package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"

	"tolk/internal/update"
)

// changelogSection returns the "## <version> …" block of a changelog.
func changelogSection(text, version string) (string, error) {
	v := regexp.QuoteMeta(update.NormalizeVersion(version))
	re := regexp.MustCompile(`(?ms)^## v?` + v + `\b.*?(?:^## |\z)`)
	m := re.FindString(text)
	if m == "" {
		return "", fmt.Errorf("更新日志里找不到 %s 的章节", version)
	}
	m = strings.TrimSuffix(m, "## ")
	return strings.TrimSpace(m), nil
}

func notes(args []string) error {
	fs := flag.NewFlagSet("notes", flag.ExitOnError)
	version := fs.String("version", "", "版本号")
	changelog := fs.String("changelog", "CHANGELOG.md", "更新日志")
	sigJSON := fs.String("sig-json", "", "Windows 包的 .sig.json（旧客户端从说明里读签名标记）")
	force := fs.Bool("force", false, "追加 update=1（强制更新）")
	out := fs.String("out", "", "输出文件")
	fs.Parse(args)
	if *version == "" || *out == "" {
		return errors.New("需要 -version -out")
	}
	raw, err := os.ReadFile(*changelog)
	if err != nil {
		return err
	}
	section, err := changelogSection(string(raw), *version)
	if err != nil {
		return err
	}
	lines := []string{"# 更新日志", "", section, ""}
	if *sigJSON != "" {
		b, err := os.ReadFile(*sigJSON)
		if err != nil {
			return err
		}
		var s struct {
			Markers   []string `json:"release_notes_markers"`
			Signature string   `json:"signature"`
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(string(b), "\xef\xbb\xbf")), &s); err != nil {
			return err
		}
		if len(s.Markers) == 0 && s.Signature != "" {
			s.Markers = []string{"DZFYQ-SIG-WINDOWS:" + s.Signature, "DZFYQ-SIG:" + s.Signature}
		}
		if len(s.Markers) == 0 {
			return errors.New("签名文件里没有签名")
		}
		lines = append(lines, s.Markers...)
	}
	if *force {
		lines = append(lines, "update=1")
	}
	return os.WriteFile(*out, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

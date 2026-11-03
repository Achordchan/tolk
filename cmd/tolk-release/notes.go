package main

import (
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
	if *force {
		lines = append(lines, "update=1")
	}
	return os.WriteFile(*out, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

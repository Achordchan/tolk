package main

import (
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"tolk/internal/update"
)

// latest writes latest.json: the release as the GitHub API would describe it
// (tag, notes, files), but as a file of the release itself. Clients read it
// through the download mirrors, which do not carry the API.
func latest(args []string) error {
	fs := flag.NewFlagSet("latest", flag.ExitOnError)
	version := fs.String("version", "", "版本号")
	dir := fs.String("dir", "dist", "这次发布的全部文件")
	notesFile := fs.String("notes", "", "发布说明")
	base := fs.String("base", "", "文件下载地址前缀（默认 https://github.com/<repo>/releases/download/v<版本>/）")
	out := fs.String("out", "", "输出文件")
	fs.Parse(args)
	if *version == "" || *out == "" {
		return errors.New("需要 -version -out")
	}
	v := update.NormalizeVersion(*version)
	if *base == "" {
		*base = update.GitHubOrigin + update.Repo + "/releases/download/v" + v + "/"
	}
	body := ""
	if *notesFile != "" {
		b, err := os.ReadFile(*notesFile)
		if err != nil {
			return err
		}
		body = string(b)
	}
	entries, err := os.ReadDir(*dir)
	if err != nil {
		return err
	}
	type asset struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
		URL  string `json:"browser_download_url"`
	}
	var assets []asset
	for _, e := range entries {
		if e.IsDir() || filepath.Base(*out) == e.Name() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			return err
		}
		assets = append(assets, asset{e.Name(), fi.Size(), *base + e.Name()})
	}
	sort.Slice(assets, func(i, j int) bool { return assets[i].Name < assets[j].Name })
	b, err := json.MarshalIndent(map[string]any{
		"tag_name": "v" + v, "name": "Tolk v" + v, "body": body, "assets": assets,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(*out, append(b, '\n'), 0o644)
}

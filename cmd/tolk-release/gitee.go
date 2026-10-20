package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const giteeAPI = "https://gitee.com/api/v5"

type giteeClient struct {
	base, token, owner, repo string
	http                     *http.Client
}

func (g *giteeClient) do(method, path string, form url.Values, body io.Reader, ctype string) (int, []byte, error) {
	u := fmt.Sprintf("%s/repos/%s/%s/%s", g.base, g.owner, g.repo, path)
	var rd io.Reader = body
	if form != nil {
		form.Set("access_token", g.token)
		if method == http.MethodGet || method == http.MethodDelete {
			u += "?" + form.Encode()
		} else {
			rd = strings.NewReader(form.Encode())
			ctype = "application/x-www-form-urlencoded"
		}
	}
	req, err := http.NewRequest(method, u, rd)
	if err != nil {
		return 0, nil, err
	}
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	resp, err := g.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b, nil
}

// gitee mirrors a release to Gitee: same tag, same notes, same files. Old
// clients (<= 1.2.10) only read Gitee, and newer ones fall back to it when
// GitHub is unreachable, so every Windows release goes to both.
func gitee(args []string) error {
	fs := flag.NewFlagSet("gitee", flag.ExitOnError)
	version := fs.String("version", "", "版本号")
	assets := fs.String("assets", "", "资源目录")
	notesFile := fs.String("notes", "", "发布说明文件")
	owner := fs.String("owner", "Achordchan", "")
	repo := fs.String("repo", "dazuofanyiguan", "")
	target := fs.String("target", "master", "新建 tag 时所在分支")
	base := fs.String("api", giteeAPI, "")
	fs.Parse(args)
	token := strings.TrimSpace(os.Getenv("GITEE_TOKEN"))
	if token == "" {
		return errors.New("缺少环境变量 GITEE_TOKEN")
	}
	if *version == "" || *assets == "" || *notesFile == "" {
		return errors.New("需要 -version -assets -notes")
	}
	v := strings.TrimLeft(*version, "vV")
	tag := "v" + v
	notes, err := os.ReadFile(*notesFile)
	if err != nil {
		return err
	}
	if !strings.Contains(string(notes), "DZFYQ-SIG") {
		return errors.New("发布说明缺少 DZFYQ-SIG 签名标记，旧客户端会拒绝更新")
	}
	files, err := collectAssets(*assets, v)
	if err != nil {
		return err
	}
	g := &giteeClient{base: *base, token: token, owner: *owner, repo: *repo, http: &http.Client{Timeout: 30 * time.Minute}}
	id, err := g.ensureRelease(tag, string(notes), *target)
	if err != nil {
		return err
	}
	existing := g.attachments(id)
	for _, f := range files {
		if old, ok := existing[strings.ToLower(filepath.Base(f))]; ok {
			if st, _, err := g.do(http.MethodDelete, fmt.Sprintf("releases/%d/attach_files/%d", id, old), url.Values{}, nil, ""); err != nil || (st != 200 && st != 204) {
				return fmt.Errorf("删除旧附件失败 %s: HTTP %d %v", f, st, err)
			}
		}
		if err := g.upload(id, f); err != nil {
			return err
		}
		fmt.Println("已上传", filepath.Base(f))
	}
	fmt.Printf("完成：https://gitee.com/%s/%s/releases/tag/%s\n", *owner, *repo, tag)
	return nil
}

func collectAssets(dir, version string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		n := strings.ToLower(e.Name())
		if !e.IsDir() && n == "appcast.xml" { // macOS update checks need it on the mirror too
			files = append(files, filepath.Join(dir, e.Name()))
			continue
		}
		if e.IsDir() || !strings.Contains(e.Name(), version) {
			continue
		}
		for _, suf := range []string{".zip", ".zip.sig.json", ".exe", ".dmg"} {
			if strings.HasSuffix(n, suf) {
				files = append(files, filepath.Join(dir, e.Name()))
				break
			}
		}
	}
	sort.Strings(files)
	pkg := "dazuofanyiguan_full.for.windows_" + version + ".zip"
	found := false
	for _, f := range files {
		if filepath.Base(f) == pkg {
			found = true
		}
	}
	if !found {
		return nil, fmt.Errorf("缺少全量包 %s", pkg)
	}
	// Refuse to publish a signature file that does not describe its package.
	for _, f := range files {
		if !strings.HasSuffix(f, ".zip.sig.json") {
			continue
		}
		pkgPath := strings.TrimSuffix(f, ".sig.json")
		raw, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		var s struct {
			Filename string `json:"filename"`
			Size     int64  `json:"size_bytes"`
			SHA256   string `json:"sha256"`
		}
		if err := json.Unmarshal(bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf")), &s); err != nil {
			return nil, err
		}
		data, err := os.ReadFile(pkgPath)
		if err != nil {
			return nil, fmt.Errorf("缺少与签名文件对应的包：%s", filepath.Base(f))
		}
		sum := sha256.Sum256(data)
		if s.Filename != filepath.Base(pkgPath) || s.Size != int64(len(data)) || !strings.EqualFold(s.SHA256, hex.EncodeToString(sum[:])) {
			return nil, fmt.Errorf("签名文件与包不一致：%s", filepath.Base(f))
		}
	}
	return files, nil
}

func (g *giteeClient) ensureRelease(tag, notes, target string) (int, error) {
	st, b, err := g.do(http.MethodGet, "releases/tags/"+tag, url.Values{}, nil, "")
	if err != nil {
		return 0, err
	}
	var rel struct {
		ID int `json:"id"`
	}
	if st == 200 {
		json.Unmarshal(b, &rel)
	}
	form := url.Values{"tag_name": {tag}, "name": {tag}, "body": {notes}, "prerelease": {"false"}}
	if rel.ID != 0 {
		st, b, err = g.do(http.MethodPatch, fmt.Sprintf("releases/%d", rel.ID), form, nil, "")
		if err != nil || (st != 200 && st != 201) {
			return 0, fmt.Errorf("更新 Gitee Release 说明失败: HTTP %d %v %s", st, err, b)
		}
		return rel.ID, nil
	}
	if st != 200 && st != 404 {
		return 0, fmt.Errorf("查询 Gitee Release 失败: HTTP %d %s", st, b)
	}
	form.Set("target_commitish", target)
	st, b, err = g.do(http.MethodPost, "releases", form, nil, "")
	if err != nil || (st != 200 && st != 201) {
		return 0, fmt.Errorf("创建 Gitee Release 失败: HTTP %d %v %s", st, err, b)
	}
	if err := json.Unmarshal(b, &rel); err != nil || rel.ID == 0 {
		return 0, errors.New("Gitee 返回的 Release 数据无效")
	}
	return rel.ID, nil
}

func (g *giteeClient) attachments(id int) map[string]int {
	st, b, err := g.do(http.MethodGet, fmt.Sprintf("releases/%d/attach_files", id), url.Values{"page": {"1"}, "per_page": {"100"}}, nil, "")
	out := map[string]int{}
	if err != nil || st != 200 {
		return out
	}
	var list []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	json.Unmarshal(b, &list)
	for _, a := range list {
		out[strings.ToLower(a.Name)] = a.ID
	}
	return out
}

func (g *giteeClient) upload(id int, path string) error {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	mw.WriteField("access_token", g.token)
	w, err := mw.CreateFormFile("file", filepath.Base(path))
	if err != nil {
		return err
	}
	if err := copyFile(w, path); err != nil {
		return err
	}
	mw.Close()
	st, b, err := g.do(http.MethodPost, fmt.Sprintf("releases/%d/attach_files", id), nil, &body, mw.FormDataContentType())
	if err != nil || (st != 200 && st != 201) {
		return fmt.Errorf("上传附件失败 %s: HTTP %d %v %s", filepath.Base(path), st, err, b)
	}
	return nil
}

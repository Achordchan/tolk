package update

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Source is one place that publishes releases. GitHub comes first; Gitee is a
// mirror for networks where GitHub is unreachable (a missing mirror repo is
// just skipped).
type Source struct {
	Name string
	API  string
}

// DefaultSources are tried in order.
var DefaultSources = []Source{
	{"GitHub", "https://api.github.com/repos/Achordchan/tolk/releases/latest"},
	{"Gitee", "https://gitee.com/api/v5/repos/Achordchan/tolk/releases/latest"},
}

// Info describes an available update.
type Info struct {
	Version string `json:"version"`
	Notes   string `json:"notes"`
	Force   bool   `json:"force"`
	Size    int64  `json:"size"`
	Source  string `json:"source"`

	name      string // asset name, used to find the same file on a mirror
	url       string
	sig       string // Windows: Ed25519 signature; macOS: Sparkle edSignature
	sha       string // Windows only
	wantSize  int64
	pkgGOOS   string
	userAgent string
}

// Checker looks for updates.
type Checker struct {
	Client    *http.Client
	Sources   []Source
	GOOS      string
	UserAgent string
	// OSVersion is the running macOS version; appcast items that need a newer
	// system are not offered. Empty skips the check.
	OSVersion string
}

// errOSTooOld means the newest release needs a newer macOS than this one.
var errOSTooOld = errors.New("新版本需要更高版本的 macOS")

// NewChecker returns a Checker for this machine.
func NewChecker(goos, version string) *Checker {
	c := &Checker{
		Client:    &http.Client{Timeout: 15 * time.Second},
		Sources:   DefaultSources,
		GOOS:      goos,
		UserAgent: "Tolk/" + version,
	}
	if goos == "darwin" {
		if out, err := exec.Command("sw_vers", "-productVersion").Output(); err == nil {
			c.OSVersion = strings.TrimSpace(string(out))
		}
	}
	return c
}

type ghAsset struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	URL  string `json:"browser_download_url"`
}

type ghRelease struct {
	Tag    string    `json:"tag_name"`
	Name   string    `json:"name"`
	Body   string    `json:"body"`
	Assets []ghAsset `json:"assets"`
}

func (c *Checker) get(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept", "application/vnd.github+json, application/json, application/xml")
	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, errors.New("响应体积异常")
	}
	return b, nil
}

func (c *Checker) release(ctx context.Context, s Source) (*ghRelease, error) {
	b, err := c.get(ctx, s.API, 4<<20)
	if err != nil {
		return nil, fmt.Errorf("%s：%w", s.Name, err)
	}
	var r ghRelease
	if err := json.Unmarshal(b, &r); err != nil || (r.Tag == "" && r.Name == "") {
		return nil, fmt.Errorf("%s 返回的 Release 数据无效", s.Name)
	}
	return &r, nil
}

var forceRe = regexp.MustCompile(`(?m)^\s*update=1\s*$`)

// WindowsAssetName is the package name the legacy updater also expects.
func WindowsAssetName(version string) string {
	return "dazuofanyiguan_full.for.windows_" + NormalizeVersion(version) + ".zip"
}

var markerRe = regexp.MustCompile(`DZFYQ-SIG-WINDOWS:([A-Za-z0-9+/=]+)`)

// Check returns nil when current is the newest version.
func (c *Checker) Check(ctx context.Context, current string) (*Info, error) {
	var errs []string
	for _, s := range c.Sources {
		r, err := c.release(ctx, s)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		var info *Info
		switch c.GOOS {
		case "windows":
			info, err = c.windowsInfo(ctx, r)
		case "darwin":
			info, err = c.macInfo(ctx, r)
		default:
			return nil, nil
		}
		if errors.Is(err, errOSTooOld) {
			return nil, nil
		}
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s：%v", s.Name, err))
			continue
		}
		info.Source = s.Name
		if CompareVersions(info.Version, current) <= 0 {
			return nil, nil
		}
		info.Notes = cleanNotes(r.Body)
		info.Force = forceRe.MatchString(r.Body)
		info.pkgGOOS = c.GOOS
		info.userAgent = c.UserAgent
		return info, nil
	}
	return nil, fmt.Errorf("检查更新失败：%s", strings.Join(errs, "；"))
}

func cleanNotes(body string) string {
	body = regexp.MustCompile(`DZFYQ-SIG(?:-[A-Za-z]+)?:[A-Za-z0-9+/=]+`).ReplaceAllString(body, "")
	body = forceRe.ReplaceAllString(body, "")
	return strings.TrimSpace(body)
}

func find(assets []ghAsset, name string) *ghAsset {
	for i := range assets {
		if strings.EqualFold(assets[i].Name, name) {
			return &assets[i]
		}
	}
	return nil
}

func (c *Checker) windowsInfo(ctx context.Context, r *ghRelease) (*Info, error) {
	v := NormalizeVersion(r.Tag)
	if v == "" {
		v = NormalizeVersion(r.Name)
	}
	name := WindowsAssetName(v)
	pkg := find(r.Assets, name)
	if pkg == nil || pkg.URL == "" {
		return nil, fmt.Errorf("最新版本没有提供 %s", name)
	}
	info := &Info{Version: v, Size: pkg.Size, name: name, url: pkg.URL, wantSize: pkg.Size}
	if sj := find(r.Assets, name+".sig.json"); sj != nil {
		b, err := c.get(ctx, sj.URL, 64<<10)
		if err != nil {
			return nil, fmt.Errorf("下载签名元数据失败：%w", err)
		}
		var s SigJSON
		if err := json.Unmarshal([]byte(strings.TrimPrefix(string(b), "\xef\xbb\xbf")), &s); err != nil {
			return nil, errors.New("签名元数据损坏")
		}
		if NormalizeVersion(s.AppVersion) != v {
			return nil, errors.New("签名元数据目标版本不匹配")
		}
		info.sig, info.sha, info.wantSize = s.Signature, strings.ToLower(s.SHA256), s.SizeBytes
		if pkg.Size != 0 && pkg.Size != s.SizeBytes {
			return nil, errors.New("签名元数据文件大小与 Release 不一致")
		}
		info.Size = s.SizeBytes
		return info, nil
	}
	if m := markerRe.FindStringSubmatch(r.Body); m != nil {
		info.sig = m[1]
		return info, nil
	}
	return nil, errors.New("最新版本缺少更新签名")
}

type appcast struct {
	Items []struct {
		Version   string `xml:"version"`
		Short     string `xml:"shortVersionString"`
		Enclosure struct {
			URL    string `xml:"url,attr"`
			Length int64  `xml:"length,attr"`
			Sig    string `xml:"edSignature,attr"`
		} `xml:"enclosure"`
		Hardware string `xml:"hardwareRequirements"`
		MinOS    string `xml:"minimumSystemVersion"`
	} `xml:"channel>item"`
}

func (c *Checker) macInfo(ctx context.Context, r *ghRelease) (*Info, error) {
	a := find(r.Assets, "appcast.xml")
	if a == nil {
		return nil, errors.New("最新版本没有 appcast.xml")
	}
	b, err := c.get(ctx, a.URL, 1<<20)
	if err != nil {
		return nil, fmt.Errorf("下载 appcast 失败：%w", err)
	}
	var ac appcast
	if err := xml.Unmarshal(b, &ac); err != nil {
		return nil, errors.New("appcast 格式无效")
	}
	best, tooOld := -1, false
	for i, it := range ac.Items {
		if it.Enclosure.URL == "" || it.Enclosure.Sig == "" {
			continue
		}
		if c.OSVersion != "" && it.MinOS != "" && CompareVersions(c.OSVersion, it.MinOS) < 0 {
			tooOld = true
			continue
		}
		if best < 0 || CompareVersions(it.Short, ac.Items[best].Short) > 0 {
			best = i
		}
	}
	if best < 0 && tooOld {
		return nil, errOSTooOld
	}
	if best < 0 {
		return nil, errors.New("appcast 里没有带签名的更新项")
	}
	it := ac.Items[best]
	v := NormalizeVersion(it.Short)
	if v == "" {
		v = NormalizeVersion(r.Tag)
	}
	name := it.Enclosure.URL[strings.LastIndex(it.Enclosure.URL, "/")+1:]
	url := it.Enclosure.URL
	// A mirror's appcast still names the primary host; prefer the archive
	// attached to this very release. The signature is checked on the bytes.
	if a := find(r.Assets, name); a != nil && a.URL != "" {
		url = a.URL
	}
	return &Info{Version: v, Size: it.Enclosure.Length, name: name, url: url, sig: it.Enclosure.Sig, wantSize: it.Enclosure.Length}, nil
}

// mirrorURLs finds the same asset on the other sources, so a blocked asset
// host does not stop an update. Verification is unchanged: a mirror cannot
// swap the content.
func (c *Checker) mirrorURLs(ctx context.Context, info *Info) []string {
	var out []string
	for _, s := range c.Sources {
		if s.Name == info.Source {
			continue
		}
		r, err := c.release(ctx, s)
		if err != nil || NormalizeVersion(r.Tag) != info.Version {
			continue
		}
		if a := find(r.Assets, info.name); a != nil && a.URL != "" {
			out = append(out, a.URL)
		}
	}
	return out
}

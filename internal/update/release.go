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

// Repo is where Tolk is published. Only GitHub: in China its file host is
// slow rather than blocked, so every github.com download also has Mirrors.
const Repo = "Achordchan/tolk"

// Where the newest release is described. latest.json is a copy of the release
// data that every release carries as a file, so it can come through Mirrors
// like any download; the API is the fallback for a release without it.
const (
	DefaultLatest = "https://github.com/" + Repo + "/releases/latest/download/latest.json"
	DefaultAPI    = "https://api.github.com/repos/" + Repo + "/releases/latest"
	GitHubOrigin  = "https://github.com/"
)

// DefaultMirrors are public GitHub download proxies, tried alongside
// github.com itself; the fastest one wins. They only pass bytes along: every
// package is still checked against the embedded keys, so a proxy cannot slip
// in anything else.
var DefaultMirrors = []string{"https://ghfast.top/", "https://gh-proxy.com/"}

// Info describes an available update.
type Info struct {
	Version string `json:"version"`
	Notes   string `json:"notes"`
	Force   bool   `json:"force"`
	Size    int64  `json:"size"`
	Source  string `json:"source"`

	name      string // asset name
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
	GOOS      string
	UserAgent string
	// OSVersion is the running macOS version; appcast items that need a newer
	// system are not offered. Empty skips the check.
	OSVersion string
	Latest    string   // latest.json URL
	API       string   // GitHub API URL of the latest release
	Origin    string   // URLs under this prefix can also come through Mirrors
	Mirrors   []string // prefixes put in front of an Origin URL
	// Logf, if set, is told which route each download took.
	Logf func(format string, args ...any)
	// verify checks a downloaded package before its route is accepted;
	// tests swap it, since Verify trusts only the embedded keys.
	verify func(info *Info, path string) error
	// windowsKey, when set, replaces the embedded key for signature
	// metadata (tests).
	windowsKey string
}

// errOSTooOld means the newest release needs a newer macOS than this one.
var errOSTooOld = errors.New("新版本需要更高版本的 macOS")

// NewChecker returns a Checker for this machine.
func NewChecker(goos, version string) *Checker {
	c := &Checker{
		Client:    &http.Client{Timeout: 15 * time.Second},
		GOOS:      goos,
		UserAgent: "Tolk/" + version,
		Latest:    DefaultLatest,
		API:       DefaultAPI,
		Origin:    GitHubOrigin,
		Mirrors:   DefaultMirrors,
		verify:    Verify,
	}
	if goos == "darwin" {
		if out, err := exec.Command("sw_vers", "-productVersion").Output(); err == nil {
			c.OSVersion = strings.TrimSpace(string(out))
		}
	}
	return c
}

func (c *Checker) logf(format string, args ...any) {
	if c.Logf != nil {
		c.Logf(format, args...)
	}
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

// retryable reports whether a failed request is worth repeating: the network
// broke or the server had a moment, rather than refusing for good.
func retryable(err error) bool {
	var h httpError
	if errors.As(err, &h) {
		return h >= 500 || h == http.StatusTooManyRequests
	}
	return !errors.Is(err, errTooLarge)
}

type httpError int

func (e httpError) Error() string { return fmt.Sprintf("HTTP %d", int(e)) }

var errTooLarge = errors.New("响应体积异常")

// get fetches url, trying up to three times: right after launch (and after an
// update restarts the app) the first connection to GitHub's file host often
// times out while the next one goes through.
func (c *Checker) get(ctx context.Context, url string, limit int64) ([]byte, error) {
	var err error
	for attempt, wait := 0, time.Second; ; attempt, wait = attempt+1, wait*2 {
		var b []byte
		if b, err = c.getOnce(ctx, url, limit); err == nil || attempt == 2 || !retryable(err) {
			return b, err
		}
		select {
		case <-ctx.Done():
			return nil, err
		case <-time.After(wait):
		}
	}
}

func (c *Checker) getOnce(ctx context.Context, url string, limit int64) ([]byte, error) {
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
		return nil, httpError(resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, errTooLarge
	}
	return b, nil
}

func parseRelease(b []byte) (*ghRelease, error) {
	var r ghRelease
	if err := json.Unmarshal(b, &r); err != nil || (r.Tag == "" && r.Name == "") {
		return nil, errors.New("Release 数据无效")
	}
	return &r, nil
}

// latest reads the newest release: latest.json over every route at once.
// GitHub's own answer wins whenever it arrives within a moment of the first;
// only without it is the newest mirror answer taken (a mirror may still hold
// an older copy). Then the API. What a mirror claims is bound later: the
// Windows package's signed metadata names its version, and a macOS bundle
// must carry the version it was offered as.
func (c *Checker) latest(ctx context.Context) (*ghRelease, string, error) {
	var errs []string
	var best, direct *ghRelease
	from := ""
	valid := func(b []byte) error { _, err := parseRelease(b); return err }
	for _, r := range c.race(ctx, c.routes(c.Latest), 1<<20, 2*time.Second, valid) {
		if r.err != nil {
			errs = append(errs, fmt.Sprintf("%s：%v", routeName(r.url, c.Origin), r.err))
			continue
		}
		rel, err := parseRelease(r.body)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s：%v", routeName(r.url, c.Origin), err))
			continue
		}
		if r.url == c.Latest {
			direct = rel
		}
		if best == nil || CompareVersions(tagVersion(rel), tagVersion(best)) > 0 {
			best, from = rel, routeName(r.url, c.Origin)
		}
	}
	if direct != nil {
		return direct, "GitHub", nil
	}
	if best != nil {
		return best, from, nil
	}
	if c.API != "" {
		b, err := c.get(ctx, c.API, 4<<20)
		if err == nil {
			rel, perr := parseRelease(b)
			if perr == nil {
				return rel, "GitHub", nil
			}
			err = perr
		}
		errs = append(errs, fmt.Sprintf("GitHub API：%v", err))
	}
	return nil, "", errors.New(strings.Join(errs, "；"))
}

func tagVersion(r *ghRelease) string {
	if v := NormalizeVersion(r.Tag); v != "" {
		return v
	}
	return NormalizeVersion(r.Name)
}

var forceRe = regexp.MustCompile(`(?m)^\s*update=1\s*$`)

// WindowsAssetName is the name of the Windows update package.
func WindowsAssetName(version string) string {
	return "Tolk-update-windows_" + NormalizeVersion(version) + ".zip"
}

// Check returns nil when current is the newest version.
func (c *Checker) Check(ctx context.Context, current string) (*Info, error) {
	r, from, err := c.latest(ctx)
	if err != nil {
		return nil, fmt.Errorf("检查更新失败：%v", err)
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
		return nil, fmt.Errorf("检查更新失败：%v", err)
	}
	info.Source = from
	if CompareVersions(info.Version, current) <= 0 {
		return nil, nil
	}
	info.Notes = cleanNotes(r.Body)
	// Nothing signs the release notes, so a forced update (restart without
	// asking) is taken only from GitHub itself; through a mirror the same
	// update just waits for the user.
	info.Force = from == "GitHub" && forceRe.MatchString(r.Body)
	info.pkgGOOS = c.GOOS
	info.userAgent = c.UserAgent
	return info, nil
}

func cleanNotes(body string) string {
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
	v := tagVersion(r)
	name := WindowsAssetName(v)
	pkg := find(r.Assets, name)
	if pkg == nil || pkg.URL == "" {
		return nil, fmt.Errorf("最新版本没有提供 %s", name)
	}
	info := &Info{Version: v, Size: pkg.Size, name: name, url: pkg.URL, wantSize: pkg.Size}
	sj := find(r.Assets, name+".sig.json")
	if sj == nil || sj.URL == "" {
		return nil, errors.New("最新版本缺少更新签名")
	}
	// parse runs on every route's answer at once, so it only reads. A route
	// counts only with metadata that fits this release and is signed, so a
	// mirror serving something stale or broken cannot win the race.
	key := c.windowsKey
	if key == "" {
		key = windowsKey
	}
	parse := func(b []byte) (SigJSON, error) {
		var x SigJSON
		if err := json.Unmarshal([]byte(strings.TrimPrefix(string(b), "\xef\xbb\xbf")), &x); err != nil || x.Signature == "" {
			return x, errors.New("签名元数据损坏")
		}
		if NormalizeVersion(x.AppVersion) != v {
			return x, errors.New("签名元数据目标版本不匹配")
		}
		if x.Filename != name || x.PackageType != "windows_full_update" || x.Platform != "windows" {
			return x, errors.New("签名元数据的文件名、包类型或平台不匹配")
		}
		if pkg.Size != 0 && pkg.Size != x.SizeBytes {
			return x, errors.New("签名元数据文件大小与 Release 不一致")
		}
		if err := VerifyPayload(key, x.Payload, x.Signature); err != nil {
			return x, fmt.Errorf("签名元数据验签失败：%w", err)
		}
		return x, nil
	}
	b, err := c.fetch(ctx, sj.URL, 64<<10, func(b []byte) error { _, err := parse(b); return err })
	if err != nil {
		return nil, fmt.Errorf("下载签名元数据失败：%w", err)
	}
	s, err := parse(b)
	if err != nil {
		return nil, err
	}
	info.sig, info.sha, info.wantSize = s.Signature, strings.ToLower(s.SHA256), s.SizeBytes
	info.Size = s.SizeBytes
	return info, nil
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
	// pick finds the newest signed item this Mac can run. It runs on every
	// route's answer at once, so it only reads. A route counts only with an
	// appcast whose item is this very release: a mirror's stale copy must
	// not beat the direct answer.
	want := tagVersion(r)
	pick := func(b []byte) (*appcast, int, error) {
		var ac appcast
		if xml.Unmarshal(b, &ac) != nil || len(ac.Items) == 0 {
			return nil, -1, errors.New("appcast 格式无效")
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
			return &ac, -1, errOSTooOld
		}
		if best < 0 {
			return nil, -1, errors.New("appcast 里没有带签名的更新项")
		}
		if v := NormalizeVersion(ac.Items[best].Short); v != "" && want != "" && v != want {
			return nil, -1, fmt.Errorf("appcast 是 %s 的，不是 %s", v, want)
		}
		return &ac, best, nil
	}
	valid := func(b []byte) error {
		if _, _, err := pick(b); err != nil && !errors.Is(err, errOSTooOld) {
			return err
		}
		return nil
	}
	b, err := c.fetchPreferDirect(ctx, a.URL, 1<<20, valid)
	if err != nil {
		return nil, fmt.Errorf("下载 appcast 失败：%w", err)
	}
	ac, best, err := pick(b)
	if err != nil {
		return nil, err
	}
	it := ac.Items[best]
	v := NormalizeVersion(it.Short)
	if v == "" {
		v = tagVersion(r)
	}
	name := it.Enclosure.URL[strings.LastIndex(it.Enclosure.URL, "/")+1:]
	url := it.Enclosure.URL
	// Prefer the archive attached to this very release. The signature is
	// checked on the bytes.
	if a := find(r.Assets, name); a != nil && a.URL != "" {
		url = a.URL
	}
	return &Info{Version: v, Size: it.Enclosure.Length, name: name, url: url, sig: it.Enclosure.Sig, wantSize: it.Enclosure.Length}, nil
}

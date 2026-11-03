// Package report builds problem reports (the 提交问题 button, and errors and
// crashes sent by themselves) and delivers them to the relay that files them
// as GitHub issues. The relay holds the GitHub token; the app never does.
package report

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"tolk/internal/logbuf"
)

// Kinds of report.
const (
	Manual = "manual" // the user pressed 提交问题
	Error  = "error"  // an error-level log line
	Crash  = "crash"  // the previous run died (Go crash output)
	Update = "update" // the update script reported a failure
)

// Field is one line of the environment table.
type Field struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Attachment is a log or file attached to the report, already scrubbed.
type Attachment struct {
	Name string `json:"name"`
	Body string `json:"body"`
}

// Report is what reaches the relay. Everything in it has gone through Scrub.
type Report struct {
	Kind        string       `json:"kind"`
	Title       string       `json:"title"`
	Description string       `json:"description"`
	Contact     string       `json:"contact,omitempty"`
	Fingerprint string       `json:"fingerprint,omitempty"`
	InstallID   string       `json:"installId"`
	Version     string       `json:"version"`
	OS          string       `json:"os"`
	Arch        string       `json:"arch"`
	Env         []Field      `json:"env"`
	Attachments []Attachment `json:"attachments"`
}

// Limits keep a report inside what a GitHub issue can hold (65 536
// characters) with room for the relay's own lines.
const (
	maxAttachment = 24000
	maxBody       = 60000
	maxTitle      = 120
)

var (
	extraSecrets = []*regexp.Regexp{
		// DeepL keys look like a UUID with ":fx"; Azure and others are long hex.
		regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}(:fx)?\b`),
		regexp.MustCompile(`\b(ghp|gho|ghu|ghs|github_pat)_[A-Za-z0-9_]{10,}`),
		regexp.MustCompile(`(?i)((?:\b|_)(?:token|secret|password|passwd|authkey|key)["']?\s*[:=]\s*["']?)[^\s"'&,}]{6,}`),
		regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`),
	}
	// Home folders give away the account name: /Users/name, C:\Users\name, /home/name.
	homeRes = []*regexp.Regexp{
		regexp.MustCompile(`(?i)([A-Z]:\\\\?Users\\\\?)[^\\/\s"'<>|]+`),
		regexp.MustCompile(`(/Users/|/home/)[^/\s"'<>]+`),
	}
)

// Scrub removes credentials, e-mail addresses and account names from text
// that leaves the machine.
func Scrub(s string) string {
	if home, err := os.UserHomeDir(); err == nil && len(home) > 3 {
		s = strings.ReplaceAll(s, home, "~")
		s = strings.ReplaceAll(s, filepath.ToSlash(home), "~")
	}
	s = logbuf.Scrub(s)
	for i, re := range extraSecrets {
		switch i {
		case 0:
			s = re.ReplaceAllString(s, "***-uuid-***")
		case 1:
			s = re.ReplaceAllString(s, "${1}_***")
		case 2:
			s = re.ReplaceAllString(s, "${1}***")
		default:
			s = re.ReplaceAllString(s, "***@***")
		}
	}
	for _, re := range homeRes {
		s = re.ReplaceAllString(s, "${1}***")
	}
	return s
}

// Clean scrubs every field and trims the report to the size limits.
func (r Report) Clean() Report {
	r.Title = truncate(oneLine(Scrub(r.Title)), maxTitle)
	r.Description = Scrub(r.Description)
	r.Contact = truncate(oneLine(r.Contact), 100)
	env := make([]Field, len(r.Env))
	for i, f := range r.Env {
		env[i] = Field{f.Name, truncate(Scrub(f.Value), 300)}
	}
	r.Env = env
	att := make([]Attachment, 0, len(r.Attachments))
	for _, a := range r.Attachments {
		if body := strings.TrimSpace(a.Body); body != "" {
			att = append(att, Attachment{a.Name, Tail(Scrub(body), maxAttachment)})
		}
	}
	r.Attachments = att
	return r
}

// Markdown is the issue body, also shown as the preview and copied when the
// relay cannot be reached.
func (r Report) Markdown() string {
	var b strings.Builder
	desc := strings.TrimSpace(r.Description)
	if desc == "" {
		desc = "_（没有填写描述）_"
	}
	b.WriteString("### 问题描述\n\n" + desc + "\n\n")
	if r.Contact != "" {
		b.WriteString("**联系方式：** " + r.Contact + "\n\n")
	}
	b.WriteString("### 环境\n\n| 项目 | 值 |\n| --- | --- |\n")
	row := func(k, v string) {
		if v != "" {
			b.WriteString("| " + k + " | " + strings.ReplaceAll(v, "|", "\\|") + " |\n")
		}
	}
	row("类型", kindName(r.Kind))
	row("版本", r.Version)
	row("系统", r.OS+" / "+r.Arch)
	for _, f := range r.Env {
		row(f.Name, f.Value)
	}
	row("安装标识", r.InstallID)
	if r.Fingerprint != "" {
		row("错误指纹", "`"+r.Fingerprint+"`")
	}
	for _, a := range r.Attachments {
		fmt.Fprintf(&b, "\n<details><summary>%s</summary>\n\n```text\n%s\n```\n\n</details>\n", a.Name, strings.ReplaceAll(a.Body, "```", "ˋˋˋ"))
	}
	return Tail(b.String(), maxBody)
}

func kindName(k string) string {
	switch k {
	case Error:
		return "自动上报：错误日志"
	case Crash:
		return "自动上报：程序崩溃"
	case Update:
		return "自动上报：更新失败"
	}
	return "用户反馈"
}

// IssueURL is a github.com "new issue" link with the report filled in, for
// when the relay cannot be reached. Browsers and GitHub refuse very long
// links, so the logs are cut to fit; the full text goes to the clipboard.
func IssueURL(repo string, r Report) string {
	const budget = 7000
	base := "https://github.com/" + repo + "/issues/new?title=" + url.QueryEscape(r.Title) + "&body="
	short := r
	short.Attachments = append([]Attachment(nil), r.Attachments...)
	for limit := maxAttachment; ; limit /= 2 {
		for i := range short.Attachments {
			short.Attachments[i].Body = Tail(r.Attachments[i].Body, limit)
		}
		body := short.Markdown()
		if limit < 400 {
			short.Attachments = nil
			body = short.Markdown() + "\n_日志太长，已复制到剪贴板，请粘贴到这里。_\n"
		}
		if u := base + url.QueryEscape(body); len(u) <= budget || short.Attachments == nil {
			return u
		}
	}
}

// Fingerprint names an error independent of the details that change from
// one occurrence to the next (numbers, paths, quoted values), so the relay
// can put repeats of the same error on one issue.
func Fingerprint(kind, text string) string {
	sum := sha1.Sum([]byte(kind + "\n" + Normalize(text)))
	return hex.EncodeToString(sum[:])[:12]
}

var normRes = []struct {
	re   *regexp.Regexp
	with string
}{
	{regexp.MustCompile(`https?://\S+`), "<url>"},
	{regexp.MustCompile(`"[^"]*"|“[^”]*”|「[^」]*」|'[^']*'`), "<q>"},
	{regexp.MustCompile(`(?:[A-Za-z]:)?[\\/][^\s:：,，)）]+`), "<path>"},
	{regexp.MustCompile(`0x[0-9a-fA-F]+`), "<hex>"},
	{regexp.MustCompile(`\d+`), "<n>"},
	{regexp.MustCompile(`\s+`), " "},
}

// Normalize strips the varying parts of an error message.
func Normalize(text string) string {
	text = strings.TrimSpace(text)
	for _, n := range normRes {
		text = n.re.ReplaceAllString(text, n.with)
	}
	return text
}

// Tail keeps the last max bytes of s, cut on a line boundary when possible.
func Tail(s string, max int) string {
	if len(s) <= max {
		return s
	}
	s = s[len(s)-max:]
	for len(s) > 0 && !utf8.RuneStart(s[0]) {
		s = s[1:]
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 && i < 200 {
		s = s[i+1:]
	}
	return "…（前面的内容已省略）\n" + s
}

func truncate(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max-1]) + "…"
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

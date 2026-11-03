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
	// KeepHead: when cut, keep the beginning instead of the end. A Go crash
	// dump starts with the panic and the crashing goroutine; logs are the
	// other way round.
	KeepHead bool `json:"keepHead,omitempty"`
}

// Cut shortens a to max bytes from the end it values.
func (a Attachment) Cut(max int) string {
	if a.KeepHead {
		return Head(a.Body, max)
	}
	return Tail(a.Body, max)
}

// Report is what reaches the relay. Everything in it has gone through Scrub.
type Report struct {
	Kind        string `json:"kind"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Contact     string `json:"contact,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
	InstallID   string `json:"installId"`
	// SubmissionID names one submission: the relay answers a repeat (another
	// endpoint after a lost reply, the 重试 button) with the first result
	// instead of filing it twice.
	SubmissionID string       `json:"submissionId"`
	Version      string       `json:"version"`
	OS           string       `json:"os"`
	Arch         string       `json:"arch"`
	Env          []Field      `json:"env"`
	Attachments  []Attachment `json:"attachments"`
}

// Limits keep a report inside what a GitHub issue can hold (65 536
// characters) with room for the relay's own lines.
const (
	maxAttachment  = 24000
	maxBody        = 60000
	maxTitle       = 120
	maxDescription = 8000
)

var (
	// secretKey names a setting whose value is a credential, up to the
	// value. The name may be quoted, also JSON-escaped (\"password\").
	secretKey    = `(?i)((?:\b|_)(?:api[_-]?key|access[_-]?key|secret[_-]?key|auth[_-]?key|token|secret|password|passwd|key)(?:\\?["'])?\s*[:=]`
	extraSecrets = []struct {
		re   *regexp.Regexp
		with string
	}{
		// DeepL keys look like a UUID with ":fx"; Azure and others are long hex.
		{regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}(:fx)?\b`), "***-uuid-***"},
		{regexp.MustCompile(`\b(ghp|gho|ghu|ghs|github_pat)_[A-Za-z0-9_]{10,}`), "${1}_***"},
		{regexp.MustCompile(`[A-Za-z0-9._%+\-]{1,64}@[A-Za-z0-9\-]{1,63}(?:\.[A-Za-z0-9\-]{1,63}){0,8}\.[A-Za-z]{2,24}`), "***@***"}, // same as the relay's
	}
	// pemBlock is a PEM block (a private key pasted in), its body over any
	// number of lines, also JSON-escaped ones, up to its END line or, cut
	// off, the end of the text. Same as the relay's.
	pemBlock = regexp.MustCompile(`-----BEGIN ([A-Z0-9 ]{1,40})-----[\s\S]*?(?:-----END [A-Z0-9 ]{1,40}-----|$)`)
	// secretAssign is a credential setting up to its = or :.
	secretAssign = regexp.MustCompile(secretKey + `)`)
	// Home folders give away the account name: /Users/name, C:\Users\name, /home/name.
	homeRes = []*regexp.Regexp{
		// The account name runs to the next separator and may hold spaces
		// ("Jane Doe"); JSON doubles the backslashes.
		regexp.MustCompile(`(?i)([A-Z]:(?:\\{1,2}|/)Users(?:\\{1,2}|/))[^\\/"'<>|\r\n]+`),
		regexp.MustCompile(`(/Users/|/home/)[^/"'<>\r\n]+`),
	}
)

// Scrub removes credentials, e-mail addresses and account names from text
// that leaves the machine.
func Scrub(s string) string {
	if home, err := os.UserHomeDir(); err == nil && len(home) > 3 {
		s = strings.ReplaceAll(s, strings.ReplaceAll(home, `\`, `\\`), "~") // JSON-escaped
		s = strings.ReplaceAll(s, home, "~")
		s = strings.ReplaceAll(s, filepath.ToSlash(home), "~")
	}
	s = logbuf.Scrub(s)
	s = pemBlock.ReplaceAllString(s, "-----BEGIN ${1}----- *** -----END ${1}-----")
	s = scrubSecrets(s)
	for _, x := range extraSecrets {
		s = x.re.ReplaceAllString(s, x.with)
	}
	for _, re := range homeRes {
		s = re.ReplaceAllString(s, "${1}***")
	}
	return s
}

// scrubSecrets blanks the whole value of every credential setting. A
// quoted value goes up to its closing quote, escaped quotes and backslashes
// included, or for a value cut off (a truncated log line) to the end of the
// line; an unquoted one up to the next whitespace, whatever characters it
// holds. A scan rather than a pattern: one pass, nothing to retry, and no
// way to stop early at an escape or a symbol. The relay's worker.js does
// the same.
func scrubSecrets(s string) string {
	var b strings.Builder
	pos := 0
	for pos < len(s) {
		m := secretAssign.FindStringSubmatchIndex(s[pos:])
		if m == nil {
			break
		}
		start := valueStart(s, pos+m[1])
		var end int
		if open := opener(s, start); open != "" {
			start += len(open)
			end = quotedEnd(s, start, open)
		} else {
			end = start
			for end < len(s) && !strings.ContainsRune(" \t\r\n", rune(s[end])) {
				end++
			}
		}
		b.WriteString(s[pos:start])
		if end > start {
			b.WriteString("***")
		}
		pos = end
	}
	b.WriteString(s[pos:])
	return b.String()
}

// valueStart skips what may stand between a setting's = or : and its value:
// spaces and line breaks, also JSON-escaped ones (\n, \r, \t) in a log
// that holds pretty-printed JSON.
func valueStart(s string, i int) int {
	for i < len(s) {
		switch {
		case strings.IndexByte(" \t\r\n", s[i]) >= 0:
			i++
		case s[i] == '\\' && i+1 < len(s) && strings.IndexByte("nrt", s[i+1]) >= 0:
			i += 2
		default:
			return i
		}
	}
	return i
}

// opener is the quote a value at i opens with: ", ', \" or \' (JSON in a
// JSON string), or "" for an unquoted value.
func opener(s string, i int) string {
	switch {
	case i < len(s) && (s[i] == '"' || s[i] == '\''):
		return s[i : i+1]
	case i+1 < len(s) && s[i] == '\\' && (s[i+1] == '"' || s[i+1] == '\''):
		return s[i : i+2]
	}
	return ""
}

// maxQuotedSpan is how far a quoted value may run over line breaks.
const maxQuotedSpan = 8192

// quotedEnd is where the value starting at i, opened by open, ends: at its
// closing quote (within maxQuotedSpan, across lines), the end of the line,
// or the end of s.
func quotedEnd(s string, i int, open string) int {
	q := open[len(open)-1]
	nl := func(c byte) bool { return c == '\r' || c == '\n' }
	if len(open) == 1 { // "…" or '…': \x is an escape
		// A value pasted over several lines (a PEM key in quotes) goes whole
		// when its closing quote is near; otherwise it was cut off, and only
		// the rest of its line goes.
		for j := i; j < len(s) && j < i+maxQuotedSpan; {
			switch s[j] {
			case '\\':
				j += 2
			case q:
				return j
			default:
				j++
			}
		}
		for i < len(s) {
			switch c := s[i]; {
			case c == '\\':
				if i+1 >= len(s) || nl(s[i+1]) {
					return i + 1
				}
				i += 2
			case c == q || nl(c):
				return i
			default:
				i++
			}
		}
		return len(s)
	}
	// \"…\" inside a JSON string. Escaped once: \" ends the value, a raw "
	// ends the outer string, \x is one character. \\ is a backslash of the
	// inner text, which escapes the character after it (\\\" is a quote).
	for i < len(s) {
		switch c := s[i]; {
		case c == '\\' && i+1 < len(s) && s[i+1] == q:
			return i
		case c == '\\' && i+1 < len(s) && s[i+1] == '\\':
			i += 2
			if i < len(s) && s[i] == '\\' && i+1 < len(s) && !nl(s[i+1]) {
				i += 2
			} else if i < len(s) && s[i] != '"' && s[i] != '\\' && !nl(s[i]) {
				i++
			}
		case c == '\\':
			if i+1 >= len(s) || nl(s[i+1]) {
				return i + 1
			}
			i += 2
		case c == '"' || nl(c):
			return i
		default:
			i++
		}
	}
	return len(s)
}

// Clean scrubs every field and trims the report to the size limits.
func (r Report) Clean() Report {
	r.Title = truncate(oneLine(Scrub(r.Title)), maxTitle)
	r.Description = truncate(Scrub(r.Description), maxDescription)
	r.Contact = truncate(oneLine(r.Contact), 100)
	env := make([]Field, len(r.Env))
	for i, f := range r.Env {
		env[i] = Field{f.Name, truncate(Scrub(f.Value), 300)}
	}
	r.Env = env
	att := make([]Attachment, 0, len(r.Attachments))
	for _, a := range r.Attachments {
		if body := strings.TrimSpace(a.Body); body != "" {
			a.Body = Scrub(body)
			a.Body = a.Cut(maxAttachment)
			att = append(att, a)
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
	// The description and environment always stay; when everything does not
	// fit, each attachment gives up its oldest lines to an equal share.
	const wrap = 80 // <details> markup around each attachment
	room := maxBody - b.Len()
	total := 0
	for _, a := range r.Attachments {
		total += len(a.Body) + wrap + len(a.Name)
	}
	share := 0
	if total > room && len(r.Attachments) > 0 {
		share = room/len(r.Attachments) - wrap - 40
	}
	for _, a := range r.Attachments {
		body := a.Body
		if share > 0 {
			body = a.Cut(share)
		} else if share < 0 || (total > room && share == 0) {
			continue
		}
		fmt.Fprintf(&b, "\n<details><summary>%s</summary>\n\n```text\n%s\n```\n\n</details>\n", a.Name, strings.ReplaceAll(body, "```", "ˋˋˋ"))
	}
	return b.String()
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
	fits := func(body string) (string, bool) {
		u := base + url.QueryEscape(body)
		return u, len(u) <= budget
	}
	short := r
	short.Attachments = append([]Attachment(nil), r.Attachments...)
	for limit := maxAttachment; limit >= 400; limit /= 2 {
		for i := range short.Attachments {
			short.Attachments[i].Body = r.Attachments[i].Cut(limit)
		}
		if u, ok := fits(short.Markdown()); ok {
			return u
		}
	}
	// No room for logs; then shorten the description too. The full report
	// is on the clipboard either way.
	short.Attachments = nil
	const note = "\n_完整内容（含日志）已复制到剪贴板，请粘贴到这里。_\n"
	desc := []rune(r.Description)
	for n := len(desc); ; n = n * 2 / 3 {
		short.Description = string(desc[:n])
		if n < len(desc) {
			short.Description += "…"
		}
		if u, ok := fits(short.Markdown() + note); ok || n == 0 {
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

// Head keeps the first max bytes of s, cut on a line boundary when possible.
func Head(s string, max int) string {
	if len(s) <= max {
		return s
	}
	s = s[:max]
	for len(s) > 0 { // drop a rune cut in half
		if r, size := utf8.DecodeLastRuneInString(s); r != utf8.RuneError || size > 1 {
			break
		}
		s = s[:len(s)-1]
	}
	if i := strings.LastIndexByte(s, '\n'); i >= 0 && len(s)-i < 200 {
		s = s[:i]
	}
	return s + "\n…（后面的内容已省略）"
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

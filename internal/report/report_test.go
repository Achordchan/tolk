package report

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestScrub(t *testing.T) {
	in := strings.Join([]string{
		`deepl key 1a2b3c4d-1111-2222-3333-444455556666:fx`,
		`token=ghp_abcdefghijklmnop1234`,
		`"apiKey": "sk-abcdefghijklmnopqrst"`,
		`password: hunter2hunter2`,
		`mail me at someone@example.com`,
		`C:\Users\张三\AppData\Local\Tolk\Tolk.exe`,
		`/Users/achord/Library/Logs`,
		`Authorization: Bearer abcdefghijklmnop`,
	}, "\n")
	out := Scrub(in)
	for _, leak := range []string{"444455556666", "abcdefghijklmnop1234", "sk-abcdefghijklmnopqrst", "hunter2", "someone@", "张三", "achord", "Bearer abcdefghijklmnop"} {
		if strings.Contains(out, leak) {
			t.Errorf("leaked %q in:\n%s", leak, out)
		}
	}
	// Ordinary words must survive.
	if got := Scrub("shortcutKeyCode: 7, 主题 pink"); got != "shortcutKeyCode: 7, 主题 pink" {
		t.Errorf("over-scrubbed: %q", got)
	}
}

func TestFingerprintIgnoresDetails(t *testing.T) {
	a := Fingerprint(Error, `更新失败：open C:\Users\a\x.zip: Access is denied (pid 1234)`)
	b := Fingerprint(Error, `更新失败：open C:\Users\b\y.zip: Access is denied (pid 99)`)
	if a != b {
		t.Errorf("same error, different fingerprints %s %s", a, b)
	}
	if a == Fingerprint(Error, "启动原生助手失败：exec format error") {
		t.Error("different errors share a fingerprint")
	}
}

func TestIssueURLFits(t *testing.T) {
	r := Report{Kind: Manual, Title: "[反馈] 很长", Description: "desc", Version: "2.2.3", OS: "windows", Arch: "amd64",
		Attachments: []Attachment{{"运行日志", strings.Repeat("一行很长的中文日志 line\n", 3000)}}}
	r = r.Clean()
	u := IssueURL(Repo, r)
	if len(u) > 7000 {
		t.Fatalf("url too long: %d", len(u))
	}
	parsed, err := url.Parse(u)
	if err != nil || !strings.Contains(parsed.Query().Get("body"), "desc") {
		t.Fatalf("bad url %v", err)
	}
}

func TestMarkdownLimits(t *testing.T) {
	r := Report{Kind: Error, Title: strings.Repeat("长", 500), Attachments: []Attachment{
		{"a", strings.Repeat("x\n", 50000)}, {"b", strings.Repeat("y\n", 50000)}, {"c", strings.Repeat("z\n", 50000)},
	}}.Clean()
	if n := len([]rune(r.Title)); n > maxTitle {
		t.Errorf("title %d runes", n)
	}
	if n := len(r.Markdown()); n > maxBody+100 {
		t.Errorf("body %d bytes", n)
	}
}

func TestReserveLimits(t *testing.T) {
	s := NewStore(t.TempDir())
	now := time.Date(2026, 11, 3, 10, 0, 0, 0, time.Local)
	if !s.Reserve("a", now) || s.Reserve("a", now.Add(time.Hour)) {
		t.Fatal("same fingerprint must wait a day")
	}
	if !s.Reserve("a", now.Add(25*time.Hour)) {
		t.Fatal("same fingerprint allowed again after a day")
	}
	for i := 0; i < MaxAutoPerDay-1; i++ {
		if !s.Reserve(string(rune('b'+i)), now.Add(25*time.Hour)) {
			t.Fatalf("report %d refused", i)
		}
	}
	if s.Reserve("zz", now.Add(25*time.Hour)) {
		t.Fatal("daily cap not applied")
	}
	if _, err := s.Update(func(st *State) { st.AutoDisabled = true }); err != nil {
		t.Fatal(err)
	}
	if s.Reserve("new", now.Add(72*time.Hour)) {
		t.Fatal("switch off must stop auto reports")
	}
	if s.Get().InstallID == "" {
		t.Fatal("no install id")
	}
}

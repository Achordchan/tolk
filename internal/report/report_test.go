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
		`password: "my secret passphrase"`,
		`{"apiKey":"ab cd ef"}`,
		`token='x y z'`,
		`passwd=abc`,
		`{"password":"first \"secret remainder\""}`,
		`msg="{\"password\":\"secret-value\",\"user\":\"bob\"}"`,
		`{\"token\":\"a \\\"quoted\\\" tail\"}`,
	}, "\n")
	out := Scrub(in)
	for _, leak := range []string{"444455556666", "abcdefghijklmnop1234", "sk-abcdefghijklmnopqrst", "hunter2", "someone@", "张三", "achord", "Bearer abcdefghijklmnop", "my secret", "passphrase", "ab cd ef", "x y z", "=abc", "remainder", "secret-value", "quoted", "tail"} {
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
		Attachments: []Attachment{{Name: "运行日志", Body: strings.Repeat("一行很长的中文日志 line\n", 3000)}}}
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
		{Name: "a", Body: strings.Repeat("x\n", 50000)}, {Name: "b", Body: strings.Repeat("y\n", 50000)}, {Name: "c", Body: strings.Repeat("z\n", 50000)},
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
	if !s.Reserve("a", now) {
		t.Fatal("first report refused")
	}
	s.Finished("a", now)
	if s.Reserve("a", now.Add(time.Hour)) {
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
	if s.TryReserve("zz", now.Add(26*time.Hour)) != Later {
		t.Fatal("daily cap should mean try later")
	}
	if _, err := s.Update(func(st *State) { st.AutoDisabled = true }); err != nil {
		t.Fatal(err)
	}
	if s.TryReserve("new", now.Add(72*time.Hour)) != Declined {
		t.Fatal("switch off must stop auto reports")
	}
	if s.Get().InstallID == "" {
		t.Fatal("no install id")
	}
}

func TestMarkdownKeepsHeaderWhenLogsAreHuge(t *testing.T) {
	r := Report{Kind: Crash, Description: "上一次运行异常退出", Version: "2.2.3", Env: []Field{{"系统版本", "Windows 11"}}, Attachments: []Attachment{
		{Name: "崩溃信息", Body: strings.Repeat("c\n", 30000)}, {Name: "运行日志", Body: strings.Repeat("l\n", 30000)}, {Name: "更新日志", Body: strings.Repeat("u\n", 30000)},
	}}.Clean()
	md := r.Markdown()
	if len(md) > maxBody {
		t.Errorf("body %d bytes", len(md))
	}
	for _, want := range []string{"### 问题描述", "上一次运行异常退出", "Windows 11", "崩溃信息", "运行日志", "更新日志"} {
		if !strings.Contains(md, want) {
			t.Errorf("lost %q", want)
		}
	}
}

func TestIssueURLLongDescription(t *testing.T) {
	r := Report{Kind: Manual, Title: "t", Description: strings.Repeat("很长的描述", 600)}.Clean()
	if u := IssueURL(Repo, r); len(u) > 7000 {
		t.Fatalf("url %d bytes", len(u))
	}
}

func TestReserveRefusedWhenStateCannotBeSaved(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir + "/missing/\x00bad")
	if s.TryReserve("a", time.Now()) != Later {
		t.Fatal("an unsaved reservation must be refused")
	}
}

func TestCrashAttachmentKeepsItsBeginning(t *testing.T) {
	dump := "panic: boom\n\ngoroutine 1 [running]:\nmain.crash()\n" + strings.Repeat("goroutine 9 [select]:\nother()\n", 4000)
	r := Report{Kind: Crash, Attachments: []Attachment{{Name: "崩溃信息", Body: dump, KeepHead: true}, {Name: "运行日志", Body: strings.Repeat("log line\n", 9000)}}}.Clean()
	if !strings.HasPrefix(r.Attachments[0].Body, "panic: boom") {
		t.Fatal("Clean dropped the panic")
	}
	md := r.Markdown()
	if !strings.Contains(md, "main.crash()") {
		t.Fatal("Markdown dropped the crashing goroutine")
	}
	if u := IssueURL(Repo, r); !strings.Contains(u, "panic") {
		t.Fatal("issue link dropped the panic")
	}
	if got := Head("一二三四五", 7); strings.ContainsRune(got, '�') {
		t.Fatalf("Head split a rune: %q", got)
	}
}

func TestScrubHomeWithSpacesAndJSONEscapes(t *testing.T) {
	for _, in := range []string{
		`{"log_path":"C:\\Users\\Jane Doe\\.dzfyq\\update_state\\update.log"}`,
		`C:\Users\Jane Doe\AppData\Local\Tolk`,
		`/Users/Jane Doe/Library/Logs`,
	} {
		out := Scrub(in)
		if strings.Contains(out, "Jane") || strings.Contains(out, "Doe") {
			t.Errorf("account name left in %q", out)
		}
		if !strings.Contains(out, "***") {
			t.Errorf("nothing redacted in %q", out)
		}
	}
}

func TestScrubDoubleEscapedValues(t *testing.T) {
	for in, want := range map[string]string{
		`{"cfg":"{\"password\": \"a b\\\"c\\\\\", \"x\": 1}"}`: `{"cfg":"{\"password\": \"***\", \"x\": 1}"}`,
		`{"cfg":"{\"token\": \"line\\nnext\", \"x\": 1}"}`:     `{"cfg":"{\"token\": \"***\", \"x\": 1}"}`,
		`{"cfg":"{\"token\": \"abc`:                            `{"cfg":"{\"token\": \"***`,
		`password: \"` + strings.Repeat(`\`, 5000):             `password: \"***`,
		// Cut off after an escape: the rest of the line is the value.
		`password: "prefix\"secret-tail`:                 `password: "***`,
		`password: "prefix\\secret-tail` + "\nnext line": `password: "***` + "\nnext line",
		`{"l":"{\"token\": \"pre\\\"secret-tail`:         `{"l":"{\"token\": \"***`,
		`{"l":"{\"token\": \"pre\\\\secret-tail"}`:       `{"l":"{\"token\": \"***"}`,
		`secret='a\'b' rest`:                             `secret='***' rest`,
		// Only the value goes; what follows stays.
		`password: "x" user: "y"`: `password: "***" user: "y"`,
		`password: "" ok`:         `password: "" ok`,
		// Unquoted values go whole up to whitespace, whatever they hold.
		`PASSWORD=&A7secret! next`:   `PASSWORD=*** next`,
		`token=ab,cd&ef}\gh` + "\nx": `token=***` + "\nx",
		`api_key: ,leading`:          `api_key: ***`,
		// The value may start on the next line; over-blanking beats a leak.
		`password:` + "\n" + `next`:                        `password:` + "\n" + `***`,
		`"password":` + "\n  " + `"private value", "x": 1`: `"password":` + "\n  " + `"***", "x": 1`,
		`PASSWORD =` + "\r\n\t" + `&secret rest`:           `PASSWORD =` + "\r\n\t" + `*** rest`,
		`{"l":"{\n  \"token\":\n    \"abc def\"\n}"}`:      `{"l":"{\n  \"token\":\n    \"***\"\n}"}`,
	} {
		if got := Scrub(in); got != want {
			t.Errorf("Scrub(%.60q) = %.60q, want %.60q", in, got, want)
		}
	}
}

func TestReserveRetriesUploadCutShortByQuitting(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 3, 10, 0, 0, 0, time.Local)
	run1 := NewStore(dir)
	if !run1.Reserve("crash", now) {
		t.Fatal("first report refused")
	}
	if run1.Reserve("crash", now.Add(time.Minute)) {
		t.Fatal("same report let through while its upload runs")
	}
	// The app quits before the upload finishes; the next run tries again.
	first := run1.Submission("crash")
	run2 := NewStore(dir)
	if !run2.Reserve("crash", now.Add(time.Hour)) {
		t.Fatal("interrupted upload not retried")
	}
	// Same submission, so the relay can find what the cut-off attempt wrote.
	if id := run2.Submission("crash"); id == "" || id != first {
		t.Fatalf("submission %q, interrupted one was %q", id, first)
	}
	run2.Finished("crash", now.Add(time.Hour))
	run3 := NewStore(dir)
	if run3.Reserve("crash", now.Add(2*time.Hour)) {
		t.Fatal("finished upload sent again within a day")
	}
	if !run3.Reserve("crash", now.Add(26*time.Hour)) || run3.Submission("crash") == first {
		t.Fatal("a new report of the same error needs a new submission")
	}
}

func TestReleaseKeepsSubmissionForRetry(t *testing.T) {
	s := NewStore(t.TempDir())
	now := time.Date(2026, 11, 3, 10, 0, 0, 0, time.Local)
	if !s.Reserve("e", now) {
		t.Fatal("first report refused")
	}
	id := s.Submission("e")
	s.Release("e")
	if !s.Reserve("e", now.Add(time.Minute)) || s.Submission("e") != id {
		t.Fatal("a released upload should go again with the same submission")
	}
}

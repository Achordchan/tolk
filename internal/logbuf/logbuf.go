// Package logbuf keeps the app's recent log lines for the console view.
package logbuf

import (
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Entry is one log line.
type Entry struct {
	ID    int64  `json:"id"`
	Time  int64  `json:"time"` // unix milliseconds
	Level string `json:"level"`
	Text  string `json:"text"`
}

// Buffer is a bounded, goroutine-safe ring of entries.
type Buffer struct {
	mu      sync.Mutex
	max     int
	next    int64
	entries []Entry
	tee     io.Writer
}

// New returns a Buffer holding up to max lines. tee, when set, also receives
// every line (stderr, in the app).
func New(max int, tee io.Writer) *Buffer { return &Buffer{max: max, next: 1, tee: tee} }

var secretRes = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9._\-]{8,}`),
	regexp.MustCompile(`(?i)(api[_-]?key["':= ]+)[A-Za-z0-9._\-]{8,}`),
	regexp.MustCompile(`\bsk-[A-Za-z0-9_\-]{8,}`),
	regexp.MustCompile(`(?i)(access_token=)[^&\s]+`),
	regexp.MustCompile(`(?i)(Ocp-Apim-Subscription-Key["':= ]+)[A-Za-z0-9]{8,}`),
}

// Scrub removes anything that looks like a credential.
func Scrub(s string) string {
	for i, re := range secretRes {
		if i == 2 {
			s = re.ReplaceAllString(s, "sk-***")
			continue
		}
		s = re.ReplaceAllString(s, "${1}***")
	}
	return s
}

// Add records a line.
func (b *Buffer) Add(level, text string) {
	text = Scrub(strings.TrimRight(text, "\r\n"))
	if text == "" {
		return
	}
	b.mu.Lock()
	e := Entry{ID: b.next, Time: time.Now().UnixMilli(), Level: level, Text: text}
	b.next++
	b.entries = append(b.entries, e)
	if len(b.entries) > b.max {
		b.entries = append([]Entry(nil), b.entries[len(b.entries)-b.max:]...)
	}
	tee := b.tee
	b.mu.Unlock()
	if tee != nil {
		fmt.Fprintf(tee, "%s [%s] %s\n", time.UnixMilli(e.Time).Format("15:04:05"), level, text)
	}
}

func (b *Buffer) Infof(format string, a ...any)  { b.Add("info", fmt.Sprintf(format, a...)) }
func (b *Buffer) Warnf(format string, a ...any)  { b.Add("warn", fmt.Sprintf(format, a...)) }
func (b *Buffer) Errorf(format string, a ...any) { b.Add("error", fmt.Sprintf(format, a...)) }

// Since returns entries with an id above after.
func (b *Buffer) Since(after int64) []Entry {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []Entry
	for _, e := range b.entries {
		if e.ID > after {
			out = append(out, e)
		}
	}
	return out
}

// Clear drops everything.
func (b *Buffer) Clear() {
	b.mu.Lock()
	b.entries = nil
	b.mu.Unlock()
}

// Writer adapts the buffer to the standard log package: lines from log.Println
// become entries, classified by a leading "warn:"/"error:" word or common
// failure words.
func (b *Buffer) Writer() io.Writer { return writer{b} }

type writer struct{ b *Buffer }

func (w writer) Write(p []byte) (int, error) {
	for _, line := range strings.Split(strings.TrimRight(string(p), "\n"), "\n") {
		level := "info"
		l := strings.ToLower(line)
		switch {
		case strings.Contains(l, "error") || strings.Contains(line, "失败") || strings.Contains(l, "panic"):
			level = "error"
		case strings.Contains(l, "warn") || strings.Contains(line, "警告"):
			level = "warn"
		}
		w.b.Add(level, line)
	}
	return len(p), nil
}

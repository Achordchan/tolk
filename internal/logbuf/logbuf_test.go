package logbuf

import (
	"log"
	"strings"
	"testing"
)

func TestScrubAndRing(t *testing.T) {
	b := New(3, nil)
	b.Infof("Authorization: Bearer abcdefghijklmnop")
	b.Infof("key sk-abcdefghijklmnop1234 used")
	b.Infof("api_key=supersecretvalue12")
	b.Warnf("four")
	got := b.Since(0)
	if len(got) != 3 {
		t.Fatalf("ring kept %d", len(got))
	}
	for _, e := range got {
		if strings.Contains(e.Text, "abcdefghijklmnop") || strings.Contains(e.Text, "supersecret") {
			t.Errorf("secret leaked: %s", e.Text)
		}
	}
	if len(b.Since(got[1].ID)) != 1 {
		t.Error("Since must return only newer entries")
	}
}

func TestStdLogWriterClassifies(t *testing.T) {
	b := New(10, nil)
	l := log.New(b.Writer(), "", 0)
	l.Println("shortcut: 注册失败")
	l.Println("hello")
	e := b.Since(0)
	if e[0].Level != "error" || e[1].Level != "info" {
		t.Fatalf("%+v", e)
	}
}

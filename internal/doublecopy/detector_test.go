package doublecopy

import (
	"testing"
	"time"
)

func feed(d *Detector, seq uint64, at time.Duration, text string) (string, bool) {
	return d.Observe(seq, time.Unix(0, 0).Add(at), func() string { return text })
}

func TestSameTextTwiceInsideWindowFires(t *testing.T) {
	d := &Detector{Window: 550 * time.Millisecond}
	feed(d, 1, 0, "start")
	if _, ok := feed(d, 2, 100*time.Millisecond, "hello"); ok {
		t.Fatal("first copy must not fire")
	}
	got, ok := feed(d, 3, 400*time.Millisecond, "hello ")
	if !ok || got != "hello" {
		t.Fatalf("got %q %v", got, ok)
	}
}

func TestSlowOrDifferentCopiesDoNotFire(t *testing.T) {
	d := &Detector{Window: 550 * time.Millisecond}
	feed(d, 1, 0, "")
	feed(d, 2, 100*time.Millisecond, "a")
	if _, ok := feed(d, 3, 900*time.Millisecond, "a"); ok {
		t.Fatal("too slow")
	}
	if _, ok := feed(d, 4, 1000*time.Millisecond, "b"); ok {
		t.Fatal("different text")
	}
}

func TestEmptyClipboardNeverFires(t *testing.T) {
	d := &Detector{Window: time.Second}
	feed(d, 1, 0, "")
	feed(d, 2, 10*time.Millisecond, "  ")
	if _, ok := feed(d, 3, 20*time.Millisecond, "  "); ok {
		t.Fatal("blank text fired")
	}
}

func TestTripleCopySuppressesRepeat(t *testing.T) {
	d := &Detector{Window: 550 * time.Millisecond}
	feed(d, 1, 0, "")
	feed(d, 2, 100*time.Millisecond, "x")
	if _, ok := feed(d, 3, 300*time.Millisecond, "x"); !ok {
		t.Fatal("second copy should fire")
	}
	if _, ok := feed(d, 4, 500*time.Millisecond, "x"); ok {
		t.Fatal("third copy inside the window must be suppressed")
	}
}

func TestNoChangeNoRead(t *testing.T) {
	d := &Detector{Window: time.Second}
	reads := 0
	read := func() string { reads++; return "x" }
	d.Observe(5, time.Now(), read)
	d.Observe(5, time.Now(), read)
	d.Observe(5, time.Now(), read)
	if reads != 1 {
		t.Fatalf("reads %d", reads)
	}
}

package doublekey

import (
	"testing"
	"time"
)

func TestDoublePress(t *testing.T) {
	var d Detector
	n := time.Now()
	if d.Observe(true, true, true, n) {
		t.Fatal("first")
	}
	if d.Observe(true, true, true, n.Add(20*time.Millisecond)) {
		t.Fatal("repeat")
	}
	d.Observe(true, false, true, n.Add(30*time.Millisecond))
	if !d.Observe(true, true, true, n.Add(100*time.Millisecond)) {
		t.Fatal("second")
	}
}
func TestExpiredAndInterrupted(t *testing.T) {
	for _, interrupt := range []bool{false, true} {
		var d Detector
		n := time.Now()
		d.Observe(true, true, true, n)
		d.Observe(true, false, true, n)
		delay := time.Second
		if interrupt {
			d.Observe(false, true, true, n)
			delay = time.Millisecond
		}
		if d.Observe(true, true, true, n.Add(delay)) {
			t.Fatal("unexpected trigger")
		}
	}
}

func TestWindow(t *testing.T) {
	d := Detector{Window: 900 * time.Millisecond}
	n := time.Now()
	d.Observe(true, true, true, n)
	d.Observe(true, false, true, n)
	if !d.Observe(true, true, true, n.Add(800*time.Millisecond)) {
		t.Fatal("inside a longer window")
	}
	d = Detector{Window: 300 * time.Millisecond}
	d.Observe(true, true, true, n)
	d.Observe(true, false, true, n)
	if d.Observe(true, true, true, n.Add(400*time.Millisecond)) {
		t.Fatal("outside a shorter window")
	}
}

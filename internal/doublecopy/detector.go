// Package doublecopy detects "copy the same text twice quickly", the
// Cmd+C+C / Ctrl+C+C gesture. It is a port of bagayalu's
// ClipboardDoubleCopyMonitor and ClipboardDoubleCopyPolicy.
package doublecopy

import (
	"strings"
	"time"
)

// Detector is fed the clipboard change counter on every poll.
type Detector struct {
	Window time.Duration

	started     bool
	lastSeq     uint64
	haveChange  bool
	lastChange  time.Time
	latest      string
	lastEmitted string
	lastEmitAt  time.Time
}

// Observe reports whether this poll completed a double copy. read is only
// called when the counter changed, so idle polls cost nothing.
func (d *Detector) Observe(seq uint64, now time.Time, read func() string) (string, bool) {
	if !d.started {
		d.started, d.lastSeq = true, seq
		d.latest = read()
		return "", false
	}
	if seq == d.lastSeq {
		return "", false
	}
	d.lastSeq = seq
	cur := read()
	prev, hadChange, lastChange := d.latest, d.haveChange, d.lastChange
	d.latest, d.haveChange, d.lastChange = cur, true, now

	if !hadChange {
		return "", false
	}
	text := strings.TrimSpace(cur)
	if now.Sub(lastChange) > d.Window || text == "" || strings.TrimSpace(prev) != text {
		return "", false
	}
	if d.lastEmitted == text && now.Sub(d.lastEmitAt) <= d.Window {
		return "", false
	}
	d.lastEmitted, d.lastEmitAt = text, now
	return text, true
}

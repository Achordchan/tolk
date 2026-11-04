// Package doublekey recognizes two distinct key presses, excluding key repeat.
package doublekey

import "time"

type Detector struct {
	Last time.Time
	Down bool
}

func (d *Detector) Observe(x, down, modifiers bool, now time.Time) bool {
	if !x {
		if down {
			d.Last = time.Time{}
		}
		return false
	}
	if !down {
		d.Down = false
		return false
	}
	if d.Down {
		return false
	}
	d.Down = true
	if !modifiers {
		d.Last = time.Time{}
		return false
	}
	if !d.Last.IsZero() && now.Sub(d.Last) <= 550*time.Millisecond {
		d.Last = time.Time{}
		return true
	}
	d.Last = now
	return false
}

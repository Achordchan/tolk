//go:build !windows && !darwin

package doublecopy

// Sequence is not available here; double copy stays off.
func Sequence() (uint64, bool) { return 0, false }

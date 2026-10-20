//go:build windows

package doublecopy

import "syscall"

var getSeq = syscall.NewLazyDLL("user32.dll").NewProc("GetClipboardSequenceNumber")

// Sequence is the clipboard change counter; it grows on every copy, even of
// the same text.
func Sequence() (uint64, bool) {
	r, _, _ := getSeq.Call()
	return uint64(r), true
}

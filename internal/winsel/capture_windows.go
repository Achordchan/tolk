//go:build windows

package winsel

import (
	"errors"
	"strings"
	"time"

	"tolk/internal/inputassist"
)

// MaxChars is the longest selection that is translated.
const MaxChars = 5000

var (
	// ErrElevated: the app runs as administrator and Tolk does not, so
	// Windows blocks both reading and typing into it.
	ErrElevated = errors.New("该应用以管理员身份运行，Tolk 无法读取它里面的选中文字；请以管理员身份运行 Tolk")
	// ErrTooLong: the selection is longer than MaxChars.
	ErrTooLong = errors.New("选中的文字太长")
	// ErrUnreadable: the control does not tell its selection and copying to
	// find out is not allowed here (automatic mode must never press keys).
	ErrUnreadable = errors.New("当前应用没有提供选中文本")
	// ErrFocusChanged: the user moved on between reading and replacing.
	ErrFocusChanged = errors.New("焦点已经变化")
	// ErrTextChanged: the text at the selection is no longer what was read.
	ErrTextChanged = errors.New("原文已经变化")
	// ErrClipboardBusy: another program was changing the clipboard.
	ErrClipboardBusy = errors.New("剪贴板正被其它应用使用")
	// ErrUnverified: the selection could not be confirmed, so nothing is
	// typed over it.
	ErrUnverified = errors.New("无法确认原选区")
)

// Selection is the text the user selected in another app, frozen at the time
// it was read, with what is needed to check it before it is replaced.
type Selection struct {
	Text   string
	Rect   inputassist.Rect // where it is on screen (or the caret, or the mouse)
	Window Window
	Focus  uintptr // the focused control's handle, if it has one
	PID    int
	// Via is how it was read: "uia" (UI Automation, no side effects) or
	// "copy" (Ctrl+C on the user's behalf, the way for apps that expose
	// nothing, such as WeChat).
	Via string
	// Unverified: it came from a copy that may not have been a selection
	// (some editors copy the whole line when nothing is selected), so it may be
	// translated but never replaced.
	Unverified bool
}

// Capture reads the selection of the app in front. UI Automation comes
// first; when the control exposes no text, and allowCopy says pressing Ctrl+C
// on the user's behalf is fine, that is how it is read (and the clipboard is
// put back as it was).
func Capture(fg Foreground, allowCopy bool) (Selection, error) {
	if higherIntegrity(fg.App.PID) {
		return Selection{}, ErrElevated
	}
	sel := Selection{Window: fg.Window, PID: fg.App.PID, Focus: FocusWindow(fg.Window)}

	res, err := readUIA(fg.App.PID, 1500*time.Millisecond)
	if errors.Is(err, ErrPassword) {
		return sel, err
	}
	if err == nil && res.supported {
		text := strings.TrimSpace(res.text)
		if text == "" {
			return sel, ErrNoSelection
		}
		if res.truncated || len([]rune(text)) > MaxChars {
			return sel, ErrTooLong
		}
		sel.Text, sel.Via = res.text, "uia"
		sel.Rect = res.rect
		if !res.haveRect {
			sel.Rect = anchorFallback(fg.Window)
		}
		return sel, nil
	}
	if !allowCopy {
		return sel, ErrUnreadable
	}
	return copyCapture(fg, sel)
}

// anchorFallback is the caret of a classic control, or else the mouse.
func anchorFallback(w Window) inputassist.Rect {
	if r, ok := CaretRect(w); ok {
		return r
	}
	return CursorRect()
}

// stillIn reports whether the app in front is still the one that was approved.
func stillIn(fg Foreground) bool {
	cur, ok := GetForeground()
	return ok && cur.Window == fg.Window && cur.App.PID == fg.App.PID
}

func copyCapture(fg Foreground, sel Selection) (Selection, error) {
	clipMu.Lock()
	defer clipMu.Unlock()
	defer txns.Add(1)
	snap, err := takeSnapshot()
	if err != nil {
		return sel, err
	}
	before := ClipboardSeq()
	releaseModifiers()
	if !stillIn(fg) {
		snap.free()
		return sel, ErrFocusChanged
	}
	ctrlChord(vkC)
	deadline := time.Now().Add(700 * time.Millisecond)
	for ClipboardSeq() == before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	after := ClipboardSeq()
	if after == before {
		snap.free()
		return sel, ErrNoSelection // nothing was copied: nothing is selected
	}
	text, ok := clipboardText()
	// Put the user's clipboard back, unless someone else wrote to it since.
	_ = snap.restore(after)
	if !ok || strings.TrimSpace(text) == "" {
		return sel, ErrNoSelection
	}
	if len([]rune(text)) > MaxChars {
		return sel, ErrTooLong
	}
	sel.Text, sel.Via = text, "copy"
	// Editors that copy the current line when nothing is selected (Sublime
	// Text, VS Code) hand back a whole line, ending in its line break; a
	// selection made with the mouse or the keyboard hardly ever does.
	sel.Unverified = strings.HasSuffix(text, "\n") || strings.HasSuffix(text, "\r")
	sel.Rect = anchorFallback(fg.Window)
	return sel, nil
}

// sameText compares a selection with what UI Automation reads now, ignoring
// the difference between \r\n and \n that apps disagree about.
func sameText(a, b string) bool {
	norm := func(s string) string { return strings.ReplaceAll(s, "\r\n", "\n") }
	return norm(a) == norm(b)
}

// confirm is the last look before typing: the app in front, the control that
// has the focus and, when the selection was read through UI Automation, the
// selection itself must all be as they were. Anything that cannot be read
// counts against replacing.
func confirm(sel Selection) error {
	cur, ok := GetForeground()
	if !ok || cur.Window != sel.Window || cur.App.PID != sel.PID || FocusWindow(sel.Window) != sel.Focus {
		return ErrFocusChanged
	}
	if sel.Via == "uia" {
		res, err := readUIA(sel.PID, 1500*time.Millisecond)
		if err != nil || !res.supported || res.truncated {
			return ErrUnverified
		}
		if !sameText(res.text, sel.Text) {
			return ErrTextChanged
		}
	}
	return nil
}

// Replace types text over the selection, by pasting it: UI Automation can
// only set a whole value, and Ctrl+V is what every editor, WeChat included,
// understands. The clipboard is put back half a second later, unless the user
// or another program changed it meanwhile. Errors say why nothing was pasted;
// the caller then copies the translation for the user instead.
func Replace(sel Selection, text string) error {
	if sel.Unverified {
		return ErrUnverified
	}
	// The shortcut is usually still held; wait for it first, so that what is
	// checked afterwards is what the paste will land in.
	releaseModifiers()
	if err := confirm(sel); err != nil {
		return err
	}
	clipMu.Lock()
	snap, err := takeSnapshot()
	if err != nil {
		clipMu.Unlock()
		txns.Add(1)
		if strings.Contains(err.Error(), "占用") {
			return ErrClipboardBusy
		}
		return err
	}
	if err := setClipboardText(text, true); err != nil {
		snap.free()
		clipMu.Unlock()
		txns.Add(1)
		return ErrClipboardBusy
	}
	mine := ClipboardSeq()
	// Only key state and the focus are looked at again here: nothing waits
	// between this and the key press.
	if cur, ok := GetForeground(); !ok || cur.Window != sel.Window || cur.App.PID != sel.PID || FocusWindow(sel.Window) != sel.Focus {
		_ = snap.restore(mine)
		clipMu.Unlock()
		return ErrFocusChanged
	}
	ctrlChord(vkV)
	go func() {
		defer clipMu.Unlock()
		time.Sleep(500 * time.Millisecond) // the app reads the clipboard when it gets to the key
		_ = snap.restore(mine)
	}()
	return nil
}

// Busy reports whether Tolk is using the clipboard on its own account, so
// that the double-copy watcher does not take the changes for the user's.
func Busy() bool {
	if clipMu.TryLock() {
		clipMu.Unlock()
		return false
	}
	return true
}

//go:build windows

package winsel

import (
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"tolk/internal/inputassist"
)

// These tests drive a real window with a real EDIT control, so they need a
// desktop that accepts input (a GitHub Actions Windows runner has one). They
// only run when TOLK_WIN_E2E=1.

var (
	pDestroyWindow = user32.NewProc("DestroyWindow")
	pSendMessage   = user32.NewProc("SendMessageW")
	pSetForeground = user32.NewProc("SetForegroundWindow")
	pSetFocus      = user32.NewProc("SetFocus")
	pPeekMessage   = user32.NewProc("PeekMessageW")
	pTranslateMsg  = user32.NewProc("TranslateMessage")
	pKeybdEvent    = user32.NewProc("keybd_event")
	pGetWindowText = user32.NewProc("GetWindowTextW")
)

const (
	wsOverlapped = 0x00CF0000
	wsVisible    = 0x10000000
	esMultiline  = 0x0004
	emSetSel     = 0x00B1
	wmSetText    = 0x000C
)

func needE2E(t *testing.T) {
	t.Helper()
	if os.Getenv("TOLK_WIN_E2E") != "1" {
		t.Skip("set TOLK_WIN_E2E=1 to run against a real desktop")
	}
}

// editWindow is a top level window that is one EDIT control, with a message
// loop of its own.
type editWindow struct {
	hwnd uintptr
	stop chan struct{}
	done chan struct{}
}

func newEditWindow(t *testing.T, text string) *editWindow {
	t.Helper()
	return newClassWindow(t, "EDIT", "Tolk e2e", text)
}

func newClassWindow(t *testing.T, class, name, text string) *editWindow {
	t.Helper()
	w := &editWindow{stop: make(chan struct{}), done: make(chan struct{})}
	ready := make(chan uintptr)
	go func() {
		runtime.LockOSThread()
		defer close(w.done)
		h, _, _ := pCreateWindowEx.Call(0, ptr(u16(class)), ptr(u16(name)), wsOverlapped|wsVisible|esMultiline, 100, 100, 500, 200, 0, 0, 0, 0)
		if h != 0 {
			pSendMessage.Call(h, wmSetText, 0, ptr(u16(text)))
		}
		ready <- h
		var msg [8]uintptr
		for {
			select {
			case <-w.stop:
				if h != 0 {
					pDestroyWindow.Call(h)
				}
				return
			default:
			}
			if r, _, _ := pPeekMessage.Call(ptr(&msg[0]), 0, 0, 0, 1); r != 0 {
				pTranslateMsg.Call(ptr(&msg[0]))
				pDispatchMessage.Call(ptr(&msg[0]))
			} else {
				time.Sleep(5 * time.Millisecond)
			}
		}
	}()
	w.hwnd = <-ready
	if w.hwnd == 0 {
		t.Fatal("cannot create the EDIT window")
	}
	t.Cleanup(func() { close(w.stop); <-w.done })
	return w
}

func (w *editWindow) text() string {
	buf := make([]uint16, 1024)
	pGetWindowText.Call(w.hwnd, ptr(&buf[0]), uintptr(len(buf)))
	return windows.UTF16ToString(buf)
}

func (w *editWindow) selectRange(from, to int) {
	pSendMessage.Call(w.hwnd, emSetSel, uintptr(from), uintptr(to))
}

// foreground brings the window to the front; Windows only allows that for a
// process that just had input, which a tap of Alt provides.
func (w *editWindow) foreground(t *testing.T) Foreground {
	t.Helper()
	for i := 0; i < 20; i++ {
		// A lone Alt tap opens the window's system menu, a modal loop that
		// would stall this window's message pump; a key in between prevents it.
		pKeybdEvent.Call(0x12, 0, 0, 0)
		pKeybdEvent.Call(0xE8, 0, 0, 0)
		pKeybdEvent.Call(0xE8, 0, 2, 0)
		pKeybdEvent.Call(0x12, 0, 2, 0)
		pSetForeground.Call(w.hwnd)
		pSetFocus.Call(w.hwnd)
		time.Sleep(100 * time.Millisecond)
		if fg, ok := GetForeground(); ok && fg.Window == Window(w.hwnd) {
			return fg
		}
	}
	t.Skip("this desktop would not give the test window the foreground")
	return Foreground{}
}

func TestEdit_ForegroundApp(t *testing.T) {
	needE2E(t)
	w := newEditWindow(t, "hello")
	fg := w.foreground(t)
	if !strings.HasSuffix(strings.ToLower(fg.App.Exe), ".test.exe") && !strings.HasSuffix(strings.ToLower(fg.App.Exe), ".exe") {
		t.Fatalf("exe %q", fg.App.Exe)
	}
	if fg.App.PID != os.Getpid() {
		t.Fatalf("pid %d, want %d", fg.App.PID, os.Getpid())
	}
	if HigherIntegrity(fg.App.PID) {
		t.Fatal("own process reported as more privileged")
	}
	if FocusWindow(fg.Window) != w.hwnd {
		t.Fatalf("focus %x, want %x", FocusWindow(fg.Window), w.hwnd)
	}
}

func TestEdit_CaptureByUIA(t *testing.T) {
	needE2E(t)
	w := newEditWindow(t, "Hello world, this is a test")
	w.selectRange(6, 11) // "world"
	fg := w.foreground(t)
	res, err := readUIA(fg.App.PID, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("uia: supported=%v text=%q rect=%+v have=%v", res.supported, res.text, res.rect, res.haveRect)
	if !res.supported {
		// The classic EDIT control is not guaranteed a text pattern; the rich
		// edit test below is the strict one.
		t.Skipf("the EDIT control did not expose a text pattern: %s", res.why)
	}
	if res.text != "world" {
		t.Fatalf("text %q", res.text)
	}
	if res.haveRect && (res.rect.W <= 0 || res.rect.H <= 0 || res.rect.X < 90 || res.rect.Y < 90) {
		t.Fatalf("rect %+v is not where the text is", res.rect)
	}
}

func TestRichEdit_CaptureByUIA(t *testing.T) {
	needE2E(t)
	if _, err := windows.LoadLibrary("Msftedit.dll"); err != nil {
		t.Skip("no rich edit control:", err)
	}
	w := newClassWindow(t, "RICHEDIT50W", "Tolk e2e rich", "Hello world, this is a test")
	w.selectRange(6, 11)
	fg := w.foreground(t)
	res, err := readUIA(fg.App.PID, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("uia: supported=%v text=%q rect=%+v have=%v why=%s", res.supported, res.text, res.rect, res.haveRect, res.why)
	if !res.supported {
		t.Fatalf("the rich edit control did not expose a text pattern: %s", res.why)
	}
	if res.text != "world" {
		t.Fatalf("text %q", res.text)
	}
}

func TestEdit_CaptureByCopyRestoresClipboard(t *testing.T) {
	needE2E(t)
	w := newEditWindow(t, "Hello world, this is a test")
	w.selectRange(0, 5)
	fg := w.foreground(t)
	if err := SetClipboardText("user's own clipboard"); err != nil {
		t.Fatal(err)
	}
	sel, err := copyCapture(fg, Selection{Window: fg.Window, PID: fg.App.PID, Focus: FocusWindow(fg.Window)}, uiaResult{})
	if err != nil {
		t.Fatal(err)
	}
	if sel.Text != "Hello" || sel.Via != "copy" {
		t.Fatalf("%+v", sel)
	}
	if got, _ := clipboardText(); got != "user's own clipboard" {
		t.Fatalf("clipboard is %q: the user's content was not put back", got)
	}
	// No selection: nothing is copied and that is reported as such.
	w.selectRange(3, 3)
	_, err = copyCapture(fg, Selection{Window: fg.Window, PID: fg.App.PID}, uiaResult{})
	if err != ErrNoSelection {
		t.Fatalf("no selection gave %v", err)
	}
}

func TestEdit_ReplaceByPaste(t *testing.T) {
	needE2E(t)
	w := newEditWindow(t, "Hello world, this is a test")
	w.selectRange(6, 11)
	fg := w.foreground(t)
	if err := SetClipboardText("keep me"); err != nil {
		t.Fatal(err)
	}
	sel, err := Capture(fg, true)
	if err != nil {
		t.Fatal(err)
	}
	if sel.Text != "world" {
		t.Fatalf("captured %q via %s", sel.Text, sel.Via)
	}
	if err := Replace(sel, "世界 🌏\nline two"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	want := "Hello 世界 🌏\r\nline two, this is a test"
	for w.text() != want && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if got := w.text(); got != want {
		t.Fatalf("text is %q, want %q (via %s)", got, want, sel.Via)
	}
	// The clipboard comes back once the app has had time to read it.
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if got, _ := clipboardText(); got == "keep me" {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	got, _ := clipboardText()
	t.Fatalf("clipboard is %q, want the user's content back", got)
}

func TestEdit_ReplaceRefusedWhenFocusMoved(t *testing.T) {
	needE2E(t)
	w := newEditWindow(t, "Hello world")
	w.selectRange(0, 5)
	fg := w.foreground(t)
	sel, err := Capture(fg, true)
	if err != nil {
		t.Fatal(err)
	}
	sel.Window = Window(0x10) // as if the selection were read in another window
	if err := Replace(sel, "x"); err != ErrFocusChanged {
		t.Fatalf("got %v", err)
	}
	if got := w.text(); got != "Hello world" {
		t.Fatalf("text changed to %q", got)
	}
}

func TestSnapshotRoundTripsSeveralFormats(t *testing.T) {
	needE2E(t)
	if err := openClipboard(); err != nil {
		t.Fatal(err)
	}
	pEmptyClipboard.Call()
	for _, f := range []struct {
		format uint32
		data   []byte
	}{{cfUnicodeText, append([]byte("a\x00b\x00"), 0, 0)}, {cfText, []byte("ab\x00")}} {
		h := globalCopy(f.data)
		pSetClipboardData.Call(uintptr(f.format), h)
	}
	closeClipboard()
	snap, err := takeSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if err := SetClipboardText("other"); err != nil {
		t.Fatal(err)
	}
	mine := ClipboardSeq()
	if err := snap.restore(mine + 1); err != errSuperseded {
		t.Fatalf("a newer clipboard must not be overwritten: %v", err)
	}
	if got, _ := clipboardText(); got != "other" {
		t.Fatalf("overwritten with %q", got)
	}
	if err := snap.restore(mine); err != nil {
		t.Fatal(err)
	}
	if got, _ := clipboardText(); got != "ab" {
		t.Fatalf("restored %q", got)
	}
}

func TestPanelPlacement(t *testing.T) {
	needE2E(t)
	title := "Tolk 候选面板 (test)"
	// Stands in for the panel window: only the title matters (an EDIT keeps
	// its text to itself, where FindWindow cannot see it).
	newClassWindow(t, "STATIC", title, "")
	p, err := FindPanel(title)
	if err != nil {
		t.Fatal(err)
	}
	r := p.PlaceNear(inputRect(400, 300, 80, 20), 420, 120)
	if r.W <= 0 || r.H <= 0 {
		t.Fatalf("rect %+v", r)
	}
	if !p.Visible() {
		t.Fatal("not visible")
	}
	got := p.Rect()
	if got.X != r.X || got.Y != r.Y {
		t.Fatalf("placed at %+v, reported %+v", r, got)
	}
	work := WorkArea(400, 300)
	if got.X < work.X || got.Right() > work.Right() || got.Y < work.Y || got.Bottom() > work.Bottom() {
		t.Fatalf("%+v outside the work area %+v", got, work)
	}
	if got.Y < 300+20 { // below the anchor when there is room
		t.Fatalf("panel at y=%d overlaps the anchor", got.Y)
	}
	p.Hide()
	if p.Visible() {
		t.Fatal("still visible")
	}
}

func TestHooksInstallAndRemove(t *testing.T) {
	needE2E(t)
	calls := 0
	kh, err := InstallKeyboard(func(vk int, m Mods) bool { calls++; return false })
	if err != nil {
		t.Fatal(err)
	}
	mh, err := InstallMouse(func(button int, down bool, x, y int) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	kh.Remove()
	mh.Remove()
	kh.Remove() // twice is fine
	_ = calls
}

func inputRect(x, y, w, h int) inputassist.Rect { return inputassist.Rect{X: x, Y: y, W: w, H: h} }

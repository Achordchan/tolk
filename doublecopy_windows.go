//go:build windows

package main

import (
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/egoist/mygo"
	"golang.org/x/sys/windows"

	"tolk/internal/doublecopy"
	"tolk/internal/doublekey"
)

// Double copy on Windows watches the Ctrl+C presses themselves, through the
// keyboard hook that also catches Ctrl+X+X (screenshot_hotkey_windows.go),
// as the Mac helper does with ⌘C. Watching the clipboard instead fired for
// any program that wrote the same text twice (clipboard managers, remote
// desktop sync, apps that copy in two steps) and had to wake every 40 ms;
// it is only the fallback when the hook cannot be installed, and while a
// window run as administrator is in front: Windows does not give a normal
// process's hook the keys typed into a window above its integrity level.

var (
	// keyHookOK is whether the hook is installed; keyHookDone is closed once
	// that is known.
	keyHookOK   atomic.Bool
	keyHookDone = make(chan struct{})
	copyFire    atomic.Pointer[func(string)]

	// Hook thread only.
	copyDetector doublekey.Detector
	copyPressSeq uint64 // clipboard counter at the last Ctrl+C press
	copyPressTxn uint64 // 输入助手's finished clipboard transactions then

	// copyPoll is set while the window in front is above Tolk's integrity
	// level; then the clipboard is polled and the hook leaves Ctrl+C alone,
	// so one gesture never counts twice.
	copyPoll     atomic.Bool
	copyPollMu   sync.Mutex
	copyPollWake = sync.NewCond(&copyPollMu)
	// foregroundMoved is signalled from the WinEvent hook on every change of
	// the foreground window.
	foregroundMoved = make(chan struct{}, 1)
)

// watchCopyKeys makes the keyboard hook report double copies to fire. false
// when there is no hook, so the clipboard has to be polled all the time.
func watchCopyKeys(fire func(text string)) bool {
	<-keyHookDone
	if !keyHookOK.Load() {
		Logs.Warnf("双击复制：键盘钩子不可用，改为检测剪贴板")
		return false
	}
	copyFire.Store(&fire)
	return true
}

// waitWhileKeysCover blocks while the keyboard hook sees Ctrl+C, so the
// clipboard poller sleeps instead of ticking. It reports whether it waited.
func waitWhileKeysCover() bool {
	if copyFire.Load() == nil {
		return false
	}
	copyPollMu.Lock()
	defer copyPollMu.Unlock()
	waited := false
	for !copyPoll.Load() {
		waited = true
		copyPollWake.Wait()
	}
	return waited
}

func setCopyPoll(on bool) {
	copyPollMu.Lock()
	defer copyPollMu.Unlock()
	if !copyPoll.Swap(on) && on {
		copyPollWake.Broadcast()
	}
}

var foregroundEvent = syscall.NewCallback(func(hook, event, hwnd, object, child, thread, at uintptr) uintptr {
	select { // the hook thread also serves the keyboard: never block it
	case foregroundMoved <- struct{}{}:
	default:
	}
	return 0
})

// watchForeground runs on the keyboard hook's thread, which pumps the
// messages the WinEvent hook needs; it returns the hook to remove. Changes are
// checked on another goroutine.
func watchForeground() (unhook func()) {
	const eventSystemForeground = 3
	h, _, _ := shotUser.NewProc("SetWinEventHook").Call(eventSystemForeground, eventSystemForeground, 0, foregroundEvent, 0, 0, 0)
	if h == 0 {
		Logs.Warnf("双击复制：无法跟踪前台窗口，管理员窗口里的连按复制可能没有反应")
		return func() {}
	}
	go func() {
		own := integrityLevel(windows.GetCurrentProcessToken())
		if own == 0 {
			own = 0x2000 // medium, a normal launch
		}
		for range foregroundMoved {
			setCopyPoll(foregroundAbove(own))
		}
	}()
	select { // whatever is in front now
	case foregroundMoved <- struct{}{}:
	default:
	}
	return func() { shotUser.NewProc("UnhookWinEvent").Call(h) }
}

// foregroundAbove reports whether the window in front belongs to a process
// above integrity level own, whose keys the hook does not get. A process
// whose token Tolk may not read counts as above: an elevated one is like that.
func foregroundAbove(own uint32) bool {
	hwnd, _, _ := frontGetForegroundWindow.Call()
	if hwnd == 0 {
		return false
	}
	var pid uint32
	frontGetWindowThreadProcess.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid == 0 || pid == uint32(os.Getpid()) {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return true
	}
	defer windows.CloseHandle(h)
	var token windows.Token
	if err := windows.OpenProcessToken(h, windows.TOKEN_QUERY, &token); err != nil {
		return true
	}
	defer token.Close()
	return integrityLevel(token) > own
}

// integrityLevel is the token's mandatory label RID (0x2000 medium, 0x3000
// high, that is run as administrator); 0 when it cannot be read.
func integrityLevel(t windows.Token) uint32 {
	var n uint32
	_ = windows.GetTokenInformation(t, windows.TokenIntegrityLevel, nil, 0, &n)
	if n < uint32(unsafe.Sizeof(windows.Tokenmandatorylabel{})) {
		return 0
	}
	buf := make([]byte, n)
	if err := windows.GetTokenInformation(t, windows.TokenIntegrityLevel, &buf[0], n, &n); err != nil {
		return 0
	}
	sid := (*windows.Tokenmandatorylabel)(unsafe.Pointer(&buf[0])).Label.Sid
	if sid == nil || sid.SubAuthorityCount() == 0 {
		return 0
	}
	return sid.SubAuthority(uint32(sid.SubAuthorityCount()) - 1)
}

func isModifierKey(key uint32) bool {
	switch key {
	case 0x10, 0x11, 0x12, 0x5B, 0x5C, 0xA0, 0xA1, 0xA2, 0xA3, 0xA4, 0xA5:
		return true
	}
	return false
}

// observeCopyKey sees every key typed on the keyboard (never injected ones,
// such as the Ctrl+C 输入助手 sends). It runs on the hook thread, before the
// focused app handles the key, and must return quickly. ctrlOnly is whether
// Ctrl is held without Alt, Shift or Win.
func observeCopyKey(key uint32, down, ctrlOnly bool) {
	// Pressing Ctrl again between the two copies is still a double copy.
	if isModifierKey(key) {
		return
	}
	fire := copyFire.Load()
	p := currentPrefs()
	if fire == nil || !p.DoubleCopy || copyPoll.Load() {
		copyDetector = doublekey.Detector{}
		return
	}
	c := key == 0x43
	fresh := c && down && ctrlOnly && !copyDetector.Down // not key repeat
	var seq, txn uint64
	if fresh {
		seq, _ = doublecopy.Sequence()
		txn = inputAssistClipboardTransactions()
	}
	copyDetector.Window = time.Duration(p.DoubleCopyMs) * time.Millisecond
	if copyDetector.Observe(c, down, ctrlOnly, time.Now()) {
		go finishDoubleCopy(*fire, copyPressSeq, seq, copyPressTxn)
	}
	if fresh {
		copyPressSeq, copyPressTxn = seq, txn
	}
}

// finishDoubleCopy reads what the two presses copied. The hook sees the
// second press before the app copies, so wait for the clipboard to change.
// An app may leave unchanged text alone; then it is enough that the first
// press copied. If neither did, what is on the clipboard is old text.
//
// 输入助手 also writes the clipboard and puts the user's text back. A
// transaction of its own that started (txn moved) or is running since the
// first press would pass the counter checks above with text nobody copied,
// so the gesture is dropped then, as the poller does.
func finishDoubleCopy(fire func(string), first, second, txn uint64) {
	borrowed := func() bool {
		return inputAssistClipboardBusy() || inputAssistClipboardTransactions() != txn
	}
	deadline := time.Now().Add(500 * time.Millisecond)
	for {
		if borrowed() {
			return
		}
		seq, _ := doublecopy.Sequence()
		if seq != second {
			break
		}
		if time.Now().After(deadline) {
			if second == first {
				return
			}
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	// The app may still hold the clipboard open while it writes.
	for range 6 {
		if borrowed() {
			return
		}
		text := strings.TrimSpace(mygo.Clipboard.ReadText())
		if borrowed() { // the read may have raced a transaction
			return
		}
		if text != "" {
			fire(text)
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
}

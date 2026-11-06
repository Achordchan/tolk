package main

import (
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"

	"tolk/internal/autostart"
	"tolk/internal/dlx"
	"tolk/internal/doublecopy"
	"tolk/internal/legacy"
)

// Copied is sent to the main window when text should be translated there.
var Copied = mygo.NewEvent[string]("copied")

func main() {
	// Before the lock: a second launch hands over to the first instance,
	// which must be allowed to come to the front.
	allowForegroundHandoff()
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}
	mygo.Bind(Tolk{})
	// The pages are written in Chinese; without this MyGo's own menus (Edit,
	// Window…) and dialogs stay English, as for an app written in English.
	mygo.App.SetLanguages("zh-Hans", "zh-Hant", "en")

	mygo.App.WhenReady(func() {
		// 2.2.2 and older kept everything in ~/.dzfyq; move it once, before
		// anything reads the settings.
		if moved, err := legacy.MigrateHome(); err != nil {
			Logs.Errorf("搬移旧设置目录失败：%v", err)
		} else if moved {
			Logs.Infof("设置已搬到新目录")
		}
		initPlugin()
		applyNativeTheme(loadPrefs().Theme)
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:  "Tolk",
			Width:  960,
			Height: 640,
			// Below this the toolbar no longer fits on one line and the
			// panes get scroll bars (src/style.css drops the pill captions
			// under 880 px to make room).
			MinWidth:        minWidth,
			MinHeight:       minHeight,
			BackgroundColor: mainBackground,
			StateKey:        "main",
			URL:             "/",
			// Show explicitly after sizing and registering startup callbacks.
			Hidden: true,
		})
		// A size saved by an older version, when the minimum was smaller, is
		// restored as is; grow it to the new minimum.
		if w, h := win.Size(); w < minWidth || h < minHeight {
			win.SetSize(max(w, minWidth), max(h, minHeight))
		}
		mainWindow.Store(win)
		tintTitleBar(loadPrefs().Theme == "pink")

		// Also restores the window when it was minimized rather than closed.
		showMain := func() { bringToFront(win) }
		mygo.App.OnSecondInstance(func(_ []string, _ string) {
			mygo.RunOnMain(showMain)
		})
		firstShow(win)
		showMain()

		// Closing the window only hides it: the app stays in the tray / Dock
		// and a shortcut must be able to bring the same window back. A
		// destroyed window ignores Show, which left the shortcuts doing nothing.
		var quitting atomic.Bool
		mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { quitting.Store(true); dlx.Stop() })
		win.OnClose(func(e *mygo.CloseEvent) {
			if !quitting.Load() {
				e.PreventDefault()
				if trayAvailable() {
					win.Hide()
				} else {
					mygo.App.Quit()
				}
			}
		})
		// Dock icon clicked. Not gated on "has visible windows": the Mini
		// bubble counts as one while the main window is hidden.
		mygo.App.OnActivate(func(bool) { showMain() })
		translateInMain := func(text string) {
			showMain()
			_ = Copied.Emit(win, text)
		}

		// Ctrl/Cmd+Shift+C: translate the clipboard in the main window.
		if err := mygo.GlobalShortcut.Register("CmdOrCtrl+Shift+C", func() {
			translateInMain(strings.TrimSpace(mygo.Clipboard.ReadText()))
		}); err != nil {
			log.Println("shortcut:", err)
		}

		onCopied := func(text string) {
			if currentPrefs().MiniMode {
				showMini(text)
			} else {
				translateInMain(text)
			}
		}
		// The Mini bubble and the 输入助手 panel are web views of their own;
		// starting them alongside the main window's makes all three slower
		// on a weak PC. They, and whatever may show them, start once the
		// main window is up.
		afterFirstShow(func() {
			createMini()
			createInputAssistPanel()
			go keepEngineWarm()
			go watchUpdates()
			go watchDoubleCopy(onCopied)
			startHelper(onCopied)
			startInputAssist()
		})

		setupTray(win, showMain)
		setupScreenshotShortcut()
		go func() {
			if err := autostart.Migrate(); err != nil {
				Logs.Warnf("开机自启：%v", err)
			}
		}()
	})

	if err := mygo.App.Run(); err != nil {
		log.Fatalf("fatal error: %v", err) // the launcher reports this line
	}
}

const (
	minWidth  = 760
	minHeight = 480
	// Bound the delay before starting auxiliary services if WebView readiness stalls.
	showAnyway = 4 * time.Second
)

var (
	shownOnce  sync.Once
	afterShown []func()
)

// firstShow defers auxiliary services while the immediately visible window loads.
func firstShow(win *mygo.Window) {
	ready := func() {
		mygo.RunOnMain(func() {
			shownOnce.Do(func() {
				for _, fn := range afterShown {
					fn()
				}
				afterShown = nil
			})
		})
	}
	win.OnReadyToShow(ready)
	time.AfterFunc(showAnyway, ready)
}

// afterFirstShow runs fn on the main thread once the main window is first up.
// Call it from App.WhenReady, before the window can be ready.
func afterFirstShow(fn func()) { afterShown = append(afterShown, fn) }

// watchDoubleCopy polls the clipboard change counter: the fallback, on both
// systems, when the Ctrl/⌘+C presses themselves cannot be watched. Reading
// the counter is nearly free, so it is sampled every 40 ms: two copies closer
// together than that are not something a hand can produce, and the clipboard
// text is only read when the counter actually moved.
func watchDoubleCopy(fire func(text string)) {
	// Windows watches the Ctrl+C presses instead (doublecopy_windows.go):
	// the clipboard is only polled while they cannot be seen, with no
	// keyboard hook or a window run as administrator in front.
	watchCopyKeys(fire)
	if _, ok := doublecopy.Sequence(); !ok {
		log.Println("double copy: clipboard counter unavailable on this system")
		return
	}
	d := &doublecopy.Detector{}
	var seen uint64
	for range time.Tick(40 * time.Millisecond) {
		if waitWhileKeysCover() {
			d, seen = &doublecopy.Detector{}, inputAssistClipboardTransactions()
			continue
		}
		p := currentPrefs()
		// A transaction that began and ended between two polls leaves the
		// same text behind under a new counter, which is not a second copy.
		txns := inputAssistClipboardTransactions()
		borrowed := inputAssistClipboardBusy() || txns != seen
		seen = txns
		if !p.DoubleCopy || hotkeyActive() || borrowed { // the helper's hotkey handles it instead; 输入助手 borrows the clipboard
			d = &doublecopy.Detector{} // forget history while switched off
			continue
		}
		d.Window = time.Duration(p.DoubleCopyMs) * time.Millisecond
		seq, _ := doublecopy.Sequence()
		if text, ok := d.Observe(seq, time.Now(), mygo.Clipboard.ReadText); ok {
			fire(text)
		}
	}
}

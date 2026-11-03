package main

import (
	"log"
	"strings"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"

	"tolk/internal/autostart"
	"tolk/internal/doublecopy"
	"tolk/internal/legacy"
)

// Copied is sent to the main window when text should be translated there.
var Copied = mygo.NewEvent[string]("copied")

func main() {
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
		applyNativeTheme(loadPrefs().Theme)
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:           "Tolk",
			Width:           960,
			Height:          640,
			MinWidth:        480,
			MinHeight:       360,
			BackgroundColor: mainBackground,
			StateKey:        "main",
			URL:             "/",
		})
		mainWindow.Store(win)
		tintTitleBar(loadPrefs().Theme == "pink")
		createMini()
		go keepEngineWarm()
		createInputAssistPanel()

		showMain := func() { win.Show(); win.Focus() }

		// Closing the window only hides it: the app stays in the tray / Dock
		// and a shortcut must be able to bring the same window back. A
		// destroyed window ignores Show, which left the shortcuts doing nothing.
		var quitting atomic.Bool
		mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { quitting.Store(true) })
		win.OnClose(func(e *mygo.CloseEvent) {
			if !quitting.Load() {
				e.PreventDefault()
				win.Hide()
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
		go watchUpdates()
		go watchDoubleCopy(onCopied)
		startHelper(onCopied)
		startInputAssist()

		setupTray(win, showMain)
		go func() {
			if err := autostart.Migrate(); err != nil {
				Logs.Warnf("开机自启：%v", err)
			}
		}()
	})

	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// watchDoubleCopy polls the clipboard change counter, as the Mac app does.
// Reading the counter is nearly free, so it is sampled every 40 ms: two copies
// closer together than that are not something a hand can produce, and the
// clipboard text is only read when the counter actually moved.
func watchDoubleCopy(fire func(text string)) {
	if _, ok := doublecopy.Sequence(); !ok {
		log.Println("double copy: clipboard counter unavailable on this system")
		return
	}
	d := &doublecopy.Detector{}
	var seen uint64
	for range time.Tick(40 * time.Millisecond) {
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

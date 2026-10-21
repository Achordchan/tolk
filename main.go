package main

import (
	"log"
	"strings"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"

	"tolk/internal/doublecopy"
)

// Copied is sent to the main window when text should be translated there.
var Copied = mygo.NewEvent[string]("copied")

func main() {
	mygo.Bind(Tolk{})

	mygo.App.WhenReady(func() {
		loadPrefs()
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:           "Tolk",
			Width:           960,
			Height:          640,
			MinWidth:        480,
			MinHeight:       360,
			BackgroundColor: "light-dark(#ececec, #2b2b2b)",
			StateKey:        "main",
			URL:             "/",
		})
		createMini()
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

		menu := mygo.NewMenu([]*mygo.MenuItem{
			{Label: "Show Tolk", Click: func(*mygo.MenuItem, *mygo.Window) { showMain() }},
			mygo.Separator(),
			{Label: "Quit", Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() }},
		})
		if _, err := mygo.NewTray(mygo.TrayOptions{ToolTip: "Tolk", Menu: menu}); err != nil {
			log.Println("tray:", err)
		}
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
	for range time.Tick(40 * time.Millisecond) {
		p := currentPrefs()
		if !p.DoubleCopy || hotkeyActive() || inputAssistClipboardBusy() { // the helper's hotkey handles it instead; 输入助手 borrows the clipboard
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

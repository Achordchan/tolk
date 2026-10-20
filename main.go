package main

import (
	"log"
	"strings"
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

		showMain := func() { win.Show(); win.Focus() }
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

		go watchDoubleCopy(func(text string) {
			if currentPrefs().MiniMode {
				showMini(text)
			} else {
				translateInMain(text)
			}
		})

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
func watchDoubleCopy(fire func(text string)) {
	if _, ok := doublecopy.Sequence(); !ok {
		log.Println("double copy: clipboard counter unavailable on this system")
		return
	}
	d := &doublecopy.Detector{}
	for range time.Tick(180 * time.Millisecond) {
		p := currentPrefs()
		if !p.DoubleCopy {
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

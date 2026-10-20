package main

import (
	"log"
	"strings"

	"github.com/egoist/mygo"

	"tolk/internal/legacy"
)

// Tolk is callable from the frontend.
type Tolk struct{}

// Version of the app, shown in the prototype page.
func (Tolk) Version() string { return "0.1.0-proto" }

// LegacyConfig summarizes an existing dazuo-win config (no secrets).
func (Tolk) LegacyConfig() (legacy.Summary, error) {
	p, err := legacy.DefaultPath()
	if err != nil {
		return legacy.Summary{}, err
	}
	return legacy.Summarize(p)
}

// ClipboardText returns the current clipboard text.
func (Tolk) ClipboardText() string { return mygo.Clipboard.ReadText() }

// Copied is sent when the global shortcut fires, with the clipboard text.
var Copied = mygo.NewEvent[string]("copied")

func main() {
	mygo.Bind(Tolk{})

	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:           "Tolk",
			Width:           960,
			Height:          640,
			MinWidth:        480,
			MinHeight:       360,
			BackgroundColor: "light-dark(#f6f7f9, #0f1115)",
			StateKey:        "main",
			URL:             "/",
		})

		show := func() { win.Show(); win.Focus() }

		// Global shortcut, same default as dazuo-win's "copy and translate".
		if err := mygo.GlobalShortcut.Register("CmdOrCtrl+Shift+C", func() {
			show()
			_ = Copied.Emit(win, strings.TrimSpace(mygo.Clipboard.ReadText()))
		}); err != nil {
			log.Println("shortcut:", err)
		}

		menu := mygo.NewMenu([]*mygo.MenuItem{
			{Label: "Show Tolk", Click: func(*mygo.MenuItem, *mygo.Window) { show() }},
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

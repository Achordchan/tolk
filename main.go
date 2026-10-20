package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"tolk/internal/engine"
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

// TranslateResult is what the page shows after a translation.
type TranslateResult struct {
	Text       string `json:"text"`
	Detected   string `json:"detected"`
	Engine     string `json:"engine"`
	DurationMs int64  `json:"durationMs"`
}

// Languages are the labels for the language pickers.
func (Tolk) Languages() []string { return engine.Languages }

// Translate runs the engine chosen in the legacy config. Newlines are
// wrapped in a marker so engines keep paragraph breaks, as dazuo-win did.
func (Tolk) Translate(text, source, target string) (TranslateResult, error) {
	p, err := legacy.DefaultPath()
	if err != nil {
		return TranslateResult{}, err
	}
	cfg, err := legacy.Load(p)
	if err != nil {
		cfg = map[string]any{} // no config yet: the default engine needs no key
	}
	api := legacy.APIName(cfg)
	eng, err := legacy.BuildEngine(cfg, api)
	if err != nil {
		return TranslateResult{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	token := fmt.Sprintf("[[DZFYQ_NL_%x]]", time.Now().UnixNano())
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	start := time.Now()
	res, err := eng.Translate(ctx, strings.ReplaceAll(text, "\n", " "+token+" "), source, target)
	if err != nil {
		return TranslateResult{}, err
	}
	out := strings.ReplaceAll(strings.ReplaceAll(res.Text, " "+token+" ", "\n"), token, "\n")
	return TranslateResult{Text: out, Detected: res.Detected, Engine: api, DurationMs: time.Since(start).Milliseconds()}, nil
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

package main

import (
	"log"
	"runtime"
	"strings"
	"sync"

	"github.com/egoist/mygo"

	"tolk/internal/direction"
	"tolk/internal/engine"
)

// The bubble widens with the translation so a long one is not a narrow
// column; past miniMaxWidth it wraps and grows taller instead. src/mini.ts
// uses the same limits.
const (
	miniMinWidth  = 300
	miniMaxWidth  = 560
	miniMinHeight = 96
	miniMaxHeight = 420
)

// MiniState is sent to the bubble page.
type MiniState struct {
	State    string `json:"state"` // translating | result | error
	Text     string `json:"text"`
	Detected string `json:"detected"`
	Smart    bool   `json:"smart"` // the direction differs from the configured pair
	Source   string `json:"source"`
	Target   string `json:"target"`
}

// MiniUpdate carries MiniState to the bubble window.
var MiniUpdate = mygo.NewEvent[MiniState]("mini")

var (
	miniMu  sync.Mutex
	miniWin *mygo.Window
	miniSeq int
	miniW   = miniMinWidth
	miniH   = miniMinHeight
	// miniText is the copied text the bubble is translating, for 在主窗口翻译.
	miniText string
	// miniAt is the cursor when the copy happened; the bubble stays there
	// while it grows, even if the mouse has moved on.
	miniAt mygo.Point
	// miniShown is whether the bubble should be up (guarded by miniMu);
	// syncMiniWatch makes the mouse watch follow it.
	miniShown     bool
	miniPageReady bool
	miniState     MiniState

	miniWatchMu sync.Mutex
	miniWatched bool
)

// syncMiniWatch watches outside clicks exactly while the bubble is up. Show
// and hide call it from goroutines that may run in either order, so it reads
// the current state instead of being told one: a stale call changes nothing.
func syncMiniWatch() {
	miniWatchMu.Lock()
	defer miniWatchMu.Unlock()
	miniMu.Lock()
	want := miniShown
	miniMu.Unlock()
	if want != miniWatched {
		watchMiniClicks(want)
		miniWatched = want
	}
}

// miniTitle names the bubble's window, which Windows finds it by.
const miniTitle = "Tolk Mini"

func createMini() {
	miniWin = mygo.NewWindow(mygo.WindowOptions{
		Title:           miniTitle,
		URL:             "/mini.html",
		Width:           miniMinWidth,
		Height:          miniMinHeight,
		Hidden:          true,
		Frameless:       true,
		Panel:           runtime.GOOS == "windows",
		DisableMinimize: true,
		AlwaysOnTop:     true,
		SkipTaskbar:     true,
		DisableMaximize: true,
		BackgroundColor: "light-dark(#fafafa, #232323)",
	})
}

// miniWorkArea is the work area of the display the copy happened on.
func miniWorkArea() mygo.Rectangle {
	miniMu.Lock()
	pt := miniAt
	miniMu.Unlock()
	return mygo.Screen.DisplayNearestPoint(pt).WorkArea
}

// placeMini puts the bubble next to where the copy happened, kept inside the work area.
func placeMini(w, h int) {
	miniMu.Lock()
	pt := miniAt
	miniMu.Unlock()
	wa := miniWorkArea()
	x, y := int(pt.X)+14, int(pt.Y)+18
	if x+w > int(wa.X+wa.Width) {
		x = int(wa.X+wa.Width) - w - 8
	}
	if y+h > int(wa.Y+wa.Height) {
		y = int(pt.Y) - h - 14
	}
	if x < int(wa.X) {
		x = int(wa.X) + 8
	}
	if y < int(wa.Y) {
		y = int(wa.Y) + 8
	}
	miniWin.SetPosition(x, y)
}

// showMini translates text in the bubble next to the cursor.
func showMini(text string) {
	mygo.RunOnMain(func() { showMiniOnMain(text) })
}

func showMiniOnMain(text string) {
	text = strings.TrimSpace(text)
	if text == "" || miniWin == nil {
		return
	}
	p := currentPrefs()
	pair := direction.Resolve(text, p.SourceLang, p.TargetLang)
	smart := pair.Source != p.SourceLang || pair.Target != p.TargetLang

	miniMu.Lock()
	miniSeq++
	mine := miniSeq
	miniW, miniH = miniMinWidth, miniMinHeight
	miniText = text
	miniAt = mygo.Screen.CursorScreenPoint()
	miniShown = true
	miniMu.Unlock()

	miniWin.SetSize(miniMinWidth, miniMinHeight)
	placeMini(miniMinWidth, miniMinHeight)
	miniMu.Lock()
	miniState = MiniState{State: "translating", Smart: smart, Source: pair.Source, Target: pair.Target}
	ready, state := miniPageReady, miniState
	miniMu.Unlock()
	if ready {
		_ = MiniUpdate.Emit(miniWin, state)
	}
	restoreMiniInactive(miniWin)
	miniWin.ShowInactive()
	Logs.Infof("Mini 显示：visible=%v minimized=%v panel=%v", miniWin.IsVisible(), miniWin.IsMinimized(), miniWin.IsPanel())
	go syncMiniWatch() // an outside click closes the bubble

	go func() {
		res, err := translate(text, pair.Source, pair.Target)
		// The guess was wrong: the engine says the text is already in the
		// target language, which would show it back unchanged. Translate it
		// the other way instead.
		if err == nil && engine.LabelForCode(res.Detected) == pair.Target {
			other := direction.Other(pair.Target, p.SourceLang)
			if r2, err2 := translate(text, engine.Auto, other); err2 == nil {
				res, pair.Source, pair.Target, smart = r2, engine.Auto, other, true
			}
		}
		miniMu.Lock()
		stale := mine != miniSeq
		miniMu.Unlock()
		if stale {
			return // a newer copy replaced this one
		}
		st := MiniState{Smart: smart, Source: pair.Source, Target: pair.Target}
		if err != nil {
			st.State, st.Text = "error", err.Error()
		} else {
			st.State, st.Text, st.Detected = "result", res.Text, res.Detected
		}
		mygo.RunOnMain(func() {
			miniMu.Lock()
			if mine != miniSeq || !miniShown {
				miniMu.Unlock()
				return
			}
			miniState = st
			ready := miniPageReady
			miniMu.Unlock()
			if ready {
				if err := MiniUpdate.Emit(miniWin, st); err != nil {
					log.Println("mini:", err)
				}
			}
		})
	}()
}

// ResizeMini fits the bubble to its content; the page reports the size.
func (Tolk) ResizeMini(width, height int) {
	if miniWin == nil {
		return
	}
	width = min(max(width, miniMinWidth), miniMaxWidth)
	// A narrow screen: still leave the margin placeMini keeps.
	if wa := miniWorkArea().Width - 16; width > wa {
		width = max(wa, miniMinWidth)
	}
	height = min(max(height, miniMinHeight), miniMaxHeight)
	miniMu.Lock()
	changed := width != miniW || height != miniH
	miniW, miniH = width, height
	miniMu.Unlock()
	if changed {
		miniWin.SetSize(width, height)
		placeMini(width, height)
	}
}

// MiniToMain closes the bubble and translates its text in the main window,
// where it can be edited or sent the other way.
func (Tolk) MiniToMain() {
	miniMu.Lock()
	text := miniText
	miniMu.Unlock()
	Tolk{}.HideMini()
	if text != "" {
		translateInMain(text)
	}
}

// HideMini closes the bubble and drops the request in flight.
func (Tolk) HideMini() {
	if miniWin == nil {
		return
	}
	miniMu.Lock()
	miniSeq++
	miniShown = false
	miniMu.Unlock()
	miniWin.Hide()
	go syncMiniWatch()
}

// miniOutsideClick closes the bubble when the click landed outside its window.
func miniOutsideClick(x, y int) {
	if miniWin == nil || !miniWin.IsVisible() {
		return
	}
	b := miniWin.Bounds()
	if x >= b.X && x < b.X+b.Width && y >= b.Y && y < b.Y+b.Height {
		return
	}
	Tolk{}.HideMini()
}

// MiniReady replays the latest state only after the page installed its listener.
func (Tolk) MiniReady() {
	mygo.RunOnMain(func() {
		miniMu.Lock()
		miniPageReady = true
		shown, state := miniShown, miniState
		miniMu.Unlock()
		if shown && miniWin != nil {
			_ = MiniUpdate.Emit(miniWin, state)
		}
	})
}

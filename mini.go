package main

import (
	"log"
	"strings"
	"sync"

	"github.com/egoist/mygo"

	"tolk/internal/direction"
)

const (
	miniWidth     = 380
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
	miniH   = miniMinHeight
	// miniAt is the cursor when the copy happened; the bubble stays there
	// while it grows, even if the mouse has moved on.
	miniAt mygo.Point
)

func createMini() {
	miniWin = mygo.NewWindow(mygo.WindowOptions{
		Title:           "Tolk Mini",
		URL:             "/mini.html",
		Width:           miniWidth,
		Height:          miniMinHeight,
		Hidden:          true,
		Frameless:       true,
		AlwaysOnTop:     true,
		SkipTaskbar:     true,
		DisableMaximize: true,
		BackgroundColor: "light-dark(#fafafa, #232323)",
	})
}

// placeMini puts the bubble next to where the copy happened, kept inside the work area.
func placeMini(h int) {
	miniMu.Lock()
	pt := miniAt
	miniMu.Unlock()
	wa := mygo.Screen.DisplayNearestPoint(pt).WorkArea
	x, y := int(pt.X)+14, int(pt.Y)+18
	if x+miniWidth > int(wa.X+wa.Width) {
		x = int(wa.X+wa.Width) - miniWidth - 8
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
	miniH = miniMinHeight
	miniAt = mygo.Screen.CursorScreenPoint()
	miniMu.Unlock()

	miniWin.SetSize(miniWidth, miniMinHeight)
	placeMini(miniMinHeight)
	_ = MiniUpdate.Emit(miniWin, MiniState{State: "translating", Smart: smart, Source: pair.Source, Target: pair.Target})
	miniWin.ShowInactive()
	go macWatchMouse(true) // an outside click closes the bubble

	go func() {
		res, err := translate(text, pair.Source, pair.Target)
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
		if err := MiniUpdate.Emit(miniWin, st); err != nil {
			log.Println("mini:", err)
		}
	}()
}

// ResizeMini fits the bubble to its content; the page reports the height.
func (Tolk) ResizeMini(height int) {
	if miniWin == nil {
		return
	}
	if height < miniMinHeight {
		height = miniMinHeight
	}
	if height > miniMaxHeight {
		height = miniMaxHeight
	}
	miniMu.Lock()
	changed := height != miniH
	miniH = height
	miniMu.Unlock()
	if changed {
		miniWin.SetSize(miniWidth, height)
		placeMini(height)
	}
}

// HideMini closes the bubble and drops the request in flight.
func (Tolk) HideMini() {
	if miniWin == nil {
		return
	}
	miniMu.Lock()
	miniSeq++
	miniMu.Unlock()
	miniWin.Hide()
	go macWatchMouse(false)
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

//go:build windows

package winsel

import (
	"errors"

	"tolk/internal/inputassist"
)

const (
	gwlExStyle     = ^uintptr(19) // -20
	wsExNoActivate = 0x08000000
	wsExToolWindow = 0x00000080
	wsExTopmost    = 0x00000008
	wsExTransp     = 0x00000020

	swpNoSize     = 0x0001
	swpNoMove     = 0x0002
	swpNoActivate = 0x0010
	swpShowWindow = 0x0040
	swpHideWindow = 0x0080
	hwndTopmost   = ^uintptr(0) // -1
)

// Panel is one of Tolk's own floating windows (the candidate panel): shown
// without taking the keyboard, placed in screen pixels.
type Panel struct{ hwnd uintptr }

// FindPanel finds the top-level window with the given title and makes it a
// window that never activates, so that the app being typed in keeps the focus.
func FindPanel(title string) (*Panel, error) {
	h, _, _ := pFindWindow.Call(0, ptr(u16(title)))
	if h == 0 {
		return nil, errors.New("找不到候选面板窗口")
	}
	ex, _, _ := pGetWindowLongPtr.Call(h, gwlExStyle)
	pSetWindowLongPtr.Call(h, gwlExStyle, ex|wsExNoActivate|wsExToolWindow|wsExTopmost)
	return &Panel{hwnd: h}, nil
}

// WindowRect is the rectangle, in screen pixels, of the top-level window with
// the given title, and whether it is on screen.
func WindowRect(title string) (inputassist.Rect, bool) {
	h, _, _ := pFindWindow.Call(0, ptr(u16(title)))
	if h == 0 {
		return inputassist.Rect{}, false
	}
	if v, _, _ := pIsWindowVisible.Call(h); v == 0 {
		return inputassist.Rect{}, false
	}
	r, _ := windowRect(h)
	return r, true
}

// windowRect is a window's rectangle in screen pixels.
func windowRect(h uintptr) (inputassist.Rect, bool) {
	var r rect
	if ok, _, _ := pGetWindowRect.Call(h, ptr(&r)); ok == 0 || r.Right <= r.Left || r.Bottom <= r.Top {
		return inputassist.Rect{}, false
	}
	return inputassist.Rect{X: int(r.Left), Y: int(r.Top), W: int(r.Right - r.Left), H: int(r.Bottom - r.Top)}, true
}

// Visible reports whether the panel is on screen.
func (p *Panel) Visible() bool {
	r, _, _ := pIsWindowVisible.Call(p.hwnd)
	return r != 0
}

// Rect is the panel's rectangle in screen pixels.
func (p *Panel) Rect() inputassist.Rect {
	var r rect
	pGetWindowRect.Call(p.hwnd, ptr(&r))
	return inputassist.Rect{X: int(r.Left), Y: int(r.Top), W: int(r.Right - r.Left), H: int(r.Bottom - r.Top)}
}

// Hide takes the panel off screen.
func (p *Panel) Hide() {
	pSetWindowPos.Call(p.hwnd, 0, 0, 0, 0, 0, swpNoSize|swpNoMove|swpNoActivate|swpHideWindow|0x0004 /* NOZORDER */)
}

// Scale is pixels per DIP for the panel's current monitor.
func (p *Panel) Scale() float64 {
	d, _, _ := pGetDpiForWindow.Call(p.hwnd)
	if d == 0 {
		return 1
	}
	return float64(d) / 96
}

// PlaceNear shows the panel at wDIP×hDIP next to anchor (screen pixels): below
// it, above if there is no room, always inside the work area of the monitor.
// It returns the rectangle the panel got, in pixels.
func (p *Panel) PlaceNear(anchor inputassist.Rect, wDIP, hDIP int) inputassist.Rect {
	dpi := DPIAt(anchor.X, anchor.Y)
	var out inputassist.Rect
	for attempt := 0; attempt < 3; attempt++ {
		w, h := wDIP*dpi/96, hDIP*dpi/96
		work := WorkArea(anchor.X, anchor.Y)
		x, y := inputassist.PanelOrigin(anchor, w, h, work)
		pSetWindowPos.Call(p.hwnd, hwndTopmost, uintptr(int32(x)), uintptr(int32(y)), uintptr(w), uintptr(h), swpNoActivate|swpShowWindow)
		out = inputassist.Rect{X: x, Y: y, W: w, H: h}
		// Moving to a monitor with another DPI changes the window's own DPI,
		// and the window system may resize it for that: measure and redo.
		actual := int(p.Scale()*96 + 0.5)
		if actual == dpi {
			break
		}
		dpi = actual
	}
	return out
}

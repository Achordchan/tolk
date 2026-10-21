package inputassist

import "strings"

// RowKind is what a candidate row currently shows.
type RowKind string

const (
	// RowLoading is the skeleton every row starts as.
	RowLoading RowKind = "loading"
	// RowTranslated has a translation.
	RowTranslated RowKind = "translated"
	// RowFailed failed on its own; clicking it retries. One failure does not
	// take the others down.
	RowFailed RowKind = "failed"
)

// RowState is the content of one row.
type RowState struct {
	Kind RowKind `json:"kind"`
	Text string  `json:"text,omitempty"`
	// Cached marks a translation that came from the local cache (shown with ⚡).
	Cached    bool   `json:"cached,omitempty"`
	LatencyMs int    `json:"latencyMs,omitempty"`
	Engine    string `json:"engine,omitempty"`
	Message   string `json:"message,omitempty"`
}

// Loading is the state of a row waiting for its translation.
func Loading() RowState { return RowState{Kind: RowLoading} }

// Row is one target language.
type Row struct {
	Code  string   `json:"code"`  // zh-CN, en…
	Label string   `json:"label"` // the compact tag: ZH-CN, EN
	State RowState `json:"state"`
}

// Committable reports whether the row has a non-blank translation to use.
func (r Row) Committable() bool {
	return r.State.Kind == RowTranslated && strings.TrimSpace(r.State.Text) != ""
}

// List is the candidate list: the rows and which one is highlighted. It knows
// nothing about windows or translation.
type List struct {
	Rows     []Row
	Selected int // always starts at the first row
}

// NewList starts every code as a skeleton row.
func NewList(codes []string) List {
	l := List{}
	for _, c := range codes {
		l.Rows = append(l.Rows, Row{Code: c, Label: DisplayCode(c), State: Loading()})
	}
	return l
}

// Move shifts the highlight and stops at the ends. It reports a change.
func (l *List) Move(delta int) bool {
	if len(l.Rows) == 0 {
		return false
	}
	t := min(max(l.Selected+delta, 0), len(l.Rows)-1)
	if t == l.Selected {
		return false
	}
	l.Selected = t
	return true
}

// Select highlights row i. It reports a change.
func (l *List) Select(i int) bool {
	if i < 0 || i >= len(l.Rows) || i == l.Selected {
		return false
	}
	l.Selected = i
	return true
}

// Update sets the state of the row for code. It reports a change.
func (l *List) Update(code string, st RowState) bool {
	for i := range l.Rows {
		if l.Rows[i].Code == code {
			if l.Rows[i].State == st {
				return false
			}
			l.Rows[i].State = st
			return true
		}
	}
	return false
}

// Committable reports whether row i has a translation to use.
func (l *List) Committable(i int) bool {
	return i >= 0 && i < len(l.Rows) && l.Rows[i].Committable()
}

// Text is the translation of row i, or "".
func (l *List) Text(i int) string {
	if !l.Committable(i) {
		return ""
	}
	return l.Rows[i].State.Text
}

// ---- keys ----

// Key codes are Windows virtual-key codes.
const (
	VKBack     = 0x08
	VKTab      = 0x09
	VKReturn   = 0x0D
	VKShift    = 0x10
	VKControl  = 0x11
	VKMenu     = 0x12
	VKEscape   = 0x1B
	VKSpace    = 0x20
	VKLeft     = 0x25
	VKUp       = 0x26
	VKRight    = 0x27
	VKDown     = 0x28
	VKLWin     = 0x5B
	VKRWin     = 0x5C
	VKLShift   = 0xA0
	VKRShift   = 0xA1
	VKLControl = 0xA2
	VKRControl = 0xA3
	VKLMenu    = 0xA4
	VKRMenu    = 0xA5
	vkC        = 0x43
	vk1        = 0x31
	vk6        = 0x36
	vkNum1     = 0x61
	vkNum6     = 0x66
)

// KeyEvent is one key press, as the keyboard hook sees it.
type KeyEvent struct {
	VK    int
	Ctrl  bool
	Alt   bool
	Shift bool
	Win   bool
}

// IsModifier is true for the modifier keys themselves. Pressing or releasing
// one is never a reason to change the panel: the user is still holding the
// shortcut's modifiers when it appears.
func IsModifier(vk int) bool {
	switch vk {
	case VKShift, VKControl, VKMenu, VKLWin, VKRWin, VKLShift, VKRShift, VKLControl, VKRControl, VKLMenu, VKRMenu:
		return true
	}
	return false
}

// Action is what a key does to the open panel.
type Action string

const (
	ActionMoveUp   Action = "moveUp"
	ActionMoveDown Action = "moveDown"
	// ActionCommit replaces the source with the highlighted translation.
	ActionCommit Action = "commit"
	// ActionCommitIndex picks row Index directly (Ctrl+1 to Ctrl+6).
	ActionCommitIndex Action = "commitIndex"
	ActionCopy        Action = "copy"
	ActionDismiss     Action = "dismiss"
	// ActionDismissPass closes the panel but the key still goes to the app
	// (ordinary typing, cursor keys).
	ActionDismissPass Action = "dismissPass"
)

// Decision is the verdict on a key.
type Decision struct {
	Action  Action
	Index   int  // for ActionCommitIndex
	Swallow bool // true: the app never sees the key
}

// Route decides what a key does while the panel is open. ok is false when the
// key is none of the panel's business and must pass untouched with the panel
// unchanged. It is the old app's InputAssistKeyRouter, with Ctrl in place of ⌘.
func Route(e KeyEvent, l *List) (d Decision, ok bool) {
	if IsModifier(e.VK) {
		return Decision{}, false
	}
	if e.Ctrl {
		// Ctrl must be the only modifier, or Ctrl+Shift+3 style shortcuts of
		// the app would count as Ctrl+3.
		only := !e.Shift && !e.Alt && !e.Win
		if only {
			if i := digitIndex(e.VK); i >= 0 {
				// Ctrl+4 with three candidates is the app's own shortcut: leave it.
				if i >= len(l.Rows) || !l.Committable(i) {
					return Decision{}, false
				}
				return Decision{Action: ActionCommitIndex, Index: i, Swallow: true}, true
			}
			if e.VK == vkC {
				if !l.Committable(l.Selected) {
					return Decision{}, false
				}
				return Decision{Action: ActionCopy, Swallow: true}, true
			}
		}
		// Any other Ctrl combination edits the text or moves the cursor, so
		// what the panel recorded is stale: close it, never eat the shortcut.
		return Decision{Action: ActionDismissPass}, true
	}
	// Panel keys are plain keys. Shift+Enter is "new line without sending" in
	// chat apps and must not commit.
	if e.Shift || e.Alt || e.Win {
		return Decision{Action: ActionDismissPass}, true
	}
	switch e.VK {
	case VKEscape:
		return Decision{Action: ActionDismiss, Swallow: true}, true
	case VKUp:
		return Decision{Action: ActionMoveUp, Swallow: true}, true
	case VKDown:
		return Decision{Action: ActionMoveDown, Swallow: true}, true
	case VKReturn:
		// With no translation ready, Enter must go through: in a chat window
		// swallowing it would keep the user's message from being sent.
		if !l.Committable(l.Selected) {
			return Decision{Action: ActionDismissPass}, true
		}
		return Decision{Action: ActionCommit, Swallow: true}, true
	}
	return Decision{Action: ActionDismissPass}, true
}

func digitIndex(vk int) int {
	switch {
	case vk >= vk1 && vk <= vk6:
		return vk - vk1
	case vk >= vkNum1 && vk <= vkNum6:
		return vk - vkNum1
	}
	return -1
}

// ---- layout ----

// Panel layout, in DIPs. The size depends on the source text, never on the
// translations, so rows do not jump as they arrive.
const (
	PanelWidth        = 420
	HorizontalPadding = 10
	VerticalPadding   = 6
	RowSpacing        = 1
	RowVerticalPad    = 5
	LineHeight        = 18
	CopyNoticeHeight  = 25
	NormalMaxLines    = 2
	SelectedMaxLines  = 4

	charsPerLine = 42
	cjkExpansion = 2.6
	latExpansion = 1.2
)

// ReservedLines estimates from the source how many text lines each row needs
// (1 to 4); Chinese turns into many more characters in Latin script.
func ReservedLines(source string) int {
	n := len([]rune(source))
	if n == 0 {
		return 1
	}
	f := latExpansion
	if containsCJK(source) {
		f = cjkExpansion
	}
	est := float64(n) * f / charsPerLine
	lines := int(est)
	if float64(lines) < est {
		lines++ // round up
	}
	return max(1, min(SelectedMaxLines, lines))
}

func containsCJK(s string) bool {
	for _, r := range s {
		switch {
		case r >= 0x3040 && r <= 0x30FF, r >= 0x3400 && r <= 0x4DBF, r >= 0x4E00 && r <= 0x9FFF,
			r >= 0xAC00 && r <= 0xD7AF, r >= 0xF900 && r <= 0xFAFF:
			return true
		}
	}
	return false
}

// LinesFor is how many text lines a row shows: the highlighted one may grow.
func LinesFor(reserved int, selected bool) int {
	m := NormalMaxLines
	if selected {
		m = SelectedMaxLines
	}
	return max(1, min(m, reserved))
}

// RowHeight is the height of a row.
func RowHeight(reserved int, selected bool) int {
	return LinesFor(reserved, selected)*LineHeight + RowVerticalPad*2
}

// Geometry is the panel's size and where each row sits inside it.
type Geometry struct {
	Width, Height int
	Tops, Heights []int // per row, relative to the top of the panel
}

// Layout computes the geometry of a list. copyNotice adds the line that says
// the translation will be copied instead of replacing the text.
func Layout(rows, selected, reserved int, copyNotice bool) Geometry {
	g := Geometry{Width: PanelWidth}
	if rows == 0 {
		g.Height = LineHeight + VerticalPadding*2
		return g
	}
	y := VerticalPadding
	for i := 0; i < rows; i++ {
		h := RowHeight(reserved, i == selected)
		g.Tops = append(g.Tops, y)
		g.Heights = append(g.Heights, h)
		y += h + RowSpacing
	}
	y += VerticalPadding - RowSpacing
	if copyNotice {
		y += CopyNoticeHeight
	}
	g.Height = y
	return g
}

// RowAt returns the row at y (relative to the panel's top), or -1.
func (g Geometry) RowAt(y int) int {
	for i := range g.Tops {
		if y >= g.Tops[i] && y < g.Tops[i]+g.Heights[i] {
			return i
		}
	}
	return -1
}

// ---- placement ----

// Rect is a rectangle in screen pixels, origin at the top left.
type Rect struct{ X, Y, W, H int }

// Right and Bottom are the far edges.
func (r Rect) Right() int  { return r.X + r.W }
func (r Rect) Bottom() int { return r.Y + r.H }

// Placement gaps in pixels.
const (
	AnchorGap    = 6
	ScreenMargin = 8
)

// PanelOrigin puts a w×h panel next to anchor inside work: below the text if
// there is room, else above, then clamped into the work area.
func PanelOrigin(anchor Rect, w, h int, work Rect) (x, y int) {
	y = anchor.Bottom() + AnchorGap
	if y+h > work.Bottom()-ScreenMargin {
		if above := anchor.Y - AnchorGap - h; above >= work.Y+ScreenMargin {
			y = above
		}
	}
	x = anchor.X
	if x+w > work.Right()-ScreenMargin {
		x = work.Right() - ScreenMargin - w
	}
	x = clamp(x, work.X+ScreenMargin, max(work.X+ScreenMargin, work.Right()-ScreenMargin-w))
	y = clamp(y, work.Y+ScreenMargin, max(work.Y+ScreenMargin, work.Bottom()-ScreenMargin-h))
	return x, y
}

func clamp(v, lo, hi int) int { return min(max(v, lo), hi) }

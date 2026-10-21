//go:build windows

package main

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"

	"tolk/internal/inputassist"
	"tolk/internal/winsel"
)

// 输入助手 on Windows: select text in any app, press the shortcut, a panel
// next to the selection shows the translation into each target language, and
// choosing one pastes it over the selection. Everything runs in this process.
//
// The panel is a window of the app that is never activated, so the text box
// the user is typing in keeps the keyboard and its selection. Because of that
// the keys (↑ ↓ Enter Esc, Ctrl+1…6) and the clicks on the panel are seen by
// low level input hooks that exist only while the panel is open.

const panelTitle = "Tolk 候选面板"

// iaSession is one open panel: what was selected, the rows and the layout.
type iaSession struct {
	id     int
	fg     winsel.Foreground
	sel    winsel.Selection
	req    inputassist.Request
	cancel context.CancelFunc
	ctx    context.Context

	mu       sync.Mutex
	list     inputassist.List
	reserved int
	badge    bool

	renderMu sync.Mutex
	layout   atomic.Pointer[iaLayout]
	closing  atomic.Bool
}

// iaLayout is where the panel is and how it is divided, for hit testing.
type iaLayout struct {
	rect inputassist.Rect // screen pixels
	geom inputassist.Geometry
}

type iaState struct {
	mu      sync.Mutex // guards everything below, never held across a hook call
	cfg     inputassist.Config
	active  bool
	errText string
	accel   string // the shortcut registered now

	panelWin *mygo.Window
	panel    *winsel.Panel
	view     PanelView
	msgTimer *time.Timer
	kbd      *winsel.Hook
	mouse    *winsel.Hook
	nextID   int

	cur  atomic.Pointer[iaSession]
	busy atomic.Bool // a trigger is reading the selection, or a commit is running

	// Automatic display (the 实验 switch): the selection already handled, and
	// how the last mouse press went.
	lastFp    string
	pressAt   time.Time
	pressX    int
	pressY    int
	clicks    int
	autoTimer *time.Timer
}

var ia iaState

var iaTranslator = &inputassist.Translator{Backend: iaBackend{}, Cache: inputassist.NewCache(inputassist.DefaultCachePath())}

// iaBackend lets the translator use Tolk's engine.
type iaBackend struct{}

func (iaBackend) Engine() inputassist.EngineInfo {
	fp, title := currentEngineIdentity()
	return inputassist.EngineInfo{Fingerprint: fp, Title: title}
}

func (iaBackend) Translate(text, source, target string) (string, string, error) {
	before, _ := currentEngineIdentity()
	r, err := translate(text, source, target)
	after, _ := currentEngineIdentity()
	return r.Text, stableFingerprint(before, after), err
}

// ---- setup ----

func createInputAssistPanel() {
	ia.panelWin = mygo.NewWindow(mygo.WindowOptions{
		Title:           panelTitle,
		URL:             "/panel.html",
		Width:           inputassist.PanelWidth,
		Height:          60,
		Hidden:          true,
		Frameless:       true,
		Panel:           true,
		DisableMaximize: true,
		BackgroundColor: "light-dark(#fafafa, #232323)",
	})
}

func startInputAssist() {
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { iaTranslator.Cache.Flush() })
	go pushInputAssist()
}

func inputAssistClipboardBusy() bool { return winsel.Busy() }

func panelSnapshot() PanelView {
	ia.mu.Lock()
	defer ia.mu.Unlock()
	if ia.view.Mode == "" {
		return PanelView{Mode: "none"}
	}
	return ia.view
}

func inputAssistStatus() (bool, string) {
	ia.mu.Lock()
	defer ia.mu.Unlock()
	return ia.active, ia.errText
}

func triggerInputAssist() error {
	ia.mu.Lock()
	enabled := ia.cfg.Enabled
	ia.mu.Unlock()
	if !enabled {
		return fmt.Errorf("输入助手未开启")
	}
	go ia.trigger()
	return nil
}

// pushInputAssist applies the saved settings: registers (or drops) the
// shortcut and the automatic display, and reports whether it all came up.
func pushInputAssist() {
	cfg := loadInputAssist()
	ia.mu.Lock()
	ia.cfg = cfg
	want := ""
	var accErr error
	if cfg.Enabled {
		want, accErr = inputassist.WindowsShortcut(cfg.ShortcutKeyCode, cfg.ShortcutModifiers)
	}
	old := ia.accel
	ia.mu.Unlock()

	if old != "" && old != want {
		mygo.GlobalShortcut.Unregister(old)
		ia.mu.Lock()
		ia.accel = ""
		ia.mu.Unlock()
		old = ""
	}
	var regErr string
	switch {
	case !cfg.Enabled:
	case accErr != nil:
		regErr = accErr.Error()
	case old == "":
		if err := mygo.GlobalShortcut.Register(want, func() { go ia.trigger() }); err != nil {
			regErr = fmt.Sprintf("快捷键 %s 注册失败，可能被其它应用占用", want)
			Logs.Warnf("输入助手：%s（%v）", regErr, err)
		} else {
			ia.mu.Lock()
			ia.accel = want
			ia.mu.Unlock()
		}
	}
	ia.mu.Lock()
	ia.active = cfg.Enabled && ia.accel != ""
	ia.errText = regErr
	ia.mu.Unlock()

	if !cfg.Enabled {
		if s := ia.cur.Load(); s != nil {
			ia.dismiss(s)
		}
		ia.hideMessage()
	}
	ia.syncMouseHook()
}

// ---- hooks ----

// syncMouseHook keeps the mouse hook installed exactly while it is needed: for
// the automatic display, or while a panel is open.
func (st *iaState) syncMouseHook() {
	st.mu.Lock()
	need := (st.cfg.Enabled && st.cfg.AutoShow) || st.cur.Load() != nil
	have := st.mouse != nil
	var drop *winsel.Hook
	if !need && have {
		drop, st.mouse = st.mouse, nil
	}
	st.mu.Unlock()
	if drop != nil {
		drop.Remove()
	}
	if need && !have {
		h, err := winsel.InstallMouse(iaMouse)
		if err != nil {
			Logs.Warnf("输入助手：%v", err)
			return
		}
		st.mu.Lock()
		if st.mouse == nil {
			st.mouse = h
			h = nil
		}
		st.mu.Unlock()
		if h != nil {
			h.Remove()
		}
	}
}

// ---- triggering ----

func (st *iaState) config() inputassist.Config {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.cfg
}

func (st *iaState) allowed(app *inputassist.App, cfg inputassist.Config) bool {
	return inputassist.AppAllowed(app, cfg.Scope, inputassist.ParseList(cfg.Blocklist), inputassist.ParseList(cfg.Allowlist))
}

// trigger is the shortcut: read the selection of the app in front and open the
// panel for it.
func (st *iaState) trigger() {
	cfg := st.config()
	if !cfg.Enabled || !st.busy.CompareAndSwap(false, true) {
		return
	}
	defer st.busy.Store(false)
	winsel.SuppressMenuActivation()

	fg, ok := winsel.GetForeground()
	if !ok {
		st.showMessage("请先选择要翻译的文字", "warning", winsel.CursorRect())
		return
	}
	if !st.allowed(&fg.App, cfg) {
		Logs.Infof("选区翻译已跳过当前应用（按「应用范围」设置）：%s", fg.App.Exe)
		st.showMessage("当前应用在选区翻译的排除列表中", "info", winsel.CursorRect())
		return
	}
	// Pressing the shortcut again is taking the selection again.
	if s := st.cur.Load(); s != nil {
		st.dismiss(s)
	}
	sel, err := winsel.Capture(fg, true)
	if err != nil {
		st.captureFailed(fg, sel, err)
		return
	}
	if !inputassist.LooksTranslatable(sel.Text) {
		st.showMessage("请先选择要翻译的文字", "warning", sel.Rect)
		return
	}
	st.start(fg, sel)
}

func (st *iaState) captureFailed(fg winsel.Foreground, sel winsel.Selection, err error) {
	anchor := winsel.CursorRect()
	switch {
	case err == winsel.ErrNoSelection:
		st.showMessage("请先选择要翻译的文字", "warning", anchor)
	case err == winsel.ErrPassword:
		st.showMessage("密码框里的内容不会被读取", "info", anchor)
	case err == winsel.ErrElevated:
		Logs.Warnf("输入助手：%s（%s）", err, fg.App.Exe)
		st.showMessage(err.Error(), "warning", anchor)
	case err == winsel.ErrTooLong:
		st.showMessage(fmt.Sprintf("选中的文字太长（最多 %d 字）", winsel.MaxChars), "warning", anchor)
	default:
		Logs.Warnf("输入助手：读取选中文字失败（%s）：%v", fg.App.Exe, err)
		st.showMessage("无法读取选中的文字："+err.Error(), "warning", anchor)
	}
}

// start opens the panel for a selection.
func (st *iaState) start(fg winsel.Foreground, sel winsel.Selection) {
	cfg := st.config()
	source := inputassist.SelectionSourceLanguage(inputassist.ScanScripts(sel.Text), inputassist.Detect(sel.Text))
	var codes []string
	for _, l := range cfg.Targets {
		if c, ok := inputassist.CodeOf(l); ok && c != inputassist.AutoCode {
			codes = append(codes, c)
		}
	}
	targets := inputassist.VisibleTargets(inputassist.SanitizeCodes(codes), source)
	if len(targets) == 0 {
		st.showMessage("选中文本已是配置的目标语言", "info", sel.Rect)
		return
	}
	if source == "" {
		source = inputassist.AutoCode
	}
	st.hideMessage()

	ctx, cancel := context.WithCancel(context.Background())
	st.mu.Lock()
	st.nextID++
	s := &iaSession{
		id: st.nextID, fg: fg, sel: sel, ctx: ctx, cancel: cancel,
		req:      inputassist.Request{Text: sel.Text, Source: source, Codes: targets},
		list:     inputassist.NewList(targets),
		reserved: inputassist.ReservedLines(sel.Text),
		badge:    cfg.CacheBadge,
	}
	st.lastFp = iaFingerprint(sel)
	st.mu.Unlock()

	if !st.openPanel(s) {
		cancel()
		return
	}
	go st.watchForeground(s)
	iaTranslator.Start(ctx, s.req, func(code string, state inputassist.RowState) {
		s.mu.Lock()
		changed := s.list.Update(code, state)
		s.mu.Unlock()
		if changed {
			st.render(s)
		}
	})
}

func iaFingerprint(sel winsel.Selection) string {
	return fmt.Sprintf("%d|%d|%s", sel.PID, sel.Window, sel.Text)
}

// openPanel makes s the current session, shows its panel and starts watching
// the keyboard and the mouse. false when the keys cannot be taken over (then
// the panel would be a window nobody could answer).
func (st *iaState) openPanel(s *iaSession) bool {
	if !st.ensurePanel() {
		return false
	}
	kbd, err := winsel.InstallKeyboard(st.onKey)
	if err != nil {
		Logs.Errorf("输入助手：无法接管候选面板的按键，已取消本次操作：%v", err)
		return false
	}
	st.mu.Lock()
	st.kbd = kbd
	st.mu.Unlock()
	st.cur.Store(s)
	st.syncMouseHook()
	st.render(s)
	return true
}

func (st *iaState) ensurePanel() bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.panel != nil {
		return true
	}
	if st.panelWin == nil {
		return false
	}
	var err error
	for i := 0; i < 20; i++ { // the window exists from the start; be patient anyway
		if st.panel, err = winsel.FindPanel(panelTitle); err == nil {
			return true
		}
		st.mu.Unlock()
		time.Sleep(50 * time.Millisecond)
		st.mu.Lock()
		if st.panel != nil {
			return true
		}
	}
	Logs.Errorf("输入助手：%v", err)
	return false
}

// render draws the session's rows and puts the panel in place. The size only
// changes when the highlight moves (the highlighted row can grow); translations
// arriving never move or resize it.
func (st *iaState) render(s *iaSession) {
	s.renderMu.Lock()
	defer s.renderMu.Unlock()
	if s.closing.Load() || st.cur.Load() != s {
		return
	}
	s.mu.Lock()
	rows := append([]inputassist.Row(nil), s.list.Rows...)
	selected := s.list.Selected
	s.mu.Unlock()
	g := inputassist.Layout(len(rows), selected, s.reserved, false)
	v := PanelView{
		Mode: "list", Rows: rows, Selected: selected, Tops: g.Tops, Heights: g.Heights,
		Width: g.Width, Height: g.Height, Lines: s.reserved, Badge: s.badge,
	}
	st.mu.Lock()
	st.view = v
	panel := st.panel
	st.mu.Unlock()
	if panel == nil {
		return
	}
	_ = PanelUpdate.Emit(ia.panelWin, v)
	old := s.layout.Load()
	if old == nil || old.geom.Height != g.Height {
		rect := panel.PlaceNear(s.sel.Rect, g.Width, g.Height)
		s.layout.Store(&iaLayout{rect: rect, geom: g})
		return
	}
	s.layout.Store(&iaLayout{rect: old.rect, geom: g})
}

// dismiss closes the session's panel and everything that was watching for it.
func (st *iaState) dismiss(s *iaSession) {
	if !st.cur.CompareAndSwap(s, nil) {
		return
	}
	s.closing.Store(true)
	s.cancel()
	st.mu.Lock()
	kbd := st.kbd
	st.kbd = nil
	panel := st.panel
	st.view = PanelView{Mode: "none"}
	st.mu.Unlock()
	if kbd != nil {
		kbd.Remove()
	}
	_ = PanelUpdate.Emit(st.panelWin, PanelView{Mode: "none"})
	if panel != nil {
		panel.Hide()
	}
	st.syncMouseHook()
}

// watchForeground closes the panel when the user goes to another window.
func (st *iaState) watchForeground(s *iaSession) {
	for !s.closing.Load() {
		time.Sleep(150 * time.Millisecond)
		cur, ok := winsel.GetForeground()
		if !ok || cur.Window != s.fg.Window {
			st.dismiss(s)
			return
		}
	}
}

// ---- keyboard and mouse ----

// onKey runs inside the keyboard hook: it decides and hands the work off.
func (st *iaState) onKey(vk int, m winsel.Mods) bool {
	s := st.cur.Load()
	if s == nil || s.closing.Load() {
		return false
	}
	e := inputassist.KeyEvent{VK: vk, Ctrl: m.Ctrl, Alt: m.Alt, Shift: m.Shift, Win: m.Win}
	s.mu.Lock()
	d, ok := inputassist.Route(e, &s.list)
	if ok {
		switch d.Action {
		case inputassist.ActionMoveUp:
			s.list.Move(-1)
		case inputassist.ActionMoveDown:
			s.list.Move(1)
		case inputassist.ActionCommit:
			d.Index = s.list.Selected
		case inputassist.ActionCopy:
			d.Index = s.list.Selected
		}
	}
	s.mu.Unlock()
	if !ok {
		return false
	}
	switch d.Action {
	case inputassist.ActionMoveUp, inputassist.ActionMoveDown:
		go st.render(s)
	default:
		// From here the panel is going away: later keys are the app's.
		s.closing.Store(true)
		go st.act(s, d)
	}
	return d.Swallow
}

func (st *iaState) act(s *iaSession, d inputassist.Decision) {
	// closing was set by the hook to stop interception; dismiss needs the
	// session still current, which it is.
	switch d.Action {
	case inputassist.ActionCommit, inputassist.ActionCommitIndex:
		st.commit(s, d.Index)
	case inputassist.ActionCopy:
		st.copyRow(s, d.Index)
	default:
		st.dismiss(s)
	}
}

// onMouse runs inside the mouse hook.
func iaMouse(button int, down bool, x, y int) bool {
	st := &ia
	s := st.cur.Load()
	if s != nil && !s.closing.Load() {
		if !down {
			return false
		}
		l := s.layout.Load()
		if l != nil && x >= l.rect.X && x < l.rect.Right() && y >= l.rect.Y && y < l.rect.Bottom() {
			if button == winsel.ButtonLeft {
				scale := float64(l.rect.W) / float64(l.geom.Width)
				row := l.geom.RowAt(int(float64(y-l.rect.Y) / scale))
				go st.clickRow(s, row)
			}
			return true // the page below must not get a click it did not mean
		}
		go st.dismiss(s)
		return false
	}
	st.autoMouse(button, down, x, y)
	return false
}

func (st *iaState) clickRow(s *iaSession, row int) {
	if row < 0 || s.closing.Load() {
		return
	}
	s.mu.Lock()
	if row >= len(s.list.Rows) {
		s.mu.Unlock()
		return
	}
	r := s.list.Rows[row]
	committable := r.Committable()
	s.mu.Unlock()
	switch {
	case committable:
		s.closing.Store(true)
		st.commit(s, row)
	case r.State.Kind == inputassist.RowFailed:
		iaTranslator.Retry(s.ctx, s.req, r.Code, func(code string, state inputassist.RowState) {
			s.mu.Lock()
			changed := s.list.Update(code, state)
			s.mu.Unlock()
			if changed {
				st.render(s)
			}
		})
	}
}

// ---- finishing ----

// commit replaces the selection with row i's translation, or copies it when
// the replacement cannot be done safely.
func (st *iaState) commit(s *iaSession, i int) {
	s.mu.Lock()
	text := s.list.Text(i)
	s.mu.Unlock()
	if text == "" {
		st.dismiss(s)
		return
	}
	if !st.busy.CompareAndSwap(false, true) {
		st.dismiss(s)
		return
	}
	defer st.busy.Store(false)
	anchor := s.sel.Rect
	st.dismiss(s)

	err := winsel.Replace(s.sel, text)
	switch err {
	case nil:
		Logs.Infof("选区翻译已替换原文（%s）", s.sel.Via)
	case winsel.ErrClipboardBusy:
		st.showMessage("剪贴板正被其它应用使用，本次未替换，请稍后重试", "warning", anchor)
	default:
		Logs.Warnf("选区翻译无法替换原文：%v", err)
		if cerr := winsel.SetClipboardText(text); cerr != nil {
			st.showMessage("无法写入剪贴板："+cerr.Error(), "warning", anchor)
			return
		}
		st.showMessage("无法安全替换，译文已复制", "copied", anchor)
	}
}

func (st *iaState) copyRow(s *iaSession, i int) {
	s.mu.Lock()
	text := s.list.Text(i)
	s.mu.Unlock()
	anchor := s.sel.Rect
	st.dismiss(s)
	if text == "" {
		return
	}
	if err := winsel.SetClipboardText(text); err != nil {
		st.showMessage("无法写入剪贴板："+err.Error(), "warning", anchor)
		return
	}
	st.showMessage("已复制译文", "success", anchor)
}

// ---- short messages ----

func (st *iaState) hideMessage() {
	st.mu.Lock()
	if st.msgTimer != nil {
		st.msgTimer.Stop()
		st.msgTimer = nil
	}
	showing := st.view.Mode == "message"
	panel := st.panel
	if showing {
		st.view = PanelView{Mode: "none"}
	}
	st.mu.Unlock()
	if showing && panel != nil {
		_ = PanelUpdate.Emit(st.panelWin, PanelView{Mode: "none"})
		panel.Hide()
	}
}

// showMessage tells the user something in a small bubble by the selection,
// which goes away by itself.
func (st *iaState) showMessage(text, style string, anchor inputassist.Rect) {
	if s := st.cur.Load(); s != nil { // a message never covers a panel with choices
		return
	}
	if !st.ensurePanel() {
		return
	}
	width := min(420, max(220, len([]rune(text))*13+56))
	v := PanelView{Mode: "message", Message: text, Style: style, Width: width, Height: 38}
	st.mu.Lock()
	if st.msgTimer != nil {
		st.msgTimer.Stop()
	}
	st.view = v
	panel := st.panel
	st.mu.Unlock()
	_ = PanelUpdate.Emit(st.panelWin, v)
	panel.PlaceNear(anchor, width, 38)
	timer := time.AfterFunc(2400*time.Millisecond, st.hideMessage)
	st.mu.Lock()
	st.msgTimer = timer
	st.mu.Unlock()
}

// ---- automatic display (实验) ----

// autoMouse watches the left button for the end of a selection: a drag, or a
// double or triple click. Plain clicks only count when they are the first
// after the user came back to a selection already handled.
func (st *iaState) autoMouse(button int, down bool, x, y int) {
	if button != winsel.ButtonLeft {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if !st.cfg.Enabled || !st.cfg.AutoShow {
		return
	}
	now := time.Now()
	if down {
		interval := time.Duration(winsel.DoubleClickTime()) * time.Millisecond
		if now.Sub(st.pressAt) <= interval && abs(x-st.pressX) <= 4 && abs(y-st.pressY) <= 4 {
			st.clicks++
		} else {
			st.clicks = 1
		}
		st.pressAt, st.pressX, st.pressY = now, x, y
		if st.autoTimer != nil { // a new press: whatever was about to be read is stale
			st.autoTimer.Stop()
			st.autoTimer = nil
		}
		if st.clicks >= 2 {
			st.lastFp = "" // selecting again, so the same text may show again
		}
		return
	}
	if abs(x-st.pressX) > 4 || abs(y-st.pressY) > 4 {
		st.lastFp = "" // a drag: the selection really changed
	}
	if st.autoTimer != nil {
		st.autoTimer.Stop()
	}
	st.autoTimer = time.AfterFunc(180*time.Millisecond, st.autoCapture)
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// autoCapture reads the selection once the user has finished selecting. It
// only asks UI Automation: it never presses keys on its own.
func (st *iaState) autoCapture() {
	cfg := st.config()
	if !cfg.Enabled || !cfg.AutoShow || st.cur.Load() != nil || st.busy.Load() {
		return
	}
	fg, ok := winsel.GetForeground()
	if !ok || !st.allowed(&fg.App, cfg) {
		return
	}
	sel, err := winsel.Capture(fg, false)
	if err != nil {
		st.mu.Lock()
		st.lastFp = ""
		st.mu.Unlock()
		return
	}
	if !inputassist.LooksTranslatable(sel.Text) {
		return
	}
	fp := iaFingerprint(sel)
	st.mu.Lock()
	same := st.lastFp == fp
	st.mu.Unlock()
	if same || !st.busy.CompareAndSwap(false, true) {
		return
	}
	defer st.busy.Store(false)
	st.start(fg, sel)
}

package main

import (
	_ "embed"
	"log"
	"runtime"
	"sync"

	"github.com/egoist/mygo"
)

// trayIconColor is the 32 px picture of design/logo.ico, for the Windows
// notification area (which draws nothing for an icon without a picture).
//
//go:embed design/tray.png
var trayIconColor []byte

// trayIconTemplate is the black-on-transparent menu bar icon that macOS
// tints to match the menu bar (design/tray-template.svg).
//
//go:embed design/tray-template.png
var trayIconTemplate []byte

// MenuCommand asks the main window to open a part of its UI: "settings" or
// "update" (settings → 关于, then check for updates).
var MenuCommand = mygo.NewEvent[string]("menuCommand")

// InputAssistChanged tells an open settings page that the menu switched
// 输入助手 on or off, so saving the page does not switch it back.
var InputAssistChanged = mygo.NewEvent[bool]("inputAssistChanged")

var trayState struct {
	mu   sync.Mutex
	tray *mygo.Tray
	win  *mygo.Window
	show func()
}

// setupTray puts Tolk in the notification area (Windows) or the menu bar
// (macOS) with the same menu as the old apps had, and gives the Dock icon
// (macOS) the same shortcuts.
func setupTray(win *mygo.Window, showMain func()) {
	trayState.mu.Lock()
	trayState.win, trayState.show = win, showMain
	trayState.mu.Unlock()

	opts := mygo.TrayOptions{ToolTip: "Tolk", Menu: trayMenu(true)}
	if runtime.GOOS == "darwin" {
		opts.Icon, opts.IconIsTemplate = trayIconTemplate, true
	} else {
		opts.Icon = trayIconColor
	}
	tray, err := mygo.NewTray(opts)
	if err != nil {
		log.Println("tray:", err)
	} else if runtime.GOOS == "windows" {
		// A left click brings the window back; the right click opens the menu.
		tray.OnClick(showMain)
	}
	trayState.mu.Lock()
	trayState.tray = tray
	trayState.mu.Unlock()
	if runtime.GOOS == "darwin" {
		mygo.App.Dock.SetMenu(trayMenu(false)) // 退出 is already in the Dock's own items
	}
}

var refreshMu sync.Mutex

// refreshTray redraws the check marks after a switch changed somewhere else
// (the settings page, the main window's Mini button).
func refreshTray() {
	// One refresh at a time, so an older snapshot never lands after a newer one.
	refreshMu.Lock()
	defer refreshMu.Unlock()
	trayState.mu.Lock()
	tray := trayState.tray
	trayState.mu.Unlock()
	if tray == nil {
		return
	}
	tray.SetMenu(trayMenu(true))
	if runtime.GOOS == "darwin" {
		mygo.App.Dock.SetMenu(trayMenu(false))
	}
}

func trayMenu(withQuit bool) *mygo.Menu {
	trayState.mu.Lock()
	win, showMain := trayState.win, trayState.show
	trayState.mu.Unlock()
	command := func(c string) func(*mygo.MenuItem, *mygo.Window) {
		return func(*mygo.MenuItem, *mygo.Window) {
			showMain()
			_ = MenuCommand.Emit(win, c)
		}
	}
	ia := loadInputAssist()
	items := []*mygo.MenuItem{
		{Label: "显示主窗口", Click: func(*mygo.MenuItem, *mygo.Window) { showMain() }},
		mygo.Separator(),
		{Label: "Mini 模式", Type: mygo.MenuItemCheckbox, Checked: currentPrefs().MiniMode, Click: func(it *mygo.MenuItem, _ *mygo.Window) {
			on := it.Checked
			go func() {
				if err := (Tolk{}).SetMini(on); err != nil {
					Logs.Errorf("Mini 模式切换失败：%v", err)
				}
				_ = PrefsChanged.Broadcast(currentPrefs())
				refreshTray()
			}()
		}},
		{Label: "输入助手", Type: mygo.MenuItemCheckbox, Checked: ia.Enabled, Click: func(it *mygo.MenuItem, _ *mygo.Window) {
			on := it.Checked
			go func() {
				if err := setInputAssistEnabled(on); err != nil {
					Logs.Errorf("输入助手切换失败：%v", err)
					refreshTray()
					return
				}
				_ = InputAssistChanged.Broadcast(on)
			}()
		}},
	}
	if runtime.GOOS == "darwin" {
		items = append(items, &mygo.MenuItem{Label: "截图翻译", Click: func(*mygo.MenuItem, *mygo.Window) {
			go func() {
				if err := macScreenshot("translate"); err != nil {
					Logs.Errorf("截图翻译：%v", err)
				}
			}()
		}})
	}
	items = append(items,
		mygo.Separator(),
		&mygo.MenuItem{Label: "设置…", Click: command("settings")},
		&mygo.MenuItem{Label: "检查更新…", Click: command("update")},
	)
	if withQuit {
		items = append(items, mygo.Separator(),
			&mygo.MenuItem{Label: "退出 Tolk", Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() }})
	}
	return mygo.NewMenu(items)
}

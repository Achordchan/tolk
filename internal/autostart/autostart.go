// Package autostart manages launching Tolk at login.
//
// On Windows it keeps using dazuo-win's Startup-folder shortcut
// (大佐翻译官.lnk), so a user who already enabled it keeps it, and switching it
// in Tolk changes the same entry. macOS uses the app's own login item.
package autostart

import "errors"

// ErrUnsupported is returned where there is no way to start at login.
var ErrUnsupported = errors.New("这个系统暂不支持开机自启")

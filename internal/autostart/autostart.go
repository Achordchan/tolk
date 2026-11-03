// Package autostart manages launching Tolk at login.
//
// On Windows it is a shortcut in the Startup folder (Tolk.lnk); macOS uses the
// app's own login item.
package autostart

import "errors"

// ErrUnsupported is returned where there is no way to start at login.
var ErrUnsupported = errors.New("这个系统暂不支持开机自启")

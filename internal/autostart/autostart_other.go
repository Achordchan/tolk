//go:build !windows

package autostart

import "github.com/egoist/mygo"

// Enabled reports whether the app is a login item.
func Enabled() bool { return mygo.App.OpenAtLogin() }

// Set adds or removes the login item.
func Set(on bool) error { return mygo.App.SetOpenAtLogin(on) }

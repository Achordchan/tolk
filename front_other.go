//go:build !windows

package main

import "github.com/egoist/mygo"

// bringToFront shows win and focuses it. On macOS Focus is
// makeKeyAndOrderFront, which also brings a minimized window back.
func bringToFront(win *mygo.Window) { win.Show(); win.Focus() }

func allowForegroundHandoff() {}

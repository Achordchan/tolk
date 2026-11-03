//go:build !darwin && !windows

package main

import "github.com/egoist/mygo"

func nativeTitleBar(*mygo.Window, bool, uint32, uint32) {}

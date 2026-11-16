//go:build !windows

package main

// watchCopyKeys is Windows only; macOS has the helper's ⌘C hotkey.
func watchCopyKeys(func(string)) bool { return false }

func waitWhileKeysCover() bool { return false }

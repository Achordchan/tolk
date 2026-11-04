//go:build !windows

package main

import "fmt"

func (Tolk) ScreenshotWindows() error { return fmt.Errorf("此截图入口仅支持 Windows") }
func setupScreenshotShortcut()        {}

func (Tolk) SetWindowsScreenshotHotkey(bool) error {
	return fmt.Errorf("此快捷键仅支持 Windows")
}

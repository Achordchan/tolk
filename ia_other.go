//go:build !darwin && !windows

package main

import "errors"

func pushInputAssist() {}

func inputAssistStatus() (bool, string) { return false, "" }

func triggerInputAssist() error { return errors.New("输入助手只在 Windows 和 macOS 上可用") }

func createInputAssistPanel() {}

func startInputAssist() {}

func panelSnapshot() PanelView { return PanelView{Mode: "none"} }

func inputAssistClipboardBusy() bool { return false }

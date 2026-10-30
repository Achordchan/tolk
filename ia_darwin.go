//go:build darwin

package main

// On the Mac 输入助手 lives in the native helper (macos/helper); these are the
// parts the shared code asks of every platform.

func pushInputAssist() { macPushInputAssist() }

func inputAssistStatus() (bool, string) { return macInputAssistStatus() }

func triggerInputAssist() error { return macSimple("inputassist.trigger", nil) }

func createInputAssistPanel() {}

func startInputAssist() {}

func panelSnapshot() PanelView { return PanelView{Mode: "none"} }

func inputAssistClipboardBusy() bool           { return false }
func inputAssistClipboardTransactions() uint64 { return 0 }

// watchMiniClicks has the helper report mouse presses while the Mini bubble
// is up, so a click outside closes it.
func watchMiniClicks(on bool) { macWatchMouse(on) }

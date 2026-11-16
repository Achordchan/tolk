//go:build !darwin

package main

import "errors"

var errNoHelper = errors.New("原生助手只在 macOS 上可用")

func helperVersion() string { return "" }

func startHelper(func(string)) {}

func hotkeyActive() bool { return false }

func macStatus() MacStatus { return MacStatus{} }

func macSimple(string, map[string]any) error { return errNoHelper }

func macScreenshot(string) error { return errNoHelper }

func macApplyHotkeys() {}

func macApplePacks([]string) (map[string]string, error) { return nil, errNoHelper }

func macAppleDownload(string) error { return errNoHelper }

// startReopen: reopening after a quit (for a macOS permission) is macOS only;
// nothing asks for it elsewhere.
func startReopen(string) error { return errors.New("这个系统不需要重新打开 Tolk") }

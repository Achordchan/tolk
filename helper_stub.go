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

func macImport(bool) ([]string, error) { return nil, errNoHelper }

func macWatchMouse(bool) {}

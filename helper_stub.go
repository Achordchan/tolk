//go:build !darwin

package main

// helperVersion is empty where there is no native helper.
func helperVersion() string { return "" }

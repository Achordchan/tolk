//go:build !windows

package update

// installLocation: only the Windows installer records one.
func installLocation() string { return "" }

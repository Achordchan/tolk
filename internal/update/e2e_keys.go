//go:build e2e

package update

import "os"

// The e2e build (cmd/selfupdate-e2e) trusts a throwaway key made for the
// test; release builds never include this file.
func init() {
	if k := os.Getenv("TOLK_E2E_WINDOWS_KEY"); k != "" {
		windowsKey = k
	}
	if k := os.Getenv("TOLK_E2E_SPARKLE_KEY"); k != "" {
		sparkleKey = k
	}
}

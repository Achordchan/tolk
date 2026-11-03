//go:build !darwin && !windows

package report

import (
	"os/exec"
	"strings"
)

// OSVersion is the kernel release.
func OSVersion() string {
	out, _ := exec.Command("uname", "-sr").Output()
	return strings.TrimSpace(string(out))
}

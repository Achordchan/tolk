package report

import (
	"os/exec"
	"strings"
)

// OSVersion is the macOS version, such as "15.1 (24B83)".
func OSVersion() string {
	v, _ := exec.Command("sw_vers", "-productVersion").Output()
	b, _ := exec.Command("sw_vers", "-buildVersion").Output()
	s := strings.TrimSpace(string(v))
	if bs := strings.TrimSpace(string(b)); bs != "" {
		s += " (" + bs + ")"
	}
	return s
}

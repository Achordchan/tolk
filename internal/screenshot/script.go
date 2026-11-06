package screenshot

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"os"
	"path/filepath"
)

//go:embed controls.ps1
var controlsScript string

//go:embed capture.ps1
var captureScript string

//go:embed layout.ps1
var layoutScript string

//go:embed workspace.ps1
var workspaceScript string

// Script is the whole workspace as one Windows PowerShell file. The UTF-8 BOM
// is what makes Windows PowerShell 5.1 read the Chinese literals correctly.
func Script() string {
	return "\xef\xbb\xbf" + controlsScript + "\n" + captureScript + "\n" + layoutScript + "\n" + workspaceScript
}

// ControlsDLL is the file the compiled controls are kept in, inside dir. Its
// name carries a hash of controls.ps1, so a new version compiles once and
// never loads a stale build.
func ControlsDLL(dir string) string {
	sum := sha256.Sum256([]byte(controlsScript))
	return filepath.Join(dir, "controls-"+hex.EncodeToString(sum[:8])+".dll")
}

// PrepareCache creates dir and removes everything in it but the current
// build: DLLs of older versions, half-copied files and the private folders
// a compile leaves behind. A file still loaded by a running workspace (a pin
// kept open) cannot be removed and is left for the next time.
func PrepareCache(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dll := ControlsDLL(dir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if e.Name() != filepath.Base(dll) {
			_ = os.RemoveAll(filepath.Join(dir, e.Name()))
		}
	}
	return dll, nil
}

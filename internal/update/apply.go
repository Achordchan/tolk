package update

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ExeName is the program inside a Windows update package (and of an install).
const ExeName = "Tolk.exe"

// WindowsPlan is a verified, extracted package ready for the swap script.
type WindowsPlan struct {
	SourceDir string
	ExeName   string
	Version   string
}

// PrepareWindows extracts a verified package and checks it (manifest version,
// main program). The executable
// inside is renamed to exeName, the name of the running program, so the swap
// never leaves two executables behind.
func PrepareWindows(zipPath, workDir, version, exeName string) (*WindowsPlan, error) {
	dest := filepath.Join(workDir, "full_update_"+sanitize(version))
	if err := os.RemoveAll(dest); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dest, 0o700); err != nil {
		return nil, err
	}
	if err := ExtractZip(zipPath, dest); err != nil {
		return nil, err
	}
	root := dest
	if _, err := os.Stat(filepath.Join(root, ExeName)); err != nil {
		entries, _ := os.ReadDir(dest)
		if len(entries) == 1 && entries[0].IsDir() {
			root = filepath.Join(dest, entries[0].Name())
		}
	}
	if _, err := os.Stat(filepath.Join(root, ExeName)); err != nil {
		return nil, errors.New("更新包内未找到主程序，请确认上传的是完整运行目录 zip")
	}
	b, err := os.ReadFile(filepath.Join(root, "update_manifest.json"))
	if err != nil {
		return nil, errors.New("更新包缺少版本清单 update_manifest.json")
	}
	var m struct {
		AppVersion  string `json:"app_version"`
		PackageType string `json:"package_type"`
	}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(string(b), "\xef\xbb\xbf")), &m); err != nil {
		return nil, errors.New("更新包版本清单损坏")
	}
	if CompareVersions(m.AppVersion, version) != 0 {
		return nil, fmt.Errorf("更新包版本不匹配：期望 v%s，实际 v%s", NormalizeVersion(version), NormalizeVersion(m.AppVersion))
	}
	if m.PackageType != "" && m.PackageType != "windows_full_update" {
		return nil, fmt.Errorf("更新包类型不受支持：%s", m.PackageType)
	}
	if exeName != "" && exeName != ExeName {
		if err := os.Rename(filepath.Join(root, ExeName), filepath.Join(root, exeName)); err != nil {
			return nil, err
		}
	} else {
		exeName = ExeName
	}
	return &WindowsPlan{SourceDir: root, ExeName: exeName, Version: NormalizeVersion(version)}, nil
}

func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "x"
	}
	return b.String()
}

// WindowsEnv is the environment the swap script reads.
func (p *WindowsPlan) Env(targetDir, backupDir, logPath, resultPath string, pid int, relaunch bool) []string {
	return []string{
		"TOLK_UPDATE_RELAUNCH=" + map[bool]string{true: "1", false: "0"}[relaunch],
		"TOLK_UPDATE_SOURCE_DIR=" + p.SourceDir,
		"TOLK_UPDATE_TARGET_DIR=" + targetDir,
		"TOLK_UPDATE_EXE_NAME=" + p.ExeName,
		fmt.Sprintf("TOLK_UPDATE_PROCESS_ID=%d", pid),
		"TOLK_UPDATE_BACKUP_DIR=" + backupDir,
		"TOLK_UPDATE_LOG_PATH=" + logPath,
		"TOLK_UPDATE_RESULT_PATH=" + resultPath,
		"TOLK_UPDATE_EXPECTED_VERSION=" + p.Version,
	}
}

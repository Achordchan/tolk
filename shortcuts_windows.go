//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// The online update from 大佐翻译官 swaps the files in place, so what the old
// installer left outside the install dir still says 大佐翻译官: the desktop
// and Start menu shortcuts (whose icon Windows keeps cached) and the entry in
// Apps & features. tidyLegacyShortcuts brings them over to Tolk. It only
// touches shortcuts that start this very program, and is safe to run on every
// launch: once everything is tidy it changes nothing.

// tidyShortcutsScript is read with environment variables, so no path is ever
// written into the script text.
const tidyShortcutsScript = `$ErrorActionPreference = 'SilentlyContinue'
$exe = [string]$env:TOLK_EXE
$shell = New-Object -ComObject WScript.Shell
$changed = 0
$dirs = @(
  @{ Path = [Environment]::GetFolderPath('Desktop'); Rename = $true; Recurse = $false },
  @{ Path = [Environment]::GetFolderPath('Programs'); Rename = $true; Recurse = $true },
  @{ Path = [Environment]::GetFolderPath('Startup'); Rename = $false; Recurse = $false },
  @{ Path = (Join-Path $env:APPDATA 'Microsoft\Internet Explorer\Quick Launch\User Pinned\TaskBar'); Rename = $false; Recurse = $false }
)
foreach ($d in $dirs) {
  if (-not $d.Path -or -not (Test-Path -LiteralPath $d.Path)) { continue }
  $files = if ($d.Recurse) { Get-ChildItem -LiteralPath $d.Path -Filter *.lnk -File -Recurse } else { Get-ChildItem -LiteralPath $d.Path -Filter *.lnk -File }
  foreach ($f in $files) {
    if ($d.Recurse -and $f.DirectoryName -eq [Environment]::GetFolderPath('Startup')) { continue }
    $s = $shell.CreateShortcut($f.FullName)
    if (-not $s.TargetPath -or [string]::Compare($s.TargetPath, $exe, $true) -ne 0) { continue }
    $path = $f.FullName
    if ($d.Rename -and $f.BaseName -eq '大佐翻译官') {
      $new = Join-Path $f.DirectoryName 'Tolk.lnk'
      if (Test-Path -LiteralPath $new) { Remove-Item -LiteralPath $path -Force; $changed++; continue }
      Rename-Item -LiteralPath $path -NewName 'Tolk.lnk'
      $path = $new
      $changed++
    }
    $s = $shell.CreateShortcut($path)
    if ($s.IconLocation -ne "$exe,0") {
      $s.IconLocation = "$exe,0"
      $s.Description = 'Tolk'
      $s.Save()
      $changed++
    }
  }
}
Write-Output $changed
`

// innoUninstallKey is the old installer's entry; Tolk's installer keeps the
// same AppId.
const innoUninstallKey = `Software\Microsoft\Windows\CurrentVersion\Uninstall\{5E76B529-0B3B-4E93-AE4D-DCF0EA7174CE}_is1`

func tidyLegacyShortcuts() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	changed := tidyUninstallEntry(exe)
	cmd := exec.Command("powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", tidyShortcutsScript)
	cmd.Env = append(os.Environ(), "TOLK_EXE="+exe)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		Logs.Warnf("整理旧快捷方式失败：%v", err)
	} else if n := strings.TrimSpace(string(out)); n != "" && n != "0" {
		Logs.Infof("已把 %s 处大佐翻译官的快捷方式换成 Tolk", n)
		changed = true
	}
	if changed {
		// Let Explorer drop the cached old icons.
		shChangeNotify := windows.NewLazySystemDLL("shell32.dll").NewProc("SHChangeNotify")
		const shcneAssocChanged = 0x08000000
		shChangeNotify.Call(shcneAssocChanged, 0, 0, 0)
	}
}

// tidyUninstallEntry renames the Apps & features entry the old installer
// wrote, when it belongs to this install. It reports whether it changed it.
func tidyUninstallEntry(exe string) bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, innoUninstallKey, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	loc, _, err := k.GetStringValue("InstallLocation")
	if err != nil || !strings.EqualFold(filepath.Clean(loc), filepath.Dir(exe)) {
		return false
	}
	want := map[string]string{
		"DisplayName":    "Tolk",
		"DisplayVersion": Version,
		"DisplayIcon":    exe,
		"Publisher":      "Achord",
	}
	changed := false
	for name, v := range want {
		if cur, _, err := k.GetStringValue(name); err == nil && cur == v {
			continue
		}
		if k.SetStringValue(name, v) == nil {
			changed = true
		}
	}
	if changed {
		Logs.Infof("已把「应用和功能」里的大佐翻译官改成 Tolk v%s", Version)
	}
	return changed
}

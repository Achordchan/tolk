//go:build windows

package update

import "golang.org/x/sys/windows/registry"

// installLocation is where the installer last put Tolk, from its uninstall
// entry; "" when there is none.
func installLocation() string {
	k, err := registry.OpenKey(registry.CURRENT_USER, uninstallKey, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	v, _, err := k.GetStringValue("InstallLocation")
	if err != nil {
		return ""
	}
	return v
}

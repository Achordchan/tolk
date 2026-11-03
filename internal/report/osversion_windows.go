package report

import (
	"fmt"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// OSVersion is the Windows version, such as "Windows 11 23H2 (10.0.22631.4317)".
// RtlGetVersion is used because GetVersionEx lies to unmanifested programs.
func OSVersion() string {
	v := windows.RtlGetVersion()
	name := "Windows 10"
	if v.MajorVersion == 10 && v.BuildNumber >= 22000 {
		name = "Windows 11"
	} else if v.MajorVersion != 10 {
		name = fmt.Sprintf("Windows NT %d.%d", v.MajorVersion, v.MinorVersion)
	}
	ubr, display := uint64(0), ""
	if k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion`, registry.QUERY_VALUE); err == nil {
		ubr, _, _ = k.GetIntegerValue("UBR")
		display, _, _ = k.GetStringValue("DisplayVersion")
		k.Close()
	}
	if display != "" {
		name += " " + display
	}
	return fmt.Sprintf("%s (%d.%d.%d.%d)", name, v.MajorVersion, v.MinorVersion, v.BuildNumber, ubr)
}

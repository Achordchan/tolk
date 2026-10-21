// Package winsel is the Windows half of 输入助手: reading the text the user
// selected in another application, writing the translation back, and the
// small windows and input hooks around the candidate panel. It talks to Win32
// and UI Automation directly (no cgo). Every file with code is Windows only;
// the logic that does not need Win32 is in package inputassist.
package winsel

//go:build windows

package winsel

import (
	"time"
	"unsafe"
)

const (
	inputKeyboard   = 1
	keyEventKeyUp   = 0x0002
	keyEventScan    = 0x0008
	extraInfoMarker = 0x546F6C6B // "Tolk": our own injected keys carry it

	vkShift   = 0x10
	vkControl = 0x11
	vkMenu    = 0x12
	vkLWin    = 0x5B
	vkRWin    = 0x5C
	vkC       = 0x43
	vkV       = 0x56
)

// keyInput is INPUT for a keyboard event; the union of the Win32 struct is
// as large as its mouse member, hence the padding.
type keyInput struct {
	Type uint32
	_    uint32 // alignment of the union on 64-bit
	Vk   uint16
	Scan uint16
	Flag uint32
	Time uint32
	Info uintptr
	_    [8]byte
}

func key(vk uint16, up bool) keyInput {
	in := keyInput{Type: inputKeyboard, Vk: vk, Info: extraInfoMarker}
	if up {
		in.Flag = keyEventKeyUp
	}
	return in
}

func sendKeys(in ...keyInput) {
	if len(in) == 0 {
		return
	}
	pSendInput.Call(uintptr(len(in)), uintptr(unsafe.Pointer(&in[0])), unsafe.Sizeof(in[0]))
}

func keyDown(vk int) bool {
	r, _, _ := pGetAsyncKeyState.Call(uintptr(vk))
	return r&0x8000 != 0
}

// releaseModifiers waits for the user to let go of Shift, Alt, Ctrl and Win
// (the shortcut is usually still held when it fires), and lets them go for
// the user if they do not. Ctrl+C typed while Alt and Shift are down would be
// a different shortcut altogether.
func releaseModifiers() {
	mods := []int{vkShift, vkControl, vkMenu, vkLWin, vkRWin}
	held := func() []int {
		var out []int
		for _, m := range mods {
			if keyDown(m) {
				out = append(out, m)
			}
		}
		return out
	}
	deadline := time.Now().Add(700 * time.Millisecond)
	for len(held()) > 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	var ups []keyInput
	for _, m := range held() {
		ups = append(ups, key(uint16(m), true))
	}
	sendKeys(ups...)
}

// ctrlChord presses Ctrl+letter.
func ctrlChord(vk uint16) {
	sendKeys(key(vkControl, false), key(vk, false), key(vk, true), key(vkControl, true))
}

// SuppressMenuActivation stops the app in front from opening its menu bar
// when the shortcut's Alt is released: Windows does that for an Alt that was
// pressed and released with no key in between, and the shortcut's own key was
// swallowed by the hotkey. A key that is assigned to nothing in between is
// what AutoHotkey and friends send for the same reason.
func SuppressMenuActivation() {
	if keyDown(vkMenu) {
		sendKeys(key(0xE8, false), key(0xE8, true))
	}
}

package inputassist

import (
	"fmt"
	"strings"
)

// The settings keep the shortcut the way the Mac app does: a macOS virtual
// key code and Carbon modifier bits. Windows maps them to its own keys:
// ⌃ is Ctrl, ⌥ is Alt, ⇧ is Shift and ⌘ is the Windows key.

// keyNames maps macOS virtual key codes (ANSI layout) to accelerator key names.
var keyNames = map[int]string{
	0: "A", 1: "S", 2: "D", 3: "F", 4: "H", 5: "G", 6: "Z", 7: "X", 8: "C", 9: "V",
	11: "B", 12: "Q", 13: "W", 14: "E", 15: "R", 16: "Y", 17: "T",
	18: "1", 19: "2", 20: "3", 21: "4", 22: "6", 23: "5", 24: "=", 25: "9", 26: "7",
	27: "-", 28: "8", 29: "0", 30: "]", 31: "O", 32: "U", 33: "[", 34: "I", 35: "P",
	36: "Enter", 37: "L", 38: "J", 39: "'", 40: "K", 41: ";", 42: "\\", 43: ",",
	44: "/", 45: "N", 46: "M", 47: ".", 48: "Tab", 49: "Space", 50: "`", 51: "Backspace",
	53: "Escape", 96: "F5", 97: "F6", 98: "F7", 99: "F3", 100: "F8", 101: "F9", 103: "F11",
	109: "F10", 111: "F12", 118: "F4", 120: "F2", 122: "F1",
	123: "Left", 124: "Right", 125: "Down", 126: "Up",
}

// KeyName is the name of a key code, "" when it has none.
func KeyName(keyCode int) string { return keyNames[keyCode] }

// WindowsShortcut renders a shortcut as an accelerator ("Ctrl+Alt+Space",
// which is also what the settings show). A bare key is refused: it would
// take that key away from every app.
func WindowsShortcut(keyCode, modifiers int) (string, error) {
	key := keyNames[keyCode]
	if key == "" {
		return "", fmt.Errorf("不支持的快捷键（按键 %d）", keyCode)
	}
	var parts []string
	if modifiers&ModControl != 0 {
		parts = append(parts, "Ctrl")
	}
	if modifiers&ModOption != 0 {
		parts = append(parts, "Alt")
	}
	if modifiers&ModShift != 0 {
		parts = append(parts, "Shift")
	}
	if modifiers&ModCmd != 0 {
		parts = append(parts, "Win")
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("快捷键需要至少一个修饰键（Ctrl、Alt 或 Shift）")
	}
	return strings.Join(append(parts, key), "+"), nil
}

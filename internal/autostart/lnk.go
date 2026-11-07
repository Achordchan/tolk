package autostart

import (
	"encoding/binary"
	"errors"
	"unicode/utf16"
)

// Shell Link (.lnk) fields this package reads; see [MS-SHLLINK].
const (
	lnkHeaderSize      = 0x4C
	lnkHasTargetIDList = 0x01
	lnkHasLinkInfo     = 0x02
	// LinkInfo flag: the target is on a local volume and LocalBasePath is set.
	linkInfoLocalPath = 0x01
	// LinkInfo header sizes from this one on carry the Unicode path offsets.
	linkInfoUnicodeHeader = 0x24
)

var errLnk = errors.New("读不出快捷方式的目标")

// lnkTarget returns the local path a shortcut points at, from its LinkInfo:
// LocalBasePath followed by CommonPathSuffix. Writers store the Unicode
// strings only when the path does not fit the system code page; otherwise the
// strings are in that code page, and ansi decodes them.
func lnkTarget(b []byte, ansi func([]byte) (string, error)) (string, error) {
	if len(b) < lnkHeaderSize || binary.LittleEndian.Uint32(b) != lnkHeaderSize {
		return "", errLnk
	}
	flags := binary.LittleEndian.Uint32(b[0x14:])
	off := lnkHeaderSize
	if flags&lnkHasTargetIDList != 0 {
		if off+2 > len(b) {
			return "", errLnk
		}
		off += 2 + int(binary.LittleEndian.Uint16(b[off:]))
	}
	if flags&lnkHasLinkInfo == 0 || off+0x1C > len(b) {
		return "", errLnk
	}
	info := b[off:]
	size := int(binary.LittleEndian.Uint32(info))
	if size < 0x1C || size > len(info) {
		return "", errLnk
	}
	info = info[:size]
	header := int(binary.LittleEndian.Uint32(info[4:]))
	if binary.LittleEndian.Uint32(info[8:])&linkInfoLocalPath == 0 || header > size {
		return "", errLnk
	}
	at := func(field int) int { return int(binary.LittleEndian.Uint32(info[field:])) }

	if header >= linkInfoUnicodeHeader {
		base, ok1 := utf16z(info, at(0x1C))
		suffix, ok2 := utf16z(info, at(0x20))
		if !ok1 || !ok2 || base == "" {
			return "", errLnk
		}
		return base + suffix, nil
	}
	base, ok1 := cstr(info, at(0x10))
	suffix, ok2 := cstr(info, at(0x18))
	if !ok1 || !ok2 || len(base) == 0 {
		return "", errLnk
	}
	return ansi(append(base, suffix...))
}

// cstr is the NUL-terminated byte string at off.
func cstr(b []byte, off int) ([]byte, bool) {
	if off <= 0 || off >= len(b) {
		return nil, false
	}
	for i := off; i < len(b); i++ {
		if b[i] == 0 {
			return append([]byte(nil), b[off:i]...), true
		}
	}
	return nil, false
}

// utf16z is the NUL-terminated UTF-16LE string at off.
func utf16z(b []byte, off int) (string, bool) {
	if off <= 0 || off >= len(b) {
		return "", false
	}
	var u []uint16
	for i := off; i+1 < len(b); i += 2 {
		c := binary.LittleEndian.Uint16(b[i:])
		if c == 0 {
			return string(utf16.Decode(u)), true
		}
		u = append(u, c)
	}
	return "", false
}

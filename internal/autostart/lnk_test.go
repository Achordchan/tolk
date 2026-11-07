package autostart

import (
	"bytes"
	"encoding/binary"
	"testing"
	"unicode/utf16"
)

func le32(v int) []byte { return binary.LittleEndian.AppendUint32(nil, uint32(v)) }

func utf16zBytes(s string) []byte {
	var b []byte
	for _, c := range utf16.Encode([]rune(s)) {
		b = binary.LittleEndian.AppendUint16(b, c)
	}
	return append(b, 0, 0)
}

// linkInfo lays out a LinkInfo block the way Windows writes one: header,
// VolumeID, then the strings. A non-empty unicodeBase adds the Unicode fields.
func linkInfo(ansiBase, ansiSuffix, unicodeBase, unicodeSuffix string) []byte {
	header := 0x1C
	if unicodeBase != "" {
		header = 0x24
	}
	volume := append(le32(0x11), make([]byte, 0x0D)...) // VolumeID, contents unread
	off := header + len(volume)
	baseAt := off
	off += len(ansiBase) + 1
	suffixAt := off
	off += len(ansiSuffix) + 1
	var tail []byte
	tail = append(tail, volume...)
	tail = append(tail, ansiBase...)
	tail = append(tail, 0)
	tail = append(tail, ansiSuffix...)
	tail = append(tail, 0)
	var uBaseAt, uSuffixAt int
	if unicodeBase != "" {
		if off%2 == 1 {
			tail = append(tail, 0)
			off++
		}
		uBaseAt = off
		ub := utf16zBytes(unicodeBase)
		off += len(ub)
		uSuffixAt = off
		us := utf16zBytes(unicodeSuffix)
		off += len(us)
		tail = append(append(tail, ub...), us...)
	}
	var b []byte
	b = append(b, le32(off)...)
	b = append(b, le32(header)...)
	b = append(b, le32(linkInfoLocalPath)...)
	b = append(b, le32(header)...) // VolumeIDOffset
	b = append(b, le32(baseAt)...)
	b = append(b, le32(0)...) // no network part
	b = append(b, le32(suffixAt)...)
	if unicodeBase != "" {
		b = append(b, le32(uBaseAt)...)
		b = append(b, le32(uSuffixAt)...)
	}
	return append(b, tail...)
}

func shellLink(idList []byte, info []byte) []byte {
	flags := lnkHasLinkInfo
	if idList != nil {
		flags |= lnkHasTargetIDList
	}
	h := make([]byte, lnkHeaderSize)
	copy(h, le32(lnkHeaderSize))
	copy(h[0x14:], le32(flags))
	if idList != nil {
		h = binary.LittleEndian.AppendUint16(h, uint16(len(idList)))
		h = append(h, idList...)
	}
	return append(append(h, info...), bytes.Repeat([]byte{0}, 4)...) // terminal block
}

func latin(b []byte) (string, error) { return string(b), nil }

func TestLnkTarget(t *testing.T) {
	const exe = `C:\Users\me\AppData\Local\Programs\Tolk\Tolk.exe`
	cases := map[string]struct {
		lnk  []byte
		want string
	}{
		"ansi":             {shellLink(nil, linkInfo(exe, "", "", "")), exe},
		"after an id list": {shellLink([]byte{0x14, 0, 1, 2, 3}, linkInfo(exe, "", "", "")), exe},
		"split path":       {shellLink(nil, linkInfo(`C:\Users\me\`, `Tolk\Tolk.exe`, "", "")), `C:\Users\me\Tolk\Tolk.exe`},
		"unicode":          {shellLink(nil, linkInfo(`C:\Users\??\Tolk.exe`, "", `C:\Users\小明\Tolk.exe`, "")), `C:\Users\小明\Tolk.exe`},
	}
	for name, c := range cases {
		got, err := lnkTarget(c.lnk, latin)
		if err != nil || got != c.want {
			t.Errorf("%s: got %q, %v; want %q", name, got, err, c.want)
		}
	}
}

func TestLnkTargetRejectsBadInput(t *testing.T) {
	good := shellLink(nil, linkInfo(`C:\Tolk.exe`, "", "", ""))
	noInfo := append([]byte(nil), good...)
	copy(noInfo[0x14:], le32(0))
	withFlag := func(f int) []byte {
		b := append([]byte(nil), good...)
		copy(b[0x14:], le32(lnkHasLinkInfo|f))
		return b
	}
	cases := map[string][]byte{
		"ForceNoLinkInfo":       withFlag(0x100),
		"HasExpString":          withFlag(0x200),
		"HasDarwinID":           withFlag(0x1000),
		"PreferEnvironmentPath": withFlag(0x2000000),
		"empty":       nil,
		"not a link":  []byte("<?xml version=\"1.0\"?><plist></plist>" + string(make([]byte, 80))),
		"no linkinfo": noInfo,
		"truncated":   good[:lnkHeaderSize+20],
	}
	for i := lnkHeaderSize; i < len(good)-4; i++ { // the 4 terminal bytes are not needed
		if _, err := lnkTarget(good[:i], latin); err == nil {
			t.Errorf("cut at %d: no error", i)
		}
	}
	for name, b := range cases {
		if got, err := lnkTarget(b, latin); err == nil {
			t.Errorf("%s: got %q, want an error", name, got)
		}
	}
}

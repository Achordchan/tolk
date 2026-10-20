//go:build darwin

package doublecopy

import (
	"sync"

	"github.com/ebitengine/purego"
)

var (
	once       sync.Once
	ready      bool
	msgSendID  func(obj, sel uintptr) uintptr
	msgSendN   func(obj, sel uintptr) int
	pasteboard uintptr
	selChange  uintptr
)

func setup() {
	objc, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return
	}
	if _, err := purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit", purego.RTLD_NOW|purego.RTLD_GLOBAL); err != nil {
		return
	}
	var getClass func(name string) uintptr
	var selReg func(name string) uintptr
	purego.RegisterLibFunc(&getClass, objc, "objc_getClass")
	purego.RegisterLibFunc(&selReg, objc, "sel_registerName")
	purego.RegisterLibFunc(&msgSendID, objc, "objc_msgSend")
	purego.RegisterLibFunc(&msgSendN, objc, "objc_msgSend")
	class := getClass("NSPasteboard")
	if class == 0 {
		return
	}
	// The general pasteboard is a singleton; fetching it once keeps polling
	// free of autoreleased objects on goroutine threads.
	pasteboard = msgSendID(class, selReg("generalPasteboard"))
	selChange = selReg("changeCount")
	ready = pasteboard != 0
}

// Sequence is NSPasteboard's changeCount, which grows on every copy, even of
// the same text.
func Sequence() (uint64, bool) {
	once.Do(setup)
	if !ready {
		return 0, false
	}
	return uint64(msgSendN(pasteboard, selChange)), true
}

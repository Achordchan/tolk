//go:build darwin

package main

import (
	"sync"

	"github.com/ebitengine/purego"
	"github.com/egoist/mygo"
)

var titleBarObjC struct {
	once sync.Once
	ok   bool
	sel  func(name string) uintptr
	send func(obj, sel uintptr, on bool)
}

// nativeTitleBar lets the window's background (set by tintTitleBar) show
// through the title bar, so it takes the pink theme's colour; off gives it
// back to the system. The title text keeps the system colour, which reads
// fine on the light pink.
func nativeTitleBar(w *mygo.Window, on bool, _, _ uint32) {
	o := &titleBarObjC
	o.once.Do(func() {
		lib, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			return
		}
		purego.RegisterLibFunc(&o.sel, lib, "sel_registerName")
		purego.RegisterLibFunc(&o.send, lib, "objc_msgSend")
		o.ok = true
	})
	if !o.ok {
		return
	}
	mygo.RunOnMain(func() {
		if win := w.NativeHandle(); win != 0 {
			o.send(win, o.sel("setTitlebarAppearsTransparent:"), on)
		}
	})
}

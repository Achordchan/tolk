// tolk-release builds and signs the release files that the existing update
// channels expect, so one tool serves Tolk's own releases and the bridge
// releases published to the old dazuo-win and bagayalu-translate repos.
//
//	tolk-release winpack -exe Tolk.exe -icon logo.ico -version 2.0.0 -out dist
//	tolk-release winsign -zip dist/dazuofanyiguan_full.for.windows_2.0.0.zip -version 2.0.0
//	tolk-release notes   -version 2.0.0 -changelog CHANGELOG.md -sig-json x.sig.json -out notes.md
//	tolk-release appcast -zip Tolk-2.0.0-macos.zip -version 2.0.0 -build 20000 -url https://… -notes notes.md -out appcast.xml
//	tolk-release gitee   -version 2.0.0 -assets dist -notes notes.md
//
// Private keys are read from the environment (DZFYQ_UPDATE_PRIVATE_KEY_PEM,
// SPARKLE_PRIVATE_KEY) or a file; they are never written anywhere.
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "winpack":
		err = winpack(os.Args[2:])
	case "winsign":
		err = winsign(os.Args[2:])
	case "notes":
		err = notes(os.Args[2:])
	case "appcast":
		err = appcast(os.Args[2:])
	case "gitee":
		err = gitee(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "错误：", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "用法：tolk-release winpack|winsign|notes|appcast|gitee [参数]")
}

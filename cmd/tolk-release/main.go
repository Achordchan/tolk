// tolk-release builds and signs the files of a Tolk release.
//
//	tolk-release winpack -exe Tolk.exe -version 2.0.0 -out dist
//	tolk-release winsign -zip dist/Tolk-update-windows_2.0.0.zip -version 2.0.0
//	tolk-release notes   -version 2.0.0 -changelog CHANGELOG.md -out notes.md
//	tolk-release appcast -zip Tolk-2.0.0-macos.zip -version 2.0.0 -build 20000 -url https://… -notes notes.md -out appcast.xml
//	tolk-release latest  -version 2.0.0 -dir dist -notes notes.md -out dist/latest.json
//	tolk-release keygen  -out ~/tolk-keys
//
// Private keys are read from the environment (TOLK_UPDATE_PRIVATE_KEY_PEM,
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
	case "latest":
		err = latest(os.Args[2:])
	case "keygen":
		err = keygen(os.Args[2:])
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
	fmt.Fprintln(os.Stderr, "用法：tolk-release winpack|winsign|notes|appcast|latest|keygen [参数]")
}

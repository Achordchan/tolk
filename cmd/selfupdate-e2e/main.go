// Command selfupdate-e2e plays the running Tolk in its own Windows update,
// for the win-self-update workflow: copied into a scratch install dir as
// 大佐翻译官.exe, it checks the published releases as a very old version,
// downloads, verifies and starts the swap with the real code, and exits.
// The workflow then checks that the swap happened.
package main

import (
	"context"
	"log"
	"os"
	"time"

	"tolk/internal/update"
)

const old = "0.0.1"

func main() {
	log.SetFlags(log.Ltime | log.Lmicroseconds)
	c := update.NewChecker("windows", old)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	info, err := c.Check(ctx, old)
	if err != nil || info == nil {
		log.Fatalf("check: %v %v", info, err)
	}
	log.Printf("found v%s from %s", info.Version, info.Source)
	dirs, err := update.DefaultDirs()
	if err != nil {
		log.Fatal(err)
	}
	if err := c.Install(ctx, info, dirs, nil); err != nil {
		log.Fatalf("install: %v", err)
	}
	log.Printf("swap script running, pid %d exiting", os.Getpid())
	time.Sleep(time.Second) // like the app, which quits shortly after
}

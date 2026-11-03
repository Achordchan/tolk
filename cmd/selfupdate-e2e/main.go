//go:build e2e

// Command selfupdate-e2e plays Tolk in its own update, for the
// win-self-update workflow. Built twice (an old and a new version), the old
// one checks a local release server like the app checks GitHub, downloads
// over the fastest route, verifies, starts the swap with the real code and
// exits; the workflow then checks the install dir. It also serves that
// release and makes the throwaway signing key.
//
//	selfupdate-e2e keys -out DIR             write DIR/win.pem and DIR/win.pub
//	selfupdate-e2e serve -dir DIR -addr A    the release: /dl/ (slow) and /m/ (a fast proxy of /dl/)
//	selfupdate-e2e                           update, as the app would (env TOLK_E2E_*)
package main

import (
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"tolk/internal/legacy"
	"tolk/internal/update"
)

// version is set with -ldflags "-X main.version=…".
var version = "0.0.1"

func main() {
	log.SetFlags(log.Ltime | log.Lmicroseconds)
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "keys":
			keys(os.Args[2:])
			return
		case "serve":
			serve(os.Args[2:])
			return
		case "--update-restart":
			restarted()
			return
		}
	}
	run()
}

func keys(args []string) {
	fs := flag.NewFlagSet("keys", flag.ExitOnError)
	out := fs.String("out", "", "")
	fs.Parse(args)
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		log.Fatal(err)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(priv)
	os.MkdirAll(*out, 0o700)
	must(os.WriteFile(filepath.Join(*out, "win.pem"), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600))
	must(os.WriteFile(filepath.Join(*out, "win.pub"), []byte(base64.StdEncoding.EncodeToString(pub)), 0o644))
}

// serve is the release host. /dl/ is the origin, slowed down the way GitHub
// often is from China; /m/<origin URL> is a proxy in front of it at full
// speed, so the updater should pick it.
func serve(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	dir := fs.String("dir", "", "")
	addr := fs.String("addr", "127.0.0.1:8765", "")
	fs.Parse(args)
	files := http.StripPrefix("/dl/", http.FileServer(http.Dir(*dir)))
	mux := http.NewServeMux()
	mux.Handle("/dl/", slow(files))
	mux.HandleFunc("/m/", func(w http.ResponseWriter, r *http.Request) {
		u := strings.TrimPrefix(r.URL.Path, "/m/")
		i := strings.Index(u, "/dl/")
		if i < 0 {
			http.NotFound(w, r)
			return
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = u[i:]
		files.ServeHTTP(w, r2)
	})
	log.Fatal(http.ListenAndServe(*addr, mux))
}

type slowWriter struct{ http.ResponseWriter }

func (w slowWriter) Write(b []byte) (int, error) {
	n := 0
	for len(b) > 0 {
		k := min(len(b), 16<<10)
		m, err := w.ResponseWriter.Write(b[:k])
		n += m
		if err != nil {
			return n, err
		}
		if f, ok := w.ResponseWriter.(http.Flusher); ok {
			f.Flush()
		}
		b = b[k:]
		time.Sleep(100 * time.Millisecond) // about 160 KB/s
	}
	return n, nil
}

func slow(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".json") {
			h.ServeHTTP(w, r) // small files: no need to wait
			return
		}
		h.ServeHTTP(slowWriter{w}, r)
	})
}

// restarted is the new version started by the swap script.
func restarted() {
	home, err := legacy.Home()
	must(err)
	must(os.WriteFile(filepath.Join(home, "restarted-"+version), nil, 0o644))
	log.Printf("v%s started after the update", version)
}

func run() {
	c := update.NewChecker("windows", version)
	c.Latest = os.Getenv("TOLK_E2E_LATEST")
	c.API = ""
	c.Origin = os.Getenv("TOLK_E2E_ORIGIN")
	c.Mirrors = []string{os.Getenv("TOLK_E2E_MIRROR")}
	c.Logf = log.Printf
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	info, err := c.Check(ctx, version)
	if err != nil || info == nil {
		log.Fatalf("check: %v %v", info, err)
	}
	log.Printf("v%s found v%s from %s", version, info.Version, info.Source)
	dirs, err := update.DefaultDirs()
	must(err)
	p, err := c.Prepare(ctx, info, dirs, nil)
	if err != nil {
		log.Fatalf("prepare: %v", err)
	}
	relaunch := os.Getenv("TOLK_E2E_RELAUNCH") != "0"
	if err := p.Apply(relaunch); err != nil {
		log.Fatalf("apply: %v", err)
	}
	log.Printf("swap script running (relaunch %v), pid %d exiting", relaunch, os.Getpid())
	time.Sleep(time.Second) // like the app, which quits shortly after
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

//go:build windows

package screenshot

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

// TestWorkspaceStartsWarm runs the real workspace in Windows PowerShell 5.1:
// it must report warm without touching the screen, open the selection on
// Begin, keep the compiled controls for the next start, and still work
// without a cache. Needs an interactive desktop, so it only runs when
// TOLK_WIN_E2E=1.
func TestWorkspaceStartsWarm(t *testing.T) {
	if os.Getenv("TOLK_WIN_E2E") != "1" {
		t.Skip("set TOLK_WIN_E2E=1 to run against a real desktop")
	}
	dll, err := PrepareCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ dll, types string }{{dll, "compiled"}, {dll, "cache"}, {"", "memory"}} {
		p, err := Launch(c.dll)
		if err != nil {
			t.Fatal(err)
		}
		w, err := p.WaitWarm(3 * time.Minute)
		if err != nil {
			p.Kill()
			_ = p.Wait()
			t.Fatalf("%s: %v\n%s", c.types, err, p.Stderr())
		}
		warmAfter := time.Since(p.Started())
		if w.Types != c.types {
			t.Errorf("controls came from %q, want %q", w.Types, c.types)
		}
		if !p.Alive() {
			t.Fatal("warm process not alive")
		}
		begun := time.Now()
		if err := p.Begin(Start{Target: "English", Theme: "dark", Languages: []string{"English", "简体中文"}}); err != nil {
			t.Fatal(err)
		}
		line, err := p.out.ReadBytes('\n')
		var r Request
		if err != nil || json.Unmarshal(line, &r) != nil || !r.Ready {
			p.Kill()
			_ = p.Wait()
			t.Fatalf("%s: no ready after Begin (%q, %v)\n%s", c.types, line, err, p.Stderr())
		}
		t.Logf("%s: warm %v after launch (controls %d ms), selection %v after Begin", c.types, warmAfter.Round(time.Millisecond), w.TypesMs, time.Since(begun).Round(time.Millisecond))
		p.Kill()
		_ = p.Wait()
		if p.Alive() {
			t.Fatal("killed process still alive")
		}
	}
	if _, err := os.Stat(dll); err != nil {
		t.Fatalf("compiled controls not kept: %v", err)
	}
}

// A spare whose PowerShell is gone must say so instead of waiting forever.
func TestWarmWaitEndsWhenProcessDies(t *testing.T) {
	if os.Getenv("TOLK_WIN_E2E") != "1" {
		t.Skip("set TOLK_WIN_E2E=1 to run against a real desktop")
	}
	p, err := Launch("")
	if err != nil {
		t.Fatal(err)
	}
	p.Kill()
	if _, err := p.WaitWarm(time.Minute); err == nil {
		t.Fatal("dead process reported warm")
	}
	_ = p.Wait()
}

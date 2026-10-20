package helper

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestMain doubles as a fake helper when asked to.
func TestMain(m *testing.M) {
	if os.Getenv("FAKE_HELPER") == "1" {
		fake()
		return
	}
	os.Exit(m.Run())
}

// fake answers hello, echoes translate by asking the host back, emits an event.
func fake() {
	in := bufio.NewScanner(os.Stdin)
	out := json.NewEncoder(os.Stdout)
	for in.Scan() {
		var m map[string]any
		json.Unmarshal(in.Bytes(), &m)
		switch m["op"] {
		case "hello":
			out.Encode(map[string]any{"id": m["id"], "ok": true, "version": "1", "capabilities": []string{"ocr"}})
		case "shout":
			// helper → Go request, then answer the original with what Go said
			out.Encode(map[string]any{"id": "h1", "op": "translate", "texts": []string{"a", "b"}, "source": "自动检测", "target": "简体中文"})
			if !in.Scan() {
				return
			}
			var back map[string]any
			json.Unmarshal(in.Bytes(), &back)
			out.Encode(map[string]any{"event": "log", "level": "info", "msg": "got reply"})
			out.Encode(map[string]any{"id": m["id"], "ok": true, "texts": back["texts"]})
		case "fail":
			out.Encode(map[string]any{"id": m["id"], "ok": false, "error": "缺少屏幕录制权限"})
		case "quit":
			out.Encode(map[string]any{"id": m["id"], "ok": true})
			return
		}
	}
}

func start(t *testing.T, handle Handler, event EventFunc) *Client {
	t.Helper()
	exe, _ := os.Executable()
	t.Setenv("FAKE_HELPER", "1")
	c, err := Start(exe, handle, event, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

func TestCallEventsAndHostRequests(t *testing.T) {
	events := make(chan string, 4)
	c := start(t, func(ctx context.Context, op string, raw json.RawMessage) (any, error) {
		var p struct {
			Texts []string `json:"texts"`
		}
		json.Unmarshal(raw, &p)
		out := make([]string, len(p.Texts))
		for i, s := range p.Texts {
			out[i] = strings.ToUpper(s)
		}
		return map[string]any{"texts": out}, nil
	}, func(name string, raw json.RawMessage) { events <- name })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var hello struct {
		Version string `json:"version"`
	}
	if err := c.Call(ctx, "hello", nil, &hello); err != nil || hello.Version != "1" {
		t.Fatalf("hello: %v %+v", err, hello)
	}
	var shout struct {
		Texts []string `json:"texts"`
	}
	if err := c.Call(ctx, "shout", nil, &shout); err != nil || len(shout.Texts) != 2 || shout.Texts[0] != "A" {
		t.Fatalf("shout: %v %+v", err, shout)
	}
	if got := <-events; got != "log" {
		t.Fatalf("event %s", got)
	}
	if err := c.Call(ctx, "fail", nil, nil); err == nil || !strings.Contains(err.Error(), "屏幕录制") {
		t.Fatalf("error text must come through, got %v", err)
	}
}

func TestCallAfterExitFails(t *testing.T) {
	exe, _ := os.Executable()
	cmd := exec.Command("true")
	_ = cmd
	t.Setenv("FAKE_HELPER", "1")
	c, err := Start(exe, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	<-c.Done()
	if err := c.Call(context.Background(), "hello", nil, nil); err == nil {
		t.Fatal("call on a dead helper must fail")
	}
}

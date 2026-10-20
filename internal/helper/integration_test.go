package helper

import (
	"context"
	"os"
	"testing"
	"time"
)

// Runs against the real Swift helper when TOLK_HELPER names it (the macOS CI
// job does); skipped everywhere else.
func TestRealHelperHelloAndPermissions(t *testing.T) {
	path := os.Getenv("TOLK_HELPER")
	if path == "" {
		t.Skip("TOLK_HELPER not set")
	}
	c, err := Start(path, nil, nil, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var hello struct {
		Version      string   `json:"version"`
		Macos        string   `json:"macos"`
		Capabilities []string `json:"capabilities"`
	}
	if err := c.Call(ctx, "hello", nil, &hello); err != nil {
		t.Fatal(err)
	}
	if hello.Version == "" || hello.Macos == "" {
		t.Fatalf("hello: %+v", hello)
	}
	var perms struct {
		Accessibility   *bool `json:"accessibility"`
		ScreenRecording *bool `json:"screenRecording"`
	}
	if err := c.Call(ctx, "permissions", nil, &perms); err != nil {
		t.Fatal(err)
	}
	if perms.Accessibility == nil || perms.ScreenRecording == nil {
		t.Fatalf("permissions: %+v", perms)
	}
	// Unknown ops must come back as errors, not hangs.
	if err := c.Call(ctx, "no.such.op", nil, nil); err == nil {
		t.Fatal("unknown op should fail")
	}
	// Closing stdin must end the process quickly (parent-gone behaviour).
	c.Close()
	select {
	case <-c.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("helper did not exit after Close")
	}
}

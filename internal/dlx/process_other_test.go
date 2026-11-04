//go:build !windows

package dlx

import (
	"context"
	"testing"
	"time"
)

func TestLifetimePipeStopsPlugin(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := pluginCommand(ctx, "/bin/sleep", "60")
	prepare(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	cleanup, err := supervise(cmd)
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("plugin survived parent pipe close")
	}
}

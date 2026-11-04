//go:build !windows

package dlx

import (
	"context"
	"io"
	"os/exec"
)

// The shell's fd 3 is a parent-lifetime pipe. EOF survives parent crashes and
// kills the service without PID polling. The wrapper waits and reaps its child.
func pluginCommand(ctx context.Context, path string, args ...string) *exec.Cmd {
	script := `exec 3<&0
"$@" < /dev/null &
child=$!
trap 'kill "$child" 2>/dev/null; wait "$child" 2>/dev/null; exit' TERM INT HUP
(read ignored <&3; kill "$child" 2>/dev/null) &
watcher=$!
wait "$child"
status=$?
kill "$watcher" 2>/dev/null
wait "$watcher" 2>/dev/null
exit "$status"`
	return exec.CommandContext(ctx, "/bin/sh", append([]string{"-c", script, "tolk-dlx", path}, args...)...)
}

var lifetimePipe io.WriteCloser

func prepare(cmd *exec.Cmd) { lifetimePipe, _ = cmd.StdinPipe() }
func supervise(cmd *exec.Cmd) (func(), error) {
	pipe := lifetimePipe
	return func() {
		if pipe != nil {
			_ = pipe.Close()
		}
	}, nil
}

// Package helper talks to tolk-helper, the macOS native sidecar, over
// newline-delimited JSON on its stdin/stdout. See macos/helper/PROTOCOL.md.
package helper

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// Handler answers requests that the helper sends to Tolk.
type Handler func(ctx context.Context, op string, params json.RawMessage) (any, error)

// EventFunc receives helper events.
type EventFunc func(name string, raw json.RawMessage)

// Client is one running helper process.
type Client struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	writeMu sync.Mutex
	nextID  atomic.Int64

	mu      sync.Mutex
	pending map[string]chan reply
	closed  chan struct{}
	exitErr error

	handle Handler
	event  EventFunc
}

type reply struct {
	raw json.RawMessage
	err error
}

type envelope struct {
	ID    string          `json:"id"`
	Op    string          `json:"op"`
	Event string          `json:"event"`
	OK    *bool           `json:"ok"`
	Error string          `json:"error"`
	Raw   json.RawMessage `json:"-"`
}

// Start launches the helper. handle and event may be nil.
func Start(path string, handle Handler, event EventFunc, logf func(string, ...any)) (*Client, error) {
	cmd := exec.Command(path, "--parent-pid", strconv.Itoa(os.Getpid()))
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	c := &Client{cmd: cmd, stdin: stdin, pending: map[string]chan reply{}, closed: make(chan struct{}), handle: handle, event: event}
	go func() { // human-readable logs
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			if logf != nil {
				logf("助手：%s", sc.Text())
			}
		}
	}()
	go c.readLoop(stdout)
	go func() {
		err := cmd.Wait()
		c.mu.Lock()
		c.exitErr = err
		for id, ch := range c.pending {
			ch <- reply{err: errors.New("原生助手已退出")}
			delete(c.pending, id)
		}
		c.mu.Unlock()
		close(c.closed)
	}()
	return c, nil
}

// Done is closed when the helper process has exited.
func (c *Client) Done() <-chan struct{} { return c.closed }

func (c *Client) readLoop(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), 64<<20) // translated screenshots can be large
	for sc.Scan() {
		line := append([]byte(nil), sc.Bytes()...)
		var e envelope
		if err := json.Unmarshal(line, &e); err != nil {
			continue
		}
		e.Raw = line
		switch {
		case e.Event != "":
			if c.event != nil {
				c.event(e.Event, e.Raw)
			}
		case e.Op != "":
			go c.serve(e)
		case e.OK != nil:
			c.mu.Lock()
			ch := c.pending[e.ID]
			delete(c.pending, e.ID)
			c.mu.Unlock()
			if ch != nil {
				if *e.OK {
					ch <- reply{raw: e.Raw}
				} else {
					msg := e.Error
					if msg == "" {
						msg = "助手返回了错误"
					}
					ch <- reply{err: errors.New(msg)}
				}
			}
		}
	}
}

func (c *Client) serve(e envelope) {
	var out any
	var err error
	if c.handle == nil {
		err = errors.New("不支持的请求")
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		out, err = c.handle(ctx, e.Op, e.Raw)
	}
	msg := map[string]any{"id": e.ID}
	if err != nil {
		msg["ok"], msg["error"] = false, err.Error()
	} else {
		msg["ok"] = true
		if out != nil {
			b, _ := json.Marshal(out)
			var fields map[string]any
			if json.Unmarshal(b, &fields) == nil {
				for k, v := range fields {
					msg[k] = v
				}
			}
		}
	}
	_ = c.write(msg)
}

func (c *Client) write(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err = c.stdin.Write(append(b, '\n'))
	return err
}

// Call sends a request and decodes the reply's fields into out (may be nil).
func (c *Client) Call(ctx context.Context, op string, params map[string]any, out any) error {
	id := "g" + strconv.FormatInt(c.nextID.Add(1), 10)
	msg := map[string]any{"id": id, "op": op}
	for k, v := range params {
		msg[k] = v
	}
	ch := make(chan reply, 1)
	c.mu.Lock()
	select {
	case <-c.closed:
		c.mu.Unlock()
		return errors.New("原生助手没有在运行")
	default:
	}
	c.pending[id] = ch
	c.mu.Unlock()
	if err := c.write(msg); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return fmt.Errorf("发送给助手失败：%w", err)
	}
	select {
	case r := <-ch:
		if r.err != nil {
			return r.err
		}
		if out != nil {
			return json.Unmarshal(r.raw, out)
		}
		return nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return ctx.Err()
	}
}

// Close asks the helper to quit, then makes sure it is gone.
func (c *Client) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()
	_ = c.Call(ctx, "quit", nil, nil)
	select {
	case <-c.closed:
	case <-time.After(time.Second):
		_ = c.cmd.Process.Kill()
	}
}

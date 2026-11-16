// Package screenshot defines the private protocol used by the native workspace.
package screenshot

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

type Request struct {
	Warm   bool     `json:"warm"`
	Ready  bool     `json:"ready"`
	Closed bool     `json:"closed"`
	ID     int      `json:"id"`
	Source string   `json:"source"`
	Target string   `json:"target"`
	Blocks []string `json:"blocks"`
}
type Response struct {
	ID    int      `json:"id"`
	Texts []string `json:"texts"`
	Error string   `json:"error"`
}

// Serve translates in reading order, preserving block positions in the response.
// No recognized content is logged or forwarded to the main-window editor.
func Serve(in io.Reader, out io.Writer, translate func(context.Context, string, string, string) (string, error), onClose ...func()) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 4096), 2<<20)
	encoder := json.NewEncoder(out)
	ctx, cancel := context.WithCancel(context.Background())
	var workers sync.WaitGroup
	defer func() { cancel(); workers.Wait() }()
	var writeMu sync.Mutex
	closed := false
	for scanner.Scan() {
		var req Request
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			return fmt.Errorf("invalid screenshot request: %w", err)
		}
		if req.Warm {
			// The launch handshake (Process.WaitWarm); never a translation request.
			continue
		}
		if req.Ready {
			if len(onClose) > 1 {
				onClose[1]()
			}
			continue
		}
		if req.Closed {
			cancel()
			closed = true
			if len(onClose) > 0 {
				onClose[0]()
			}
			continue
		}
		if closed {
			continue
		}
		workers.Add(1)
		go func(req Request) {
			defer workers.Done()
			res := Response{ID: req.ID, Texts: []string{}}
			if len(req.Blocks) == 0 || len(req.Blocks) > 200 {
				res.Error = "识别段落数量无效，请缩小选区"
			} else {
				for _, text := range req.Blocks {
					if len(text) > 16000 {
						res.Error = "识别段落过长，请缩小选区"
						break
					}
					if ctx.Err() != nil {
						return
					}
					translated, err := translate(ctx, text, req.Source, req.Target)
					if err != nil {
						res.Error = err.Error()
						res.Texts = nil
						break
					}
					res.Texts = append(res.Texts, translated)
				}
			}
			writeMu.Lock()
			defer writeMu.Unlock()
			if ctx.Err() == nil {
				_ = encoder.Encode(res)
			}
		}(req)
	}
	if err := scanner.Err(); err != nil {
		cancel()
		return err
	}
	return nil
}

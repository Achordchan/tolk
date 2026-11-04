// Package reportqueue persists sanitized reports before attempting delivery.
package reportqueue

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"tolk/internal/report"
)

var mu sync.Mutex
var flushMu sync.Mutex

// Enqueue writes atomically; the submission ID survives every retry.
func Enqueue(dir string, r report.Report) error {
	mu.Lock()
	defer mu.Unlock()
	if r.SubmissionID == "" || strings.ContainsAny(r.SubmissionID, "/\\.") {
		return fmt.Errorf("invalid submission id")
	}
	dir = filepath.Join(dir, "report_queue")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if len(entries) >= 100 {
		return fmt.Errorf("report queue is full; report retained by caller")
	}
	b, err := json.Marshal(r.Clean())
	if err != nil {
		return err
	}
	path := filepath.Join(dir, r.SubmissionID+".json")
	if _, err = os.Stat(path); err == nil {
		return nil
	}
	f, err := os.CreateTemp(dir, "pending-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// Enabled reads current consent on every attempt; unreadable state fails closed.
func Enabled(dir string) bool {
	b, err := os.ReadFile(filepath.Join(dir, "report_state.json"))
	if err != nil {
		return false
	}
	var s report.State
	return json.Unmarshal(b, &s) == nil && !s.AutoDisabled
}

// Flush preserves failures, pending submissions and server rate-limit responses.
func Flush(ctx context.Context, dir string) bool {
	flushMu.Lock()
	defer flushMu.Unlock()
	if !Enabled(dir) {
		return false
	}
	files, _ := filepath.Glob(filepath.Join(dir, "report_queue", "*.json"))
	for _, path := range files {
		if ctx.Err() != nil || !Enabled(dir) {
			return false
		}
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var r report.Report
		if json.Unmarshal(b, &r) != nil {
			continue
		}
		attempt, cancel := context.WithTimeout(ctx, 40*time.Second)
		res, err := report.Send(attempt, nil, r.Clean())
		cancel()
		if err == nil && res.OK && !res.Pending && res.Skipped == "" {
			// Acknowledge only this submission, never a newer reservation.
			_, saveErr := report.NewStore(dir).Update(func(st *report.State) {
				if st.Subs[r.Fingerprint] == r.SubmissionID {
					st.Tried[r.Fingerprint] = time.Now().Unix()
				}
			})
			if saveErr == nil {
				_ = os.Remove(path)
			}
		}
	}
	files, _ = filepath.Glob(filepath.Join(dir, "report_queue", "*.json"))
	return len(files) == 0
}

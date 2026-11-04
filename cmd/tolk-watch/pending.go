package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
	"tolk/internal/report"
	"tolk/internal/reportqueue"
)

// Preserve a sanitized source before limits or temporary disk errors defer delivery.
func reserveCrash(dir string, r report.Report) error {
	path := filepath.Join(dir, "crash-source-"+r.SubmissionID+".json")
	b, err := json.Marshal(r.Clean())
	if err != nil {
		return err
	}
	if err = os.WriteFile(path, b, 0600); err != nil {
		return err
	}
	return retryCrash(dir, path, r)
}
func retryCrash(dir, path string, r report.Report) error {
	store := report.NewStore(dir)
	switch store.TryReserve(r.Fingerprint, time.Now()) {
	case report.Later:
		return nil
	case report.Declined:
		return os.Remove(path)
	}
	r.SubmissionID = store.Submission(r.Fingerprint)
	if err := reportqueue.Enqueue(dir, r); err != nil {
		store.Release(r.Fingerprint)
		return err
	}
	store.Finished(r.Fingerprint, time.Now())
	return os.Remove(path)
}
func retryCrashes(dir string) {
	files, _ := filepath.Glob(filepath.Join(dir, "crash-source-*.json"))
	for _, path := range files {
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var r report.Report
		if json.Unmarshal(b, &r) == nil {
			_ = retryCrash(dir, path, r)
		}
	}
}

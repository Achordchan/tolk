package report

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Local limits for reports the app sends by itself. The relay has its own,
// these just keep a machine in an error loop from trying all day.
const (
	SameErrorEvery = 24 * time.Hour
	MaxAutoPerDay  = 5
)

// State is report_state.json: the anonymous install id, the auto-report
// switch and what was sent recently.
type State struct {
	InstallID    string           `json:"installId"`
	AutoDisabled bool             `json:"autoDisabled"`
	Sent         map[string]int64 `json:"sent"` // fingerprint -> unix seconds
	// Tried holds when the upload of a fingerprint last finished (sent or
	// failed). Sent later than Tried means the app quit mid-upload: that
	// report is let through again instead of counting as sent.
	Tried map[string]int64 `json:"tried,omitempty"`
	// Subs is the submission id of each upload not finished yet. A retry
	// after the app quit mid-upload sends the same one, so the relay can
	// tell whether the interrupted attempt already got through.
	Subs  map[string]string `json:"subs,omitempty"`
	Day   string            `json:"day"`
	Count int               `json:"count"`
	// UpdateResultAt is the updated_at of the last update result reported, so
	// one failed update is reported once.
	UpdateResultAt string `json:"updateResultAt"`
}

// Store keeps State in a file.
type Store struct {
	mu   sync.Mutex
	path string
	// inflight are this run's uploads not finished yet: an upload left
	// unfinished by an earlier run may go again, one still running may not.
	inflight map[string]bool
}

// NewStore uses dir/report_state.json.
func NewStore(dir string) *Store { return &Store{path: filepath.Join(dir, "report_state.json")} }

// Dir is the folder of the state file (crash output lives next to it).
func (s *Store) Dir() string { return filepath.Dir(s.path) }

func (s *Store) load() State {
	var st State
	if raw, err := os.ReadFile(s.path); err == nil {
		_ = json.Unmarshal(raw, &st)
	}
	if st.Sent == nil {
		st.Sent = map[string]int64{}
	}
	if st.Tried == nil {
		st.Tried = map[string]int64{}
	}
	if st.Subs == nil {
		st.Subs = map[string]string{}
	}
	return st
}

func (s *Store) save(st State) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	raw, _ := json.MarshalIndent(st, "", "  ")
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Update reads, changes and writes the state under the lock.
func (s *Store) Update(fn func(*State)) (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.load()
	fn(&st)
	if st.InstallID == "" {
		st.InstallID = newID()
	}
	return st, s.save(st)
}

// Get returns the state, creating the install id on first use.
func (s *Store) Get() State {
	s.mu.Lock()
	st := s.load()
	s.mu.Unlock()
	if st.InstallID == "" {
		st, _ = s.Update(func(*State) {})
	}
	return st
}

// Submission is the submission id Reserve chose for fp: the one an
// interrupted upload of it used, or a new one.
func (s *Store) Submission(fp string) string { return s.Get().Subs[fp] }

// Release ends this run's hold on fp without marking its upload finished:
// a later report of it goes again, with the same submission id.
func (s *Store) Release(fp string) {
	s.mu.Lock()
	delete(s.inflight, fp)
	s.mu.Unlock()
}

// Finished records that the upload reserved for fp is over, sent or not.
func (s *Store) Finished(fp string, now time.Time) {
	_, _ = s.Update(func(st *State) {
		delete(s.inflight, fp)
		if _, ok := st.Sent[fp]; ok {
			st.Tried[fp] = now.Unix()
		}
		delete(st.Subs, fp)
	})
}

// Verdict is TryReserve's answer.
type Verdict int

const (
	Reserved Verdict = iota // go ahead and send
	Declined                // not to be sent: switched off, or sent lately
	Later                   // not now, worth another go: daily cap, or state not saved
)

// Reserve records an automatic report of fp at now, or says no when the
// switch is off or the limits are reached. Recording before sending means a
// relay that is down costs one attempt, not one per error.
//
// A reservation that cannot be saved is refused: with an unwritable state
// file the limits could not hold, and the app would report the same error
// again and again.
//
// A fingerprint reserved by an earlier run whose upload never finished (the
// app quit first) is let through again: Finished was never recorded for it.
func (s *Store) Reserve(fp string, now time.Time) bool { return s.TryReserve(fp, now) == Reserved }

// TryReserve is Reserve, saying why it refuses.
func (s *Store) TryReserve(fp string, now time.Time) Verdict {
	ok := false
	why := Declined
	_, err := s.Update(func(st *State) {
		if st.AutoDisabled {
			return
		}
		day := now.Format("2006-01-02")
		if st.Day != day {
			st.Day, st.Count = day, 0
		}
		last, seen := st.Sent[fp]
		finished := st.Tried[fp] >= last
		if s.inflight[fp] || seen && finished && now.Sub(time.Unix(last, 0)) < SameErrorEvery {
			return
		}
		if st.Count >= MaxAutoPerDay {
			why = Later
			return
		}
		for k, t := range st.Sent { // forget old fingerprints
			if now.Sub(time.Unix(t, 0)) > 7*24*time.Hour {
				delete(st.Sent, k)
				delete(st.Tried, k)
				delete(st.Subs, k)
			}
		}
		if !seen || finished || st.Subs[fp] == "" {
			st.Subs[fp] = newID()
		}
		st.Sent[fp] = now.Unix()
		st.Count++
		if s.inflight == nil {
			s.inflight = map[string]bool{}
		}
		s.inflight[fp] = true
		ok = true
	})
	switch {
	case err != nil:
		if ok {
			s.mu.Lock()
			delete(s.inflight, fp)
			s.mu.Unlock()
		}
		return Later // the limits could not be recorded
	case ok:
		return Reserved
	}
	return why
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

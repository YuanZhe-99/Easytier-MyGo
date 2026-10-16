package daemon

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/health"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/supervisor"
)

var scrubDetail = supervisor.ScrubDetail

// eventStore keeps the durable health timeline: a JSONL file in the state
// directory plus an in-memory ring for the API. Records are one line per
// event; the file rotates at a size cap so an unread timeline cannot grow
// without bound. Details are already scrubbed by the engine's callers.
type eventStore struct {
	mu   sync.Mutex
	path string
	ring []health.Event
	max  int
	// seq counts appended events, so sessions can push only new ones.
	seq          uint64
	persistError string
}

const (
	eventRingMax  = 200
	eventFileMax  = 2 << 20 // 2 MiB, then rotate to events.jsonl.1
	eventFileScan = 1 << 20
)

func newEventStore(dir string) (*eventStore, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &eventStore{path: filepath.Join(dir, "events.jsonl"), max: eventRingMax}
	s.loadRecent()
	return s, nil
}

// appendEvent is the engine's event sink.
func (s *eventStore) appendEvent(ev health.Event) {
	ev.Detail = scrubDetail(ev.Detail)
	ev.Reason = scrubDetail(ev.Reason)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	s.ring = append(s.ring, ev)
	if len(s.ring) > s.max {
		s.ring = s.ring[len(s.ring)-s.max:]
	}

	if info, err := os.Stat(s.path); err == nil && info.Size() > eventFileMax {
		if err := os.Rename(s.path, s.path+".1"); err != nil {
			s.persistError = "event log rotation failed; recent events are memory-only"
			return
		}
	}
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		s.persistError = "event log persistence failed; recent events may be memory-only"
		return
	}
	defer f.Close()
	b, err := json.Marshal(ev)
	if err != nil {
		return
	}
	_, err = f.Write(append(b, '\n'))
	if err == nil {
		err = f.Sync()
	}
	if err != nil {
		s.persistError = "event log persistence failed; recent events may be memory-only"
	} else {
		s.persistError = ""
	}
}

func (s *eventStore) persistenceError() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.persistError
}

// since returns the events appended after sequence seq (bounded by the
// ring) and the current sequence.
func (s *eventStore) since(seq uint64) ([]health.Event, uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if seq >= s.seq {
		return nil, s.seq
	}
	n := int(s.seq - seq)
	if n > len(s.ring) {
		n = len(s.ring)
	}
	return append([]health.Event(nil), s.ring[len(s.ring)-n:]...), s.seq
}

// recent returns the newest n events, oldest first.
func (s *eventStore) recent(n int) []health.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n <= 0 || n > len(s.ring) {
		n = len(s.ring)
	}
	out := make([]health.Event, n)
	copy(out, s.ring[len(s.ring)-n:])
	return out
}

// loadRecent restores the ring from the log so a daemon restart shows the
// timeline that accumulated while it was down (worst case: the previous
// process was killed, and the last events are already on disk).
func (s *eventStore) loadRecent() {
	f, err := os.Open(s.path)
	if err != nil {
		if !os.IsNotExist(err) {
			s.persistError = "event log reload failed; prior events unavailable"
		}
		return
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), eventFileScan)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev health.Event
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		ev.Detail = scrubDetail(ev.Detail)
		ev.Reason = scrubDetail(ev.Reason)
		s.ring = append(s.ring, ev)
		if len(s.ring) > s.max {
			s.ring = s.ring[len(s.ring)-s.max:]
		}
	}
	if scanner.Err() != nil {
		s.persistError = "event log reload incomplete; prior events may be unavailable"
	}
}

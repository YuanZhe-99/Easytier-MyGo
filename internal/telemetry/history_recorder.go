package telemetry

import (
	"time"
)

// HistoryRecorder consumes raw samples independently of the display curve.
// Missing samples discard baselines: the next counters cannot retroactively
// turn explicitly missing time into measured coverage.
type HistoryRecorder struct {
	History    *History
	tracker    *Tracker
	interval   time.Duration
	profile    string
	last       time.Time
	generation string
	sequence   uint64
}

func NewHistoryRecorder(profile string, interval time.Duration, h *History) *HistoryRecorder {
	var sequence uint64
	for _, bucket := range h.Buckets {
		if bucket.Generation > sequence {
			sequence = bucket.Generation
		}
	}
	return &HistoryRecorder{History: h, profile: profile, interval: interval, tracker: NewTracker(interval, 1), last: h.lastEnd, sequence: sequence}
}

func (r *HistoryRecorder) Observe(s Sample) error {
	if r.generation != s.Generation {
		r.sequence++
		r.generation = s.Generation
		r.tracker = NewTracker(r.interval, 1)
	}
	if r.last.IsZero() {
		r.last = s.Time
		r.tracker.Observe(s)
		return nil
	}
	if !s.Time.After(r.last) {
		// A backwards wall clock cannot overlap persisted intervals. Keep the
		// recorded endpoint and start fresh only after time catches up.
		r.tracker = NewTracker(r.interval, 1)
		return nil
	}
	point, ok := r.tracker.Observe(s)
	gap := s.Missing || !ok || point.Gap
	if s.Missing {
		r.tracker = NewTracker(r.interval, 1)
	}
	err := r.History.Add(r.profile, r.sequence, r.last, s.Time, point.RxBytesPS, point.TxBytesPS, gap, point.Partial)
	if err == nil {
		r.last = s.Time
	}
	return err
}

package telemetry

import (
	"errors"
	"math"
	"time"
)

// HistoryBucket stores measured bytes separately from missing time. It never
// implies that missing intervals carried zero traffic.
type HistoryBucket struct {
	Start          time.Time `json:"start"`
	ProfileID      string    `json:"profile_id"`
	Generation     uint64    `json:"generation"`
	RxBytes        float64   `json:"rx_bytes"`
	TxBytes        float64   `json:"tx_bytes"`
	CoveredSeconds float64   `json:"covered_seconds"`
	GapSeconds     float64   `json:"gap_seconds"`
	Partial        bool      `json:"partial,omitempty"`
	Boundary       string    `json:"boundary"`
}

type History struct {
	Width      time.Duration
	MaxBuckets int
	Buckets    []HistoryBucket
	lastEnd    time.Time
}

// Clone snapshots history for persistence outside the collector's lock.
func (h *History) Clone() *History {
	copy := *h
	copy.Buckets = append([]HistoryBucket(nil), h.Buckets...)
	return &copy
}

// Add consumes an explicit non-overlapping interval. Splitting at bucket edges
// apportions a measured average over its actual interval; gaps have no byte value.
func (h *History) Add(profile string, generation uint64, from, to time.Time, rx, tx float64, gap, partial bool) error {
	if h.Width < time.Minute || h.Width > 24*time.Hour || h.MaxBuckets < 1 || h.MaxBuckets > 10080 || profile == "" || !to.After(from) {
		return errors.New("invalid history interval or limits")
	}
	if math.IsNaN(rx) || math.IsNaN(tx) || math.IsInf(rx, 0) || math.IsInf(tx, 0) || rx < 0 || tx < 0 {
		return errors.New("invalid history rate")
	}
	if !h.lastEnd.IsZero() && from.Before(h.lastEnd) {
		return errors.New("history intervals overlap or move backwards")
	}
	// Bound work even after a long suspend: retain only the newest window.
	oldest := to.Truncate(h.Width).Add(-time.Duration(h.MaxBuckets-1) * h.Width)
	if from.Before(oldest) {
		from = oldest
	}
	for from.Before(to) {
		start := from.Truncate(h.Width)
		end := start.Add(h.Width)
		if end.After(to) {
			end = to
		}
		seconds := end.Sub(from).Seconds()
		index := -1
		for i := len(h.Buckets) - 1; i >= 0; i-- {
			b := h.Buckets[i]
			if b.Start.Equal(start) && b.ProfileID == profile && b.Generation == generation {
				index = i
				break
			}
		}
		if index < 0 {
			h.Buckets = append(h.Buckets, HistoryBucket{Start: start, ProfileID: profile, Generation: generation, Boundary: "easytier tunnel connection counters"})
			index = len(h.Buckets) - 1
		}
		b := &h.Buckets[index]
		if gap {
			b.GapSeconds += seconds
		} else {
			b.CoveredSeconds += seconds
			b.RxBytes += rx * seconds
			b.TxBytes += tx * seconds
		}
		b.Partial = b.Partial || partial
		from = end
	}
	if len(h.Buckets) > h.MaxBuckets {
		h.Buckets = append([]HistoryBucket(nil), h.Buckets[len(h.Buckets)-h.MaxBuckets:]...)
	}
	h.lastEnd = to
	return nil
}

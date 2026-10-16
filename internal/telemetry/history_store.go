package telemetry

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"time"
)

const historyFileLimit = 2 * 1024 * 1024

type storedHistory struct {
	Version      int             `json:"version"`
	WidthSeconds int64           `json:"width_seconds"`
	MaxBuckets   int             `json:"max_buckets"`
	LastEnd      time.Time       `json:"last_end"`
	Buckets      []HistoryBucket `json:"buckets"`
}

func (h *History) validate() error {
	if h.Width < time.Minute || h.Width > 24*time.Hour || h.Width%time.Second != 0 || h.MaxBuckets < 1 || h.MaxBuckets > 10080 || len(h.Buckets) > h.MaxBuckets {
		return errors.New("invalid history limits")
	}
	var previous time.Time
	for _, b := range h.Buckets {
		if b.Start.IsZero() || !b.Start.Equal(b.Start.Truncate(h.Width)) || b.Start.Before(previous) || b.ProfileID == "" || b.Boundary != "easytier tunnel connection counters" {
			return errors.New("invalid history bucket identity or order")
		}
		for _, v := range []float64{b.RxBytes, b.TxBytes, b.CoveredSeconds, b.GapSeconds} {
			if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
				return errors.New("invalid history bucket measurement")
			}
		}
		if b.CoveredSeconds+b.GapSeconds > h.Width.Seconds()+1e-6 || (b.CoveredSeconds == 0 && (b.RxBytes != 0 || b.TxBytes != 0)) {
			return errors.New("history coverage exceeds bucket or unmeasured bytes")
		}
		if h.lastEnd.IsZero() || !h.lastEnd.After(b.Start) {
			return errors.New("invalid history endpoint")
		}
		previous = b.Start
	}
	return nil
}

// Save persists a bounded file atomically. The caller supplies the profile's
// confined path and serializes access; this prototype does not enable logging.
func (h *History) Save(path string) error {
	if err := h.validate(); err != nil {
		return err
	}
	encode := func(start int) ([]byte, error) {
		return json.Marshal(storedHistory{1, int64(h.Width / time.Second), h.MaxBuckets, h.lastEnd, h.Buckets[start:]})
	}
	b, err := encode(0)
	if err != nil {
		return err
	}
	if len(b) > historyFileLimit {
		// Bound serialized size as well as record count. Find the oldest
		// record we can retain without quadratic repeated re-encoding.
		low, high := 0, len(h.Buckets)
		for low < high {
			mid := (low + high) / 2
			candidate, err := encode(mid)
			if err != nil {
				return err
			}
			if len(candidate) > historyFileLimit {
				low = mid + 1
			} else {
				high = mid
			}
		}
		b, err = encode(low)
		if err != nil {
			return err
		}
		if len(b) > historyFileLimit {
			return errors.New("history metadata exceeds two MiB limit")
		}
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".history-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0o600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func LoadHistory(path string) (*History, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, historyFileLimit+1))
	if err != nil {
		return nil, err
	}
	if len(b) > historyFileLimit {
		return nil, errors.New("history exceeds two MiB limit")
	}
	var stored storedHistory
	if err := json.Unmarshal(b, &stored); err != nil {
		return nil, err
	}
	if stored.Version != 1 || stored.WidthSeconds < 60 || stored.WidthSeconds > 86400 {
		return nil, errors.New("unsupported history schema or width")
	}
	h := &History{Width: time.Duration(stored.WidthSeconds) * time.Second, MaxBuckets: stored.MaxBuckets, Buckets: stored.Buckets, lastEnd: stored.LastEnd}
	if err := h.validate(); err != nil {
		return nil, err
	}
	return h, nil
}

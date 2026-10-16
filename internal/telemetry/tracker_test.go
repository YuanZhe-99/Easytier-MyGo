package telemetry

import (
	"testing"
	"time"
)

func sample(at time.Time, gen string, rx, tx uint64) Sample {
	return Sample{Time: at, Generation: gen, Conns: []ConnSample{{ConnID: "c1", RxBytes: rx, TxBytes: tx}}}
}

// TestTrackerRates covers the reading rules: rates over real elapsed
// time, gaps for missing rounds, and no spikes after a counter reset or a
// restart (new generation).
func TestTrackerRates(t *testing.T) {
	t0 := time.Unix(1_000_000, 0)
	tr := NewTracker(time.Second, 16)

	if _, ok := tr.Observe(sample(t0, "g1", 1000, 500)); ok {
		t.Fatal("the first sample only sets the baseline")
	}
	p, ok := tr.Observe(sample(t0.Add(2*time.Second), "g1", 3000, 1500))
	if !ok || p.Gap || p.RxBytesPS != 1000 || p.TxBytesPS != 500 {
		t.Fatalf("rate over 2s: %+v ok=%v", p, ok)
	}

	if p, ok := tr.Observe(Sample{Time: t0.Add(3 * time.Second), Generation: "g1", Missing: true}); !ok || !p.Gap {
		t.Fatalf("missing round should close a gap: %+v ok=%v", p, ok)
	}

	// A counter going backwards (rebuilt connection) must not read as a
	// burst or a negative rate.
	tr.Observe(sample(t0.Add(4*time.Second), "g1", 3500, 1600))
	p, _ = tr.Observe(sample(t0.Add(5*time.Second), "g1", 100, 50))
	if !p.Gap && (p.RxBytesPS < 0 || p.RxBytesPS > 1e6) {
		t.Fatalf("counter reset produced %+v", p)
	}

	// A new generation (core restart) resets every baseline.
	if p, ok := tr.Observe(sample(t0.Add(6*time.Second), "g2", 9_000_000, 9_000_000)); ok && !p.Gap && p.RxBytesPS > 1e6 {
		t.Fatalf("restart produced a spike: %+v", p)
	}
}

func TestTrackerIsBounded(t *testing.T) {
	t0 := time.Unix(1_000_000, 0)
	tr := NewTracker(time.Second, 4)
	for i := 0; i < 20; i++ {
		tr.Observe(sample(t0.Add(time.Duration(i)*time.Second), "g", uint64(i*100), 0))
	}
	if n := len(tr.Points()); n != 4 {
		t.Fatalf("%d points retained, want 4", n)
	}
}

// TestRingIncrementalReads: a reader polling with its last sequence gets
// every new point exactly once, and old points fall out of a full ring.
func TestRingIncrementalReads(t *testing.T) {
	r := NewRing(3)
	if got := r.Since(0); got != nil {
		t.Fatalf("empty ring returned %v", got)
	}
	for i := 0; i < 2; i++ {
		r.Add(Point{RxBytesPS: float64(i)})
	}
	got := r.Since(0)
	if len(got) != 2 || got[0].Seq != 1 || got[1].Seq != 2 {
		t.Fatalf("Since(0) = %+v", got)
	}
	last := got[1].Seq
	for i := 2; i < 6; i++ {
		r.Add(Point{RxBytesPS: float64(i)})
	}
	got = r.Since(last)
	if len(got) != 3 || got[0].Seq != 4 || got[2].Seq != 6 || got[2].RxBytesPS != 5 {
		t.Fatalf("after overflow Since(%d) = %+v", last, got)
	}
	if got := r.Since(6); got != nil {
		t.Fatalf("nothing new expected, got %+v", got)
	}
}

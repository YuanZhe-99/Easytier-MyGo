// Package telemetry turns raw counter samples into rates and short
// in-memory curves. It holds no I/O and no UI: the daemon feeds it
// snapshots and the GUI draws what it keeps.
//
// The counters are the core's per-connection tunnel counters (rx_bytes,
// tx_bytes, packets). Two rules follow from the plan (report §8.2):
//
//  1. A rate is a difference over the real elapsed monotonic time, keyed
//     by the connection's identity (its connection id), and the baseline
//     is dropped whenever a connection disappears or a counter goes
//     backwards (a rebuilt connection restarts its counters). A missing
//     value is recorded as a gap, never drawn as a burst.
//  2. Samples are keyed by instance generation: an instance that restarts
//     makes the previous curve a gap rather than a spike.
package telemetry

import (
	"sort"
	"sync"
	"time"
)

// ConnSample is one connection's counters as observed at Time.
type ConnSample struct {
	// ConnID is the connection's identity ("conn_id" from the core). A
	// change of id means a new connection.
	ConnID string
	// PeerID identifies the remote peer; several connections may share it.
	PeerID    uint32
	RxBytes   uint64
	TxBytes   uint64
	RxPackets uint64
	TxPackets uint64
}

// Sample is one observation round: all connections of one instance.
type Sample struct {
	Time time.Time
	// Generation is the instance's run identifier (config name + start
	// time). A change resets every baseline.
	Generation string
	Conns      []ConnSample
	// Missing marks a round the collector could not deliver: it keeps the
	// timeline but records no rate.
	Missing bool
}

// Rate is one connection's computed throughput over one interval.
type Rate struct {
	ConnID    string
	PeerID    uint32
	RxBytesPS float64
	TxBytesPS float64
	// Gap is true when no trustworthy rate could be computed for this
	// interval (new/vanished connection, counter reset, or a missing
	// round). The point stays on the timeline, but must not be drawn as a
	// value.
	Gap bool
}

// Point is one aggregated interval of the instance's curve.
type Point struct {
	Time        time.Time
	RxBytesPS   float64
	TxBytesPS   float64
	RxPacketsPS float64
	TxPacketsPS float64
	// Gap marks an interval without a trustworthy total.
	Gap bool
	// Partial is true when the interval is shorter than the nominal
	// interval (the first sample, or a stretch after a restart), so a
	// chart should not read it as a full bucket.
	Partial bool
}

// histogram is the ordered window of points for one key.
type keyBaseline struct {
	rx, tx         uint64
	rxPkts, txPkts uint64
	at             time.Time
}

// Tracker computes rates and keeps a bounded number of intervals.
type Tracker struct {
	mu sync.Mutex
	// interval is the nominal bucket size; a longer gap between samples is
	// recorded as a partial interval rather than stretched over.
	interval time.Duration
	// capacity bounds the retained points (a rolling window).
	capacity int

	generation string
	seen       bool
	// baseline holds the last counters per connection.
	baseline map[string]keyBaseline
	// lastTime is when the previous sample was taken (monotonic).
	lastTime time.Time
	// points is the retained curve, oldest first.
	points []Point
}

// NewTracker creates a tracker with the given nominal interval and window.
func NewTracker(interval time.Duration, capacity int) *Tracker {
	if interval <= 0 {
		interval = time.Second
	}
	if capacity <= 0 {
		capacity = 240
	}
	return &Tracker{
		interval: interval,
		capacity: capacity,
		baseline: map[string]keyBaseline{},
	}
}

// Observe feeds one sample and returns the interval it closed (if any).
// The first sample of a generation only establishes baselines and closes
// no interval.
func (t *Tracker) Observe(s Sample) (Point, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// A new instance generation resets every baseline: counters from the
	// previous run are not comparable.
	if t.seen && s.Generation != t.generation {
		t.resetLocked(s.Generation)
	}
	if !t.seen {
		t.generation = s.Generation
		t.seen = true
	}

	if s.Missing {
		// A missing round keeps the timeline but yields no rate. The
		// elapsed clock is NOT advanced: the next good sample must measure
		// the counters over the whole un-sampled stretch, or the bytes of
		// several intervals would be divided by a single one and inflate
		// the rate.
		p := Point{Time: s.Time, Gap: true}
		t.pushLocked(p)
		return p, true
	}

	// First sample of the generation: only seed the baselines.
	if t.lastTime.IsZero() {
		t.rebaselineLocked(s)
		t.lastTime = s.Time
		return Point{}, false
	}

	elapsed := s.Time.Sub(t.lastTime)
	if elapsed <= 0 {
		// Clock went backwards (wall clock jumped): treat as a gap and
		// restart the baseline rather than emit a negative rate.
		t.rebaselineLocked(s)
		t.lastTime = s.Time
		gap := Point{Time: s.Time, Gap: true}
		t.pushLocked(gap)
		return gap, true
	}
	if elapsed > t.interval*maxStretchFactor+testSlack(t.interval) {
		// The stretch is far longer than the cadence (a sleep, a suspend,
		// or a long period with no subscriber): averaging it would present
		// a blank stretch as a low steady flow, which the plan forbids.
		// Record a gap and start a fresh baseline instead.
		t.rebaselineLocked(s)
		t.lastTime = s.Time
		gap := Point{Time: s.Time, Gap: true}
		t.pushLocked(gap)
		return gap, true
	}

	seconds := elapsed.Seconds()
	point := Point{Time: s.Time}
	// A stretch longer than the nominal interval is marked partial so a
	// chart does not read a low rate as a quiet period.
	if elapsed > t.interval+testSlack(t.interval) {
		point.Partial = true
	}

	if len(t.baseline) == 0 {
		t.rebaselineLocked(s)
		t.lastTime = s.Time
		point.Gap = true
		t.pushLocked(point)
		return point, true
	}

	// Sum the deltas of the connections that are present in both samples
	// and whose counters did not go backwards. A counter that reset is
	// skipped for this interval and re-baselined.
	var rx, tx, rxPkts, txPkts float64
	tracked := 0
	next := make(map[string]keyBaseline, len(s.Conns))
	for _, c := range s.Conns {
		prev, ok := t.baseline[c.ConnID]
		if !ok {
			// A connection that just appeared: no baseline, skip this
			// interval.
			next[c.ConnID] = baselinesOf(c, s.Time)
			continue
		}
		if c.RxBytes < prev.rx || c.TxBytes < prev.tx || c.RxPackets < prev.rxPkts || c.TxPackets < prev.txPkts {
			// The connection was rebuilt between samples: its counters
			// restarted. Skip this interval for it.
			next[c.ConnID] = baselinesOf(c, s.Time)
			continue
		}
		rx += float64(c.RxBytes - prev.rx)
		tx += float64(c.TxBytes - prev.tx)
		rxPkts += float64(c.RxPackets - prev.rxPkts)
		txPkts += float64(c.TxPackets - prev.txPkts)
		tracked++
		next[c.ConnID] = baselinesOf(c, s.Time)
	}
	// A connection that vanished between samples carries no rate; its
	// baseline is dropped by not copying it into next.
	t.baseline = next
	t.lastTime = s.Time

	if tracked == 0 {
		// Nothing to measure: keep the timeline without inventing a zero.
		point.Gap = true
		t.pushLocked(point)
		return point, true
	}

	point.RxBytesPS = rx / seconds
	point.TxBytesPS = tx / seconds
	point.RxPacketsPS = rxPkts / seconds
	point.TxPacketsPS = txPkts / seconds
	t.pushLocked(point)
	return point, true
}

// maxStretchFactor bounds how far an interval may exceed the cadence
// before it stops being a rate and becomes a gap: six intervals cover a
// late round and a short collection outage, while a sleep or a long idle
// stretch is never averaged into a fake low flow.
const maxStretchFactor = 6

// testSlack allows a little scheduling jitter before an interval counts as
// partial.
func testSlack(interval time.Duration) time.Duration {
	slack := interval / 4
	if slack > 500*time.Millisecond {
		slack = 500 * time.Millisecond
	}
	return slack
}

func baselinesOf(c ConnSample, at time.Time) keyBaseline {
	return keyBaseline{rx: c.RxBytes, tx: c.TxBytes, rxPkts: c.RxPackets, txPkts: c.TxPackets, at: at}
}

func (t *Tracker) rebaselineLocked(s Sample) {
	t.baseline = make(map[string]keyBaseline, len(s.Conns))
	for _, c := range s.Conns {
		t.baseline[c.ConnID] = baselinesOf(c, s.Time)
	}
}

func (t *Tracker) resetLocked(generation string) {
	t.generation = generation
	t.baseline = map[string]keyBaseline{}
	t.lastTime = time.Time{}
	// Keep the points: the timeline is continuous, the curve has a gap.
}

func (t *Tracker) pushLocked(p Point) {
	t.points = append(t.points, p)
	if len(t.points) > t.capacity {
		t.points = t.points[len(t.points)-t.capacity:]
	}
}

// Points returns a copy of the retained curve, oldest first.
func (t *Tracker) Points() []Point {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]Point(nil), t.points...)
}

// Latest returns the most recent point and whether one exists.
func (t *Tracker) Latest() (Point, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.points) == 0 {
		return Point{}, false
	}
	return t.points[len(t.points)-1], true
}

// Window returns the points at or after since.
func (t *Tracker) Window(since time.Time) []Point {
	t.mu.Lock()
	defer t.mu.Unlock()
	idx := sort.Search(len(t.points), func(i int) bool { return !t.points[i].Time.Before(since) })
	return append([]Point(nil), t.points[idx:]...)
}

// Interval is the nominal bucket size.
func (t *Tracker) Interval() time.Duration { return t.interval }

package telemetry

import "sync"

// SeqPoint is a curve point with its sequence number. Sequence numbers
// start at 1 and grow by one per point, so a reader that remembers the last
// one it saw gets exactly the new points.
type SeqPoint struct {
	Seq uint64
	Point
}

// Ring keeps the latest points of one curve in a fixed, preallocated
// buffer. It is safe for concurrent use.
type Ring struct {
	mu   sync.Mutex
	buf  []SeqPoint
	next uint64 // sequence of the next point
	n    int    // points held
}

// NewRing returns a ring that holds capacity points.
func NewRing(capacity int) *Ring {
	if capacity < 1 {
		capacity = 1
	}
	return &Ring{buf: make([]SeqPoint, capacity), next: 1}
}

// Add appends a point and returns its sequence number.
func (r *Ring) Add(p Point) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	seq := r.next
	r.buf[int(seq%uint64(len(r.buf)))] = SeqPoint{Seq: seq, Point: p}
	r.next++
	if r.n < len(r.buf) {
		r.n++
	}
	return seq
}

// Last returns the newest point, if any.
func (r *Ring) Last() (SeqPoint, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.n == 0 {
		return SeqPoint{}, false
	}
	return r.buf[int((r.next-1)%uint64(len(r.buf)))], true
}

// Since returns the held points with a sequence greater than seq, oldest
// first. Points that already left the buffer are simply absent.
func (r *Ring) Since(seq uint64) []SeqPoint {
	r.mu.Lock()
	defer r.mu.Unlock()
	first := r.next - uint64(r.n) // oldest held sequence
	if seq+1 > first {
		first = seq + 1
	}
	if first >= r.next {
		return nil
	}
	out := make([]SeqPoint, 0, r.next-first)
	for s := first; s < r.next; s++ {
		out = append(out, r.buf[int(s%uint64(len(r.buf)))])
	}
	return out
}

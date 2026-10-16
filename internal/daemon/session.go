package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"slices"
	"time"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/localapi"
)

// session is one attached GUI. While any session exists, networks the user
// wants connected run; its watch list selects the fast check cadence.
type session struct {
	id    string
	watch []string
}

// pushInterval caps how often a session receives frames.
const pushInterval = 500 * time.Millisecond

// Stream serves a session (localapi.Handler): it registers the session,
// pushes the initial state, then pushes changes until the client leaves.
func (d *Daemon) Stream(ctx context.Context, req localapi.Request, access localapi.Access, send func(localapi.Response) error) {
	var raw [8]byte
	_, _ = rand.Read(raw[:])
	s := &session{id: hex.EncodeToString(raw[:]), watch: req.Watch}
	d.mu.Lock()
	d.sessions[s.id] = s
	d.mu.Unlock()
	d.applyWatch()
	d.reconcile()
	defer func() {
		d.mu.Lock()
		delete(d.sessions, s.id)
		d.mu.Unlock()
		d.applyWatch()
		d.reconcile() // in Follow GUI mode the networks stop with the last session
		d.touch()
	}()

	seqs := map[string]uint64{}
	_, eventSeq := d.events.since(^uint64(0))
	first := true
	ticker := time.NewTicker(pushInterval)
	defer ticker.Stop()
	for {
		push := &localapi.Push{Networks: d.statuses(), Traffic: map[string][]localapi.TrafficPoint{}}
		if first {
			push.Session = s.id
			push.Events = d.events.recent(50)
		} else {
			push.Events, eventSeq = d.events.since(eventSeq)
		}
		d.mu.Lock()
		for id, e := range d.workers {
			if pts := e.w.Traffic(seqs[id]); len(pts) > 0 {
				push.Traffic[id] = pts
				seqs[id] = pts[len(pts)-1].Seq
			}
		}
		d.mu.Unlock()
		if err := send(localapi.Response{OK: true, Push: push}); err != nil {
			return
		}
		first = false
		select {
		case <-ctx.Done():
			return
		case <-d.ctx.Done():
			return
		case <-d.changed:
			// Coalesce bursts of changes into at most one frame per interval.
			select {
			case <-ticker.C:
			case <-ctx.Done():
				return
			}
		case <-ticker.C:
		}
	}
}

// setWatch replaces a session's watch list.
func (d *Daemon) setWatch(id string, watch []string) {
	d.mu.Lock()
	if s := d.sessions[id]; s != nil {
		s.watch = slices.Clone(watch)
	}
	d.mu.Unlock()
	d.applyWatch()
}

// applyWatch gives watched networks the fast cadence.
func (d *Daemon) applyWatch() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for id, e := range d.workers {
		fast := false
		for _, s := range d.sessions {
			fast = fast || slices.Contains(s.watch, id)
		}
		e.w.SetFast(fast)
	}
}

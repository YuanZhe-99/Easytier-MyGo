package localapi

import (
	"context"
	"sort"
	"time"
)

const RequestDiagnosticBundle RequestType = "diagnostics.networks"

// DiagnosticBundle excludes target names/IDs; ordinal distinguishes evidence
// within this reviewed export, not a stable cross-export identity.
type DiagnosticBundle struct {
	Schema    int            `json:"schema"`
	CreatedAt time.Time      `json:"created_at"`
	Scope     string         `json:"scope"`
	Networks  []*Diagnostics `json:"networks"`
}

func DiagnosticNetworks(states map[string]*Status) *DiagnosticBundle {
	bundle := &DiagnosticBundle{Schema: 1, CreatedAt: time.Now(), Scope: "all managed profiles"}
	ids := make([]string, 0, len(states))
	for id := range states {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if len(bundle.Networks) >= 4 {
			break
		}
		if d := DiagnosticSnapshot(states[id]); d != nil {
			bundle.Networks = append(bundle.Networks, d)
		}
	}
	return bundle
}

func (c *Client) DiagnosticNetworks(ctx context.Context) (*DiagnosticBundle, error) {
	r, err := c.roundTrip(ctx, Request{Type: RequestDiagnosticBundle})
	if r == nil {
		return nil, err
	}
	return r.DiagnosticBundle, err
}

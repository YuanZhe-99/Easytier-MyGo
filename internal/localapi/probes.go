package localapi

import (
	"context"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/diagnostics"
)

const RequestProbeGet RequestType = "probes.get"
const RequestProbeSave RequestType = "probes.save"

func (c *Client) ProbeSettings(ctx context.Context) (*diagnostics.Schedule, error) {
	r, err := c.roundTrip(ctx, Request{Type: RequestProbeGet})
	if r == nil {
		return nil, err
	}
	return r.ProbeSchedule, err
}
func (c *Client) SaveProbeSettings(ctx context.Context, s diagnostics.Schedule) error {
	_, err := c.roundTrip(ctx, Request{Type: RequestProbeSave, ProbeSchedule: &s})
	return err
}

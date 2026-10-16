package localapi

import (
	"context"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/telemetry"
)

const RequestHistory RequestType = "traffic.history"
const RequestHistorySave RequestType = "traffic.history.save"
const RequestHistoryDelete RequestType = "traffic.history.delete"

func (c *Client) SetTrafficHistory(ctx context.Context, enabled bool) error {
	_, err := c.roundTrip(ctx, Request{Type: RequestHistorySave, HistoryEnabled: &enabled})
	return err
}
func (c *Client) DeleteTrafficHistory(ctx context.Context) error {
	_, err := c.roundTrip(ctx, Request{Type: RequestHistoryDelete})
	return err
}

type TrafficHistory struct {
	Enabled      bool                      `json:"enabled"`
	Error        string                    `json:"error,omitempty"`
	Buckets      []telemetry.HistoryBucket `json:"buckets,omitempty"`
	WidthSeconds int                       `json:"width_seconds"`
	MaxBuckets   int                       `json:"max_buckets"`
}

func (c *Client) TrafficHistory(ctx context.Context) (*TrafficHistory, error) {
	r, err := c.roundTrip(ctx, Request{Type: RequestHistory})
	if r == nil {
		return nil, err
	}
	return r.History, err
}

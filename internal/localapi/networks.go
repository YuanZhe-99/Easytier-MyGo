package localapi

import "context"

// ForProfile returns an independent client with a fixed target. Capturing this
// client before asynchronous work prevents later GUI selection from retargeting
// a config save or lifecycle command. Global reads/shutdown remain unscoped.
func (c *Client) ForProfile(id string) *Client {
	target := NewClient(c.SocketPath)
	target.Timeout = c.Timeout
	target.profileID = id
	target.SetWantDetails(c.WantDetails())
	target.SetWantTraffic(c.WantTraffic())
	return target
}

func (c *Client) Networks(ctx context.Context) (map[string]*Status, error) {
	r, err := c.roundTrip(ctx, Request{Type: RequestNetworks})
	if r == nil {
		return nil, err
	}
	return r.Networks, err
}

func (c *Client) Target(ctx context.Context, id string, kind RequestType) (*Status, error) {
	r, err := c.roundTrip(ctx, Request{Type: kind, ProfileID: id, ClientID: c.clientID, WantDetails: c.wantDetails.Load(), WantTraffic: c.wantTraffic.Load()})
	if r == nil {
		return nil, err
	}
	return r.Status, err
}

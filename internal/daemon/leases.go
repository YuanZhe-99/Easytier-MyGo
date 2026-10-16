package daemon

import "time"

type watchLease struct {
	Until            time.Time
	Details, Traffic bool
}

func (d *Daemon) updateLease(id string, details, traffic bool, now time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.leases == nil {
		d.leases = make(map[string]watchLease)
	}
	for key, lease := range d.leases {
		if !now.Before(lease.Until) {
			delete(d.leases, key)
		}
	}
	if id == "" {
		id = "legacy"
	}
	if len(id) > 128 {
		return
	}
	if !details && !traffic {
		delete(d.leases, id)
	} else if _, ok := d.leases[id]; ok || len(d.leases) < 128 {
		d.leases[id] = watchLease{now.Add(d.opts.ClientWindow), details, traffic}
	}
	var wantDetails, wantTraffic bool
	for _, lease := range d.leases {
		wantDetails = wantDetails || lease.Details
		wantTraffic = wantTraffic || lease.Traffic
	}
	d.wantDetails.Store(wantDetails)
	d.wantTraffic.Store(wantTraffic)
}

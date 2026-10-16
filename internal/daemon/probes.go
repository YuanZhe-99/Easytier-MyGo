package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/diagnostics"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/localapi"
)

func loadProbeSchedule(stateDir string) (diagnostics.Schedule, error) {
	s := diagnostics.Schedule{IntervalSeconds: 60}
	b, err := os.ReadFile(filepath.Join(stateDir, "probes.json"))
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return s, err
	}
	return s, s.Validate()
}

func (d *Daemon) runProbes(ctx context.Context) {
	for ctx.Err() == nil {
		d.probeSettingsMu.Lock()
		schedule, err := loadProbeSchedule(d.opts.StateDir)
		// Drain notifications already represented by this load while holding
		// the save lock; a later save necessarily queues another notification.
		select {
		case <-d.probeChanged:
		default:
		}
		d.probeSettingsMu.Unlock()
		if err != nil {
			schedule = diagnostics.Schedule{IntervalSeconds: 60}
			d.opts.Logf("daemon: optional probes disabled: invalid settings")
		}
		batchCtx, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() {
			defer close(done)
			_ = diagnostics.RunSchedule(batchCtx, schedule, func(results []diagnostics.Result) {
				d.mu.Lock()
				d.probeResults = results
				d.mu.Unlock()
			})
		}()
		select {
		case <-ctx.Done():
			cancel()
			<-done
			return
		case <-d.probeChanged:
			cancel()
			<-done
		}
		d.mu.Lock()
		d.probeResults = nil
		d.mu.Unlock()
	}
}

func (d *Daemon) handleProbeSettings(req localapi.Request) localapi.Response {
	d.probeSettingsMu.Lock()
	defer d.probeSettingsMu.Unlock()
	r := localapi.Response{Version: localapi.ProtocolVersion}
	if req.Type == localapi.RequestProbeSave {
		if req.ProbeSchedule == nil {
			r.Error = "probe settings required"
			return r
		}
		schedule := *req.ProbeSchedule
		if err := schedule.Validate(); err != nil {
			r.Error = err.Error()
			return r
		}
		b, err := json.Marshal(schedule)
		if err == nil {
			err = atomicState(filepath.Join(d.opts.StateDir, "probes.json"), b)
		}
		if err != nil {
			r.Error = "cannot persist probe settings"
			return r
		}
		select {
		case d.probeChanged <- struct{}{}:
		default:
		}
		r.OK = true
		return r
	}
	schedule, err := loadProbeSchedule(d.opts.StateDir)
	if err != nil {
		r.Error = "invalid probe settings; save valid settings to repair"
		return r
	}
	r.OK = true
	r.ProbeSchedule = &schedule
	return r
}

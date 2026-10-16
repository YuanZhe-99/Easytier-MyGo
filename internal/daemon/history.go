package daemon

import (
	"context"
	"encoding/json"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/localapi"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/telemetry"
	"os"
	"path/filepath"
	"time"
)

type historySettings struct {
	Enabled bool `json:"enabled"`
}

func (d *Daemon) historyResponse() localapi.Response {
	d.operationMu.Lock()
	defer d.operationMu.Unlock()
	result := &localapi.TrafficHistory{Enabled: d.history != nil, Error: d.historyError, WidthSeconds: 60, MaxBuckets: 10080}
	if d.history != nil {
		result.Buckets = d.history.History.Clone().Buckets
	}
	return localapi.Response{Version: localapi.ProtocolVersion, OK: true, History: result}
}

// Explicit opt-in, fixed one-minute buckets / seven days / two MiB. History
// observes the existing idle/visible cadence and never raises collection demand.
func (d *Daemon) initTrafficHistory() error {
	b, err := os.ReadFile(filepath.Join(d.opts.StateDir, "history-settings.json"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var settings historySettings
	if err := json.Unmarshal(b, &settings); err != nil {
		return err
	}
	if !settings.Enabled {
		return nil
	}
	h, err := telemetry.LoadHistory(filepath.Join(d.opts.StateDir, "traffic-history.json"))
	if os.IsNotExist(err) {
		h = &telemetry.History{Width: time.Minute, MaxBuckets: 10080}
	} else if err != nil {
		return err
	}
	d.history = telemetry.NewHistoryRecorder(filepath.Base(d.opts.StateDir), d.opts.CheckInterval, h)
	return nil
}

func (d *Daemon) observeTraffic(s telemetry.Sample) {
	d.traffic.Observe(s)
	if d.history == nil {
		return
	}
	if err := d.history.Observe(s); err != nil {
		d.historyError = "traffic history interval rejected"
		return
	}
}

func (d *Daemon) runHistoryPersistence(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	save := func() {
		d.historyPersistMu.Lock()
		defer d.historyPersistMu.Unlock()
		d.operationMu.Lock()
		if d.history == nil {
			d.operationMu.Unlock()
			return
		}
		h := d.history.History.Clone()
		d.operationMu.Unlock()
		err := h.Save(filepath.Join(d.opts.StateDir, "traffic-history.json"))
		d.operationMu.Lock()
		if err != nil {
			d.historyError = "traffic history persistence failed"
		} else {
			d.historyError = ""
		}
		d.operationMu.Unlock()
	}
	for {
		select {
		case <-ctx.Done():
			save()
			return
		case <-ticker.C:
			save()
		}
	}
}

// Serialize filesystem operations before the collector lock; this is also the
// persistence lock order, so delete cannot race a cloned background write.
func (d *Daemon) handleHistoryControl(req localapi.Request) localapi.Response {
	d.historyPersistMu.Lock()
	defer d.historyPersistMu.Unlock()
	d.operationMu.Lock()
	defer d.operationMu.Unlock()
	r := localapi.Response{Version: localapi.ProtocolVersion}
	settingsPath := filepath.Join(d.opts.StateDir, "history-settings.json")
	historyPath := filepath.Join(d.opts.StateDir, "traffic-history.json")
	if req.Type == localapi.RequestHistoryDelete {
		// Delete while disabled, explicitly preserving the health event log.
		if d.history != nil {
			r.Error = "disable traffic history before deleting"
			return r
		}
		if err := os.Remove(historyPath); err != nil && !os.IsNotExist(err) {
			r.Error = "cannot delete traffic history"
			return r
		}
		d.historyError = ""
		r.OK = true
		return r
	}
	if req.HistoryEnabled == nil {
		r.Error = "history enabled setting required"
		return r
	}
	enabled := *req.HistoryEnabled
	if enabled && d.history == nil {
		h, err := telemetry.LoadHistory(historyPath)
		if os.IsNotExist(err) {
			h = &telemetry.History{Width: time.Minute, MaxBuckets: 10080}
		} else if err != nil {
			r.Error = "stored history invalid; disable and delete it explicitly to repair"
			return r
		}
		b, _ := json.Marshal(historySettings{Enabled: true})
		if err := atomicState(settingsPath, b); err != nil {
			r.Error = "cannot persist history settings"
			return r
		}
		d.history = telemetry.NewHistoryRecorder(filepath.Base(d.opts.StateDir), d.opts.CheckInterval, h)
		d.historyError = ""
	} else if !enabled {
		// Save available buckets before disable; report failure rather than
		// silently dropping data or pretending the setting changed.
		if d.history != nil {
			if err := d.history.History.Save(historyPath); err != nil {
				r.Error = "cannot save history before disabling"
				return r
			}
		}
		b, _ := json.Marshal(historySettings{Enabled: false})
		if err := atomicState(settingsPath, b); err != nil {
			r.Error = "cannot persist history settings"
			return r
		}
		d.history = nil
	}
	r.OK = true
	return r
}

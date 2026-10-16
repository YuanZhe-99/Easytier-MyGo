package diagnostics

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"time"
)

// Schedule is opt-in. Targets are local settings, excluded from diagnostic exports.
type Schedule struct {
	Enabled         bool     `json:"enabled"`
	IntervalSeconds int      `json:"interval_seconds"`
	Targets         []Target `json:"targets"`
}

func (s Schedule) Validate() error {
	if s.IntervalSeconds < 30 || s.IntervalSeconds > 3600 {
		return errors.New("probe interval must be 30–3600 seconds")
	}
	if len(s.Targets) > 8 {
		return errors.New("at most eight probe targets are allowed")
	}
	for _, target := range s.Targets {
		if target.Timeout < 0 || target.Timeout > 5*time.Second {
			return errors.New("probe timeout must not exceed five seconds")
		}
		switch target.Protocol {
		case "tcp":
			host, port, err := net.SplitHostPort(target.Address)
			n, parseErr := strconv.ParseUint(port, 10, 16)
			if err != nil || parseErr != nil || n == 0 || host == "" || strings.ContainsAny(target.Address, "/@\n\r") {
				return errors.New("invalid TCP probe target")
			}
		case "dns":
			if target.Address == "" || len(target.Address) > 253 || strings.ContainsAny(target.Address, "/@:\n\r \t") {
				return errors.New("invalid DNS probe target")
			}
		default:
			return errors.New("only TCP and DNS probes are supported")
		}
	}
	return nil
}

// RunSchedule is independent of the health loop. Each batch has bounded work;
// cancellation ends it before returning. No goroutine or timer is created when off.
func RunSchedule(ctx context.Context, schedule Schedule, publish func([]Result)) error {
	if err := schedule.Validate(); err != nil {
		return err
	}
	if !schedule.Enabled {
		return nil
	}
	runner := NewRunner()
	ticker := time.NewTicker(time.Duration(schedule.IntervalSeconds) * time.Second)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return nil
		}
		results := make([]Result, 0, len(schedule.Targets))
		for _, target := range schedule.Targets {
			if ctx.Err() != nil {
				return nil
			}
			results = append(results, runner.Run(ctx, target))
		}
		if ctx.Err() != nil {
			return nil
		}
		publish(results)
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

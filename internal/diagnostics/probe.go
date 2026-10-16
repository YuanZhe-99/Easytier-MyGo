// Package diagnostics provides optional bounded reachability checks. Results
// describe target reachability only; they never authorize core recovery.
package diagnostics

import (
	"context"
	"errors"
	"net"
	"strings"
	"time"
)

type Target struct {
	Protocol string        `json:"protocol"`
	Address  string        `json:"address"`
	Timeout  time.Duration `json:"-"`
}

// Runner bounds optional probes independently of the health collector.
type Runner struct{ slots chan struct{} }

func NewRunner() *Runner { return &Runner{slots: make(chan struct{}, 2)} }
func (r *Runner) Run(ctx context.Context, target Target) Result {
	select {
	case r.slots <- struct{}{}:
		defer func() { <-r.slots }()
		return Probe(ctx, target)
	case <-ctx.Done():
		return Result{Target: target, StartedAt: time.Now(), Path: "unverified", Failure: "cancelled while queued"}
	default:
		return Result{Target: target, StartedAt: time.Now(), Path: "unverified", Failure: "probe concurrency limit reached"}
	}
}

type Result struct {
	Target     Target    `json:"target"`
	StartedAt  time.Time `json:"started_at"`
	DurationMS int64     `json:"duration_ms"`
	Reachable  bool      `json:"reachable"`
	Source     string    `json:"source,omitempty"`
	Path       string    `json:"path"`
	Failure    string    `json:"failure,omitempty"`
}

// Probe never claims tunnel traversal: routing is controlled by the host OS.
func Probe(ctx context.Context, target Target) Result {
	result := Result{Target: target, StartedAt: time.Now(), Path: "host routing; tunnel path unverified"}
	deadline := target.Timeout
	if deadline <= 0 || deadline > 5*time.Second {
		deadline = 3 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	var err error
	switch target.Protocol {
	case "tcp":
		if strings.ContainsAny(target.Address, "/@") {
			result.Failure = "invalid TCP address"
			break
		}
		if _, _, err = net.SplitHostPort(target.Address); err != nil {
			result.Failure = "invalid TCP address"
			break
		}
		var conn net.Conn
		conn, err = (&net.Dialer{}).DialContext(ctx, "tcp", target.Address)
		if err == nil {
			result.Source = conn.LocalAddr().String()
			conn.Close()
			result.Reachable = true
		}
	case "dns":
		if target.Address == "" || len(target.Address) > 253 {
			result.Failure = "invalid DNS name"
			break
		}
		var addresses []string
		addresses, err = net.DefaultResolver.LookupHost(ctx, target.Address)
		result.Reachable = err == nil && len(addresses) > 0
	case "icmp":
		result.Failure = "ICMP path and privilege support not implemented"
	default:
		result.Failure = "unsupported probe protocol"
	}
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || ctx.Err() == context.DeadlineExceeded {
			result.Failure = "timeout"
		} else if errors.Is(err, context.Canceled) {
			result.Failure = "cancelled"
		} else {
			result.Failure = "target unreachable"
		}
	}
	result.DurationMS = time.Since(result.StartedAt).Milliseconds()
	return result
}

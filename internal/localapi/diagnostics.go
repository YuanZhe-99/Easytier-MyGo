package localapi

import (
	"context"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/health"
	"time"
)

func (c *Client) Diagnostics(ctx context.Context) (*Diagnostics, error) {
	response, err := c.roundTrip(ctx, Request{Type: RequestDiagnostics})
	if response == nil {
		return nil, err
	}
	return response.Diagnostics, err
}

// Diagnostics is an allowlisted, bounded export; free-form upstream strings,
// config paths, raw node data and core logs are deliberately absent.
type Diagnostics struct {
	EventPersistenceErrorPresent bool               `json:"event_persistence_error_present"`
	Schema                       int                `json:"schema"`
	CreatedAt                    time.Time          `json:"created_at"`
	Version                      string             `json:"version"`
	Mode                         string             `json:"mode"`
	Policy                       PolicyInfo         `json:"policy"`
	Round                        int64              `json:"round"`
	CheckedAt                    time.Time          `json:"checked_at"`
	NodeSampledAt                time.Time          `json:"node_sampled_at"`
	PeerSampledAt                time.Time          `json:"peer_sampled_at"`
	TrafficSampledAt             time.Time          `json:"traffic_sampled_at"`
	Health                       []DiagnosticHealth `json:"health"`
	Events                       []DiagnosticEvent  `json:"events"`
	CollectorErrorPresent        bool               `json:"collector_error_present"`
	ActivationErrorPresent       bool               `json:"activation_error_present"`
}

type DiagnosticHealth struct {
	Kind      health.Kind          `json:"kind"`
	Desired   health.Desired       `json:"desired"`
	Lifecycle health.Lifecycle     `json:"lifecycle"`
	Level     health.Level         `json:"level"`
	Cause     health.Cause         `json:"cause"`
	Recovery  health.RecoveryState `json:"recovery"`
	Attempts  int                  `json:"attempts"`
}

type DiagnosticEvent struct {
	Time          time.Time        `json:"time"`
	Kind          health.EventKind `json:"kind"`
	ComponentKind health.Kind      `json:"component_kind"`
	Cause         health.Cause     `json:"cause"`
	Attempt       int              `json:"attempt"`
}

func DiagnosticSnapshot(s *Status) *Diagnostics {
	if s == nil {
		return nil
	}
	d := &Diagnostics{Schema: 1, CreatedAt: time.Now(), Version: s.Daemon.Version, Mode: s.Daemon.Mode, Policy: s.Daemon.Policy, Round: s.Round, CheckedAt: s.CheckedAt, NodeSampledAt: s.NodeSampledAt, PeerSampledAt: s.PeerSampledAt, TrafficSampledAt: s.TrafficSampledAt, CollectorErrorPresent: s.CollectorError != "", ActivationErrorPresent: s.ActivationError != ""}
	d.EventPersistenceErrorPresent = s.EventPersistenceError != ""
	for i, state := range s.Health {
		if i >= 128 {
			break
		}
		d.Health = append(d.Health, DiagnosticHealth{state.Component.Kind, state.Desired, state.Lifecycle, state.Level, state.Cause, state.Recovery, state.AttemptsInWindow})
	}
	events := s.RecentEvents
	if len(events) > 200 {
		events = events[len(events)-200:]
	}
	for _, event := range events {
		d.Events = append(d.Events, DiagnosticEvent{event.Time, event.Kind, event.Component.Kind, event.Cause, event.Attempt})
	}
	return d
}

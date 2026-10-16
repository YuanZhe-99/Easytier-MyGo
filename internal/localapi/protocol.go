// Package localapi is the protocol between the GUI and the local
// management daemon. The daemon owns the health loop, recovery and the
// durable event log; the GUI reads snapshots and sends limited commands.
//
// Transport: one Unix domain socket (a named pipe is the Windows
// counterpart, later), newline-delimited JSON requests and responses.
// Every request names the protocol version; a mismatch fails instead of
// guessing.
package localapi

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/coreadapter"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/diagnostics"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/health"
)

// ProtocolVersion is bumped when request or response shapes change
// incompatibly.
const ProtocolVersion = 1

// SocketEnv overrides the socket path (tests, development).
const SocketEnv = "EASYTIER_MYGO_SOCKET"

// DefaultSocketPath is the per-user daemon socket. A machine service uses
// its own system path instead (later milestone).
func DefaultSocketPath() string {
	if p := os.Getenv(SocketEnv); p != "" {
		return p
	}
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "easytier-mygo", "ctl.sock")
}

// RequestType enumerates the daemon's commands.
type RequestType string

const (
	// RequestStatus reads the current snapshot.
	RequestStatus      RequestType = "status"
	RequestDiagnostics RequestType = "diagnostics"
	RequestNetworks    RequestType = "networks.status"
	// RequestProfileRename changes a manifest profile's durable display name.
	RequestProfileRename RequestType = "profile.rename"
	RequestLoggingGet    RequestType = "logging.get"
	RequestLoggingSave   RequestType = "logging.save"
	RequestLoggingCheck  RequestType = "logging.check"
	// RequestCheck runs one check round now.
	RequestCheck RequestType = "check"
	// RequestCoreStart starts the managed core.
	RequestCoreStart RequestType = "core.start"
	// RequestCoreStop stops the managed core at the user's request.
	RequestCoreStop RequestType = "core.stop"
	// RequestGrantRetry clears a component's recovery budget after a
	// manual decision.
	RequestGrantRetry RequestType = "recovery.grant"
	// RequestConfigGet reads one config's raw text.
	RequestConfigGet RequestType = "config.get"
	// RequestConfigSave stores edited raw text (conflict-checked).
	RequestConfigSave RequestType = "config.save"
	// RequestConfigCreate creates a new config.
	RequestConfigCreate RequestType = "config.create"
	// RequestConfigDelete removes a config (after backing it up).
	RequestConfigDelete RequestType = "config.delete"
	// RequestConfigValidate checks raw text without storing it.
	RequestConfigValidate RequestType = "config.validate"
	// RequestConfigActivate makes a config the active one and, in managed
	// mode, restarts the core with it.
	RequestConfigActivate RequestType = "config.activate"
	// RequestShutdown stops the daemon (development gesture).
	RequestShutdown RequestType = "shutdown"
)

// Request is one command.
type Request struct {
	Logging        *CoreLogging          `json:"logging,omitempty"`
	DisplayName    string                `json:"display_name,omitempty"`
	HistoryEnabled *bool                 `json:"history_enabled,omitempty"`
	ProbeSchedule  *diagnostics.Schedule `json:"probe_schedule,omitempty"`
	Version        int                   `json:"version"`
	Type           RequestType           `json:"type"`
	// ProfileID names an explicit runtime target. Until multi-network dispatch
	// is enabled the single-network handler refuses it rather than ignoring it.
	ProfileID string `json:"profile_id,omitempty"`
	// Component is "kind/id", for RequestGrantRetry.
	Component string `json:"component,omitempty"`
	// Name is the config name for the config requests.
	Name string `json:"name,omitempty"`
	// Content is raw TOML for save/create/validate.
	Content string `json:"content,omitempty"`
	// ExpectedRevision is the revision the content was edited from
	// (conflict detection on save).
	ExpectedRevision string `json:"expected_revision,omitempty"`
	// RunCoreCheck asks validate to also run the core's --check-config.
	RunCoreCheck bool `json:"run_core_check,omitempty"`
	// WantDetails tells the daemon the caller has a window on screen, so
	// the detail queries (peers, routes) are kept at the cadence. A
	// hidden or absent caller gets the idle tier instead. Only meaningful
	// on a status request.
	WantDetails bool   `json:"want_details,omitempty"`
	ClientID    string `json:"client_id,omitempty"`
	// WantTraffic tells the daemon the caller displays the traffic curve:
	// it then samples the connection counters every round instead of
	// occasionally. Only meaningful on a status request.
	WantTraffic bool `json:"want_traffic,omitempty"`
}

// RenameProfile changes a manifest profile's display name without restarting it.
func (c *Client) RenameProfile(ctx context.Context, id, name string) (*Status, error) {
	r, err := c.roundTrip(ctx, Request{Type: RequestProfileRename, ProfileID: id, DisplayName: name})
	if err != nil {
		return nil, err
	}
	return r.Status, nil
}

// LoggingSettings reads saved launch choices; a nonnil value saves them.
// The bool reports whether a daemon restart is required to load those choices.
func (c *Client) LoggingSettings(ctx context.Context, value *CoreLogging) (*CoreLogging, bool, error) {
	kind := RequestLoggingGet
	if value != nil {
		kind = RequestLoggingSave
	}
	r, err := c.roundTrip(ctx, Request{Type: kind, Logging: value})
	if err != nil {
		return nil, false, err
	}
	if r.Logging == nil {
		return nil, false, errors.New("logging settings missing from response")
	}
	return r.Logging, r.LoggingRestartRequired, nil
}

// CheckLoggingSink explicitly probes the loaded file sink, not saved pending choices.
func (c *Client) CheckLoggingSink(ctx context.Context) (*LoggingSinkCheck, error) {
	r, err := c.roundTrip(ctx, Request{Type: RequestLoggingCheck})
	if err != nil {
		return nil, err
	}
	if r.LoggingSinkCheck == nil {
		return nil, errors.New("logging sink check missing from response")
	}
	return r.LoggingSinkCheck, nil
}

// ConfigEntry describes one stored config in the list.
type ConfigEntry struct {
	Name     string    `json:"name"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
	Revision string    `json:"revision"`
	Active   bool      `json:"active"`
}

// ConfigPayload answers config.get/save/create/validate: the entry, the
// raw text and the validation verdict. Content is omitted in list
// contexts; validation errors are one-line and position-aware.
type ConfigPayload struct {
	Entry      ConfigEntry `json:"entry"`
	Content    string      `json:"content,omitempty"`
	Valid      bool        `json:"valid"`
	ParseError string      `json:"parse_error,omitempty"`
	// CoreCheck is set when the caller asked for the upstream check; the
	// core only reports through its exit code, so this is a verdict plus
	// a note, never detailed diagnostics.
	CoreCheck     bool   `json:"core_check"`
	CoreCheckOK   bool   `json:"core_check_ok"`
	CoreCheckNote string `json:"core_check_note,omitempty"`
}

// Response answers one request. OK false carries Error; snapshots ride
// along when the command changed state.
type Response struct {
	LoggingSinkCheck       *LoggingSinkCheck     `json:"logging_sink_check,omitempty"`
	Logging                *CoreLogging          `json:"logging,omitempty"`
	LoggingRestartRequired bool                  `json:"logging_restart_required,omitempty"`
	DiagnosticBundle       *DiagnosticBundle     `json:"diagnostic_bundle,omitempty"`
	History                *TrafficHistory       `json:"history,omitempty"`
	ProbeSchedule          *diagnostics.Schedule `json:"probe_schedule,omitempty"`
	Version                int                   `json:"version"`
	OK                     bool                  `json:"ok"`
	Error                  string                `json:"error,omitempty"`
	Status                 *Status               `json:"status,omitempty"`
	Config                 *ConfigPayload        `json:"config,omitempty"`
	Diagnostics            *Diagnostics          `json:"diagnostics,omitempty"`
	Networks               map[string]*Status    `json:"networks,omitempty"`
}

// LoggingSinkCheck is timestamped path evidence, never log delivery evidence.
// State is writable, unavailable, disabled or unmanaged; Reason contains no path/content.
type LoggingSinkCheck struct {
	CheckedAt time.Time `json:"checked_at"`
	State     string    `json:"state"`
	Reason    string    `json:"reason"`
}

// DaemonInfo describes the running daemon.
type DaemonInfo struct {
	Logging   CoreLogging `json:"logging"`
	Version   string      `json:"version"`
	PID       int         `json:"pid"`
	StartedAt time.Time   `json:"started_at"`
	// Mode is "managed" (the daemon runs the core) or "monitor" (it only
	// watches a core it did not start).
	Mode            string     `json:"mode"`
	CheckIntervalMS int        `json:"check_interval_ms"`
	Policy          PolicyInfo `json:"policy"`
	SocketPath      string     `json:"socket_path"`
	CoreVersion     string     `json:"core_version,omitempty"`
	// ServiceManaged is true when a service manager (systemd, SCM,
	// launchd) started this daemon, as opposed to a manual start.
	ServiceManaged bool `json:"service_managed"`
	// Access is the tier this connection was granted: "write" (a
	// per-user socket's owner, or a system socket's authorized group) or
	// "read" (another local user, who may look but not change anything).
	Access string `json:"access,omitempty"`
	// CoreNeedsRoot reports whether the active config needs privilege to
	// run: a TUN-based core (no_tun unset) needs CAP_NET_ADMIN or root,
	// while a user-mode core (no_tun=true, the pure userspace forwarding
	// mode) does not. Nil when it cannot be determined.
	CoreNeedsRoot *bool `json:"core_needs_root,omitempty"`
	// CoreUserMode is true when the active config runs without a TUN
	// device (no_tun=true): the core forwards in userspace and needs no
	// elevated rights.
	CoreUserMode *bool `json:"core_user_mode,omitempty"`
}

// CoreLogging reports launch selections without exposing log paths/content.
type CoreLogging struct {
	ConsoleLevel string `json:"console_level,omitempty"`
	FileLevel    string `json:"file_level,omitempty"`
	FileSizeMB   int    `json:"file_size_mb,omitempty"`
	FileCount    int    `json:"file_count,omitempty"`
}

// PolicyInfo mirrors the health policy for display (and later editing).
type PolicyInfo struct {
	FailThreshold   int  `json:"fail_threshold"`
	OKThreshold     int  `json:"ok_threshold"`
	GraceSeconds    int  `json:"grace_seconds"`
	BackoffBaseMS   int  `json:"backoff_base_ms"`
	BackoffMaxMS    int  `json:"backoff_max_ms"`
	MaxAttempts     int  `json:"max_attempts"`
	WindowSeconds   int  `json:"window_seconds"`
	RecoveryEnabled bool `json:"recovery_enabled"`
}

// Status is the GUI-facing snapshot.
type Status struct {
	EventPersistenceError string     `json:"event_persistence_error,omitempty"`
	ProfileName           string     `json:"profile_name,omitempty"`
	Daemon                DaemonInfo `json:"daemon"`
	// NetworkProgress is present only in multi-network manager responses.
	// A peer's completed checks never update this target's evidence.
	NetworkProgress *NetworkProgress `json:"network_progress,omitempty"`
	// ResourceWarnings contains static-analysis limitations, never raw config.
	ResourceWarnings []ResourceWarning `json:"resource_warnings,omitempty"`
	// Round increments every check round; CheckedAt is when the last
	// round finished. A GUI that cannot refresh sees stale evidence and
	// must not keep showing healthy colors.
	Round            int64     `json:"round"`
	CheckedAt        time.Time `json:"checked_at"`
	NodeSampledAt    time.Time `json:"node_sampled_at,omitempty"`
	PeerSampledAt    time.Time `json:"peer_sampled_at,omitempty"`
	TrafficSampledAt time.Time `json:"traffic_sampled_at,omitempty"`

	Health []health.State `json:"health"`
	// RecentEvents is the tail of the durable timeline.
	RecentEvents []health.Event `json:"recent_events"`
	// Node and Peers are the latest core self-report; nil when the core
	// has not answered yet.
	Node  *coreadapter.NodeInfo `json:"node,omitempty"`
	Peers []coreadapter.Peer    `json:"peers,omitempty"`
	// CollectorError is the last short adapter error (already scrubbed),
	// so the GUI can explain partially available data.
	CollectorError     string               `json:"collector_error,omitempty"`
	PeerCollectorError string               `json:"peer_collector_error,omitempty"`
	ProbeResults       []diagnostics.Result `json:"probe_results,omitempty"`

	// ConfigDir is where configs live; ActiveConfig is the name of the
	// config the core runs with (or the full path when it was started
	// with an external --config). Configs is the stored list.
	ConfigDir       string        `json:"config_dir,omitempty"`
	ActiveConfig    string        `json:"active_config,omitempty"`
	AppliedRevision string        `json:"applied_revision,omitempty"`
	ActivationState string        `json:"activation_state,omitempty"`
	ActivationError string        `json:"activation_error,omitempty"`
	Configs         []ConfigEntry `json:"configs,omitempty"`

	// Traffic is the instance's recent rate curve, oldest first. Gaps are
	// kept on the timeline (never drawn as a zero) and Partial intervals
	// are shorter than the nominal cadence. It is empty until two samples
	// exist.
	Traffic []TrafficPoint `json:"traffic,omitempty"`
	// TrafficIntervalMS is the nominal bucket size of the curve. Zero when
	// no curve is available.
	TrafficIntervalMS int `json:"traffic_interval_ms,omitempty"`
}

// NetworkProgress describes scheduling evidence, not a health verdict.
type NetworkProgress struct {
	Phase        string    `json:"phase"`
	QueuedAt     time.Time `json:"queued_at,omitempty"`
	StartedAt    time.Time `json:"started_at,omitempty"`
	CompletedAt  time.Time `json:"completed_at,omitempty"`
	QueueDelayMS int64     `json:"queue_delay_ms"`
}

type ResourceWarning struct {
	ProfileID      string `json:"profile_id"`
	Field          string `json:"field"`
	OtherProfileID string `json:"other_profile_id,omitempty"`
	OtherField     string `json:"other_field,omitempty"`
	Reason         string `json:"reason"`
}

// TrafficPoint is one interval of the aggregate rate curve in the units a
// chart wants: bytes and packets per second.
type TrafficPoint struct {
	Time        time.Time `json:"time"`
	RxBytesPS   float64   `json:"rx_bytes_ps"`
	TxBytesPS   float64   `json:"tx_bytes_ps"`
	RxPacketsPS float64   `json:"rx_packets_ps"`
	TxPacketsPS float64   `json:"tx_packets_ps"`
	// Gap marks an interval with no trustworthy measurement.
	Gap bool `json:"gap,omitempty"`
	// Partial marks an interval shorter than the nominal cadence.
	Partial bool `json:"partial,omitempty"`
}

// ErrNotRunning is returned when no daemon answers on the socket.
var ErrNotRunning = errors.New("management service not running")

// Client talks to the daemon. Safe for concurrent use.
type Client struct {
	SocketPath string
	Timeout    time.Duration
	clientID   string
	profileID  string // immutable; use ForProfile to create a scoped client

	// wantDetails / wantTraffic tell the daemon what the caller has on
	// screen, so it can pick its sampling tier. Hints only: the daemon
	// stays correct without them.
	wantDetails atomic.Bool
	wantTraffic atomic.Bool
}

// NewClient returns a client for path (empty: DefaultSocketPath).
func NewClient(path string) *Client {
	if path == "" {
		path = DefaultSocketPath()
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		panic(err)
	}
	return &Client{SocketPath: path, Timeout: 3 * time.Second, clientID: fmt.Sprintf("%x", id)}
}

// SetWantDetails records whether the caller currently has a window on
// screen (any page), which keeps the detail queries at the cadence.
func (c *Client) SetWantDetails(v bool) { c.wantDetails.Store(v) }

// WantDetails reports the flag SetWantDetails last recorded.
func (c *Client) WantDetails() bool { return c.wantDetails.Load() }

// SetWantTraffic records whether the caller currently displays the
// traffic curve.
func (c *Client) SetWantTraffic(v bool) { c.wantTraffic.Store(v) }

// WantTraffic reports the flag SetWantTraffic last recorded.
func (c *Client) WantTraffic() bool { return c.wantTraffic.Load() }

func (c *Client) roundTrip(ctx context.Context, req Request) (*Response, error) {
	req.Version = ProtocolVersion
	if req.ProfileID == "" && req.Type != RequestNetworks && req.Type != RequestShutdown && req.Type != RequestDiagnosticBundle {
		req.ProfileID = c.profileID
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	if (req.Type == RequestConfigActivate || req.Type == RequestCoreStop) && timeout == 3*time.Second {
		timeout = 45 * time.Second // preserve explicitly configured client deadlines
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", c.SocketPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || isConnRefused(err) {
			return nil, fmt.Errorf("%w: %v", ErrNotRunning, err)
		}
		return nil, err
	}
	defer conn.Close()
	stopCancellation := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stopCancellation()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	if _, err := conn.Write(append(payload, '\n')); err != nil {
		return nil, err
	}
	reader := bufio.NewReaderSize(conn, 1<<20)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	var resp Response
	if err := json.Unmarshal(bytes.TrimSpace(line), &resp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if resp.Version != ProtocolVersion {
		return nil, fmt.Errorf("protocol version mismatch: daemon %d, client %d", resp.Version, ProtocolVersion)
	}
	if !resp.OK {
		return &resp, fmt.Errorf("daemon: %s", resp.Error)
	}
	return &resp, nil
}

func isConnRefused(err error) bool {
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return opErr.Op == "dial"
	}
	return false
}

// statusOf keeps the snapshot extraction nil-safe: dial and protocol
// failures return a nil response.
func statusOf(resp *Response, err error) (*Status, error) {
	if resp == nil {
		return nil, err
	}
	return resp.Status, err
}

// Status reads a snapshot, telling the daemon whether this caller
// currently displays the traffic curve (SetWantTraffic).
func (c *Client) Status(ctx context.Context) (*Status, error) {
	resp, err := c.roundTrip(ctx, Request{
		Type:        RequestStatus,
		ClientID:    c.clientID,
		WantDetails: c.wantDetails.Load(),
		WantTraffic: c.wantTraffic.Load(),
	})
	return statusOf(resp, err)
}

// Check runs one check round now.
func (c *Client) Check(ctx context.Context) (*Status, error) {
	resp, err := c.roundTrip(ctx, Request{Type: RequestCheck})
	return statusOf(resp, err)
}

// StartCore starts the managed core.
func (c *Client) StartCore(ctx context.Context) (*Status, error) {
	resp, err := c.roundTrip(ctx, Request{Type: RequestCoreStart})
	return statusOf(resp, err)
}

// StopCore stops the managed core at the user's request.
func (c *Client) StopCore(ctx context.Context) (*Status, error) {
	resp, err := c.roundTrip(ctx, Request{Type: RequestCoreStop})
	return statusOf(resp, err)
}

// GrantRetry re-authorizes automatic recovery of one component.
func (c *Client) GrantRetry(ctx context.Context, component string) (*Status, error) {
	resp, err := c.roundTrip(ctx, Request{Type: RequestGrantRetry, Component: component})
	return statusOf(resp, err)
}

// ConfigGet reads one config's raw text; also available through the direct
// config requests for scripts.
func (c *Client) ConfigGet(ctx context.Context, name string) (*ConfigPayload, error) {
	resp, err := c.roundTrip(ctx, Request{Type: RequestConfigGet, Name: name})
	return configOf(resp, err)
}

// ConfigSave stores edited raw text with conflict detection.
func (c *Client) ConfigSave(ctx context.Context, name, content, expectedRevision string) (*ConfigPayload, error) {
	resp, err := c.roundTrip(ctx, Request{
		Type: RequestConfigSave, Name: name, Content: content, ExpectedRevision: expectedRevision,
	})
	return configOf(resp, err)
}

// ConfigCreate creates a new config.
func (c *Client) ConfigCreate(ctx context.Context, name, content string) (*ConfigPayload, error) {
	resp, err := c.roundTrip(ctx, Request{Type: RequestConfigCreate, Name: name, Content: content})
	return configOf(resp, err)
}

// ConfigDelete removes a config after backing it up.
func (c *Client) ConfigDelete(ctx context.Context, name string) error {
	_, err := c.roundTrip(ctx, Request{Type: RequestConfigDelete, Name: name})
	return err
}

// ConfigValidate checks raw text without storing it; runCoreCheck also
// runs the core's --check-config.
func (c *Client) ConfigValidate(ctx context.Context, name, content string, runCoreCheck bool) (*ConfigPayload, error) {
	resp, err := c.roundTrip(ctx, Request{
		Type: RequestConfigValidate, Name: name, Content: content, RunCoreCheck: runCoreCheck,
	})
	return configOf(resp, err)
}

// ConfigActivate makes a config active and (managed mode) restarts the
// core with it.
func (c *Client) ConfigActivate(ctx context.Context, name string, revision ...string) (*Status, error) {
	expected := ""
	if len(revision) > 0 {
		expected = revision[0]
	}
	resp, err := c.roundTrip(ctx, Request{Type: RequestConfigActivate, Name: name, ExpectedRevision: expected})
	return statusOf(resp, err)
}

// configOf keeps payload extraction nil-safe.
func configOf(resp *Response, err error) (*ConfigPayload, error) {
	if resp == nil {
		return nil, err
	}
	return resp.Config, err
}

// Shutdown stops the daemon.
func (c *Client) Shutdown(ctx context.Context) error {
	_, err := c.roundTrip(ctx, Request{Type: RequestShutdown})
	return err
}

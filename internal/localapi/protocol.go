// Package localapi is the protocol between the GUI (and the control CLI)
// and the daemon. The daemon owns networks, cores, health and recovery; the
// clients read state and send commands.
//
// Transport: a Unix domain socket, newline-delimited JSON. A request names
// the protocol version; a mismatch fails instead of guessing. Most
// requests are one request and one response per connection; a session
// (RequestSessionAttach) keeps its connection open and receives pushed
// updates until either side closes it.
package localapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/health"
)

// ProtocolVersion is bumped when a request or response changes meaning.
// Version 2 replaced the inherited single/manifest protocol.
const ProtocolVersion = 2

// SocketEnv overrides the socket path (tests, development).
const SocketEnv = "EASYTIER_MYGO_SOCKET"

// UserSocketPath is the no-privileges daemon's per-user socket:
// $EASYTIER_MYGO_SOCKET, else a per-user runtime directory
// ($XDG_RUNTIME_DIR on Linux, the temporary directory on macOS,
// %LOCALAPPDATA% on Windows) + easytier-mygo/ctl.sock.
func UserSocketPath() string {
	if p := os.Getenv(SocketEnv); p != "" {
		return p
	}
	return filepath.Join(userRuntimeDir(), "easytier-mygo", "ctl.sock")
}

// RequestType enumerates the daemon's commands.
type RequestType string

const (
	RequestInfo          RequestType = "info"           // daemon identity and mode
	RequestNetworks      RequestType = "networks"       // every network's status
	RequestDetail        RequestType = "detail"         // one network: peers, routes
	RequestTraffic       RequestType = "traffic"        // one network's samples after Since
	RequestEvents        RequestType = "events"         // recent health events
	RequestConfigGet     RequestType = "config.get"     // raw document (holds the secret)
	RequestConfigSave    RequestType = "config.save"    // conflict-checked save
	RequestNetworkCreate RequestType = "network.create" // Name + Text
	RequestNetworkDelete RequestType = "network.delete"
	RequestNetworkRename RequestType = "network.rename"
	RequestConnect       RequestType = "network.connect"    // run (applies the saved document if never applied)
	RequestDisconnect    RequestType = "network.disconnect" // user stop
	RequestApply         RequestType = "network.apply"      // restart with the saved document
	RequestRetry         RequestType = "network.retry"      // re-authorize automatic recovery
	RequestSettingsGet   RequestType = "settings.get"
	RequestSettingsSet   RequestType = "settings.set"
	RequestSessionAttach RequestType = "session.attach" // streaming; see package doc
	RequestSessionWatch  RequestType = "session.watch"  // which networks a session displays
	RequestCores         RequestType = "cores"          // installed cores
	RequestReleases      RequestType = "cores.releases" // official releases (Pre: include pre-releases)
	RequestCoreInstall   RequestType = "cores.install"  // download and verify Tag
	RequestCoreRemove    RequestType = "cores.remove"
	RequestCoreAdd       RequestType = "cores.add" // custom build: Name, Text = directory (user daemon only)
	RequestShutdown      RequestType = "shutdown"
	RequestHelperUpdate  RequestType = "helper.update" // Text = app directory with a signed bundle
)

// ErrorCode classifies a failed request for clients that react to it.
type ErrorCode string

const (
	CodeNotFound      ErrorCode = "not-found"
	CodeConflict      ErrorCode = "conflict"       // the document changed since it was loaded
	CodeInvalidConfig ErrorCode = "invalid-config" // parse or --check-config failure
	CodeNeedsHelper   ErrorCode = "needs-helper"   // a TUN network without privileges
	CodeForbidden     ErrorCode = "forbidden"
	CodeBusy          ErrorCode = "busy"
	CodeNeedsAdmin    ErrorCode = "needs-admin" // the helper needs administrator approval (pkexec)
)

// Request is one command.
type Request struct {
	Version  int         `json:"version"`
	Type     RequestType `json:"type"`
	Network  string      `json:"network,omitempty"`
	Name     string      `json:"name,omitempty"`
	Text     string      `json:"text,omitempty"`
	Expected string      `json:"expected,omitempty"` // revision the edit started from
	Since    uint64      `json:"since,omitempty"`    // traffic sequence
	Session  string      `json:"session,omitempty"`
	Watch    []string    `json:"watch,omitempty"` // network IDs on screen
	Tag      string      `json:"tag,omitempty"`   // core release tag
	Pre      bool        `json:"pre,omitempty"`   // include pre-releases
	Settings *Settings   `json:"settings,omitempty"`
}

// Response is one answer, or one pushed frame of a session.
type Response struct {
	Version  int             `json:"version"`
	OK       bool            `json:"ok"`
	Error    string          `json:"error,omitempty"`
	Code     ErrorCode       `json:"code,omitempty"`
	Info     *Info           `json:"info,omitempty"`
	Networks []NetworkStatus `json:"networks,omitempty"`
	Detail   *NetworkDetail  `json:"detail,omitempty"`
	Config   *ConfigDoc      `json:"config,omitempty"`
	Traffic  []TrafficPoint  `json:"traffic,omitempty"`
	Events   []health.Event  `json:"events,omitempty"`
	Settings *Settings       `json:"settings,omitempty"`
	Cores    []CoreInfo      `json:"cores,omitempty"`
	Releases []ReleaseInfo   `json:"releases,omitempty"`
	Push     *Push           `json:"push,omitempty"`
}

// CoreInfo is an installed EasyTier core.
type CoreInfo struct {
	Tag      string `json:"tag"`    // release tag, "bundled", or a custom name
	Source   string `json:"source"` // bundled, official, custom
	Tested   bool   `json:"tested"`
	Pre      bool   `json:"pre,omitempty"`
	Selected bool   `json:"selected,omitempty"`
}

// ReleaseInfo is an official EasyTier release available for download.
type ReleaseInfo struct {
	Tag        string    `json:"tag"`
	Prerelease bool      `json:"prerelease"`
	Published  time.Time `json:"published"`
	Size       int64     `json:"size"`
	Installed  bool      `json:"installed"`
	Tested     bool      `json:"tested"`
}

// Host names where a daemon runs.
type Host string

const (
	// HostUser is the no-privileges daemon, a child of the GUI.
	HostUser Host = "user"
	// HostHelper is the privileged helper registered with the OS.
	HostHelper Host = "helper"
)

// Info describes the daemon.
type Info struct {
	Version   string    `json:"version"`
	Host      Host      `json:"host"`
	Access    Access    `json:"access"`
	StartedAt time.Time `json:"started_at"`
	// CoreVersion is the easytier-cli version the daemon uses.
	CoreVersion string `json:"core_version,omitempty"`
	// Privileged reports whether TUN networks can run.
	Privileged bool `json:"privileged"`
}

// Settings are the daemon's persisted options.
type Settings struct {
	// Background keeps networks running without a GUI session (helper
	// only). Off means Follow GUI.
	Background bool `json:"background"`
	// Core selects the EasyTier core by tag ("" or "bundled": the bundled one).
	Core string `json:"core,omitempty"`
}

// State is the plain-language summary a client shows for a network.
type State string

const (
	StateStopped    State = "stopped"
	StateStarting   State = "starting"
	StateConnecting State = "connecting" // running, no address or no peers yet
	StateConnected  State = "connected"
	StateDegraded   State = "degraded" // running with an external or collector problem
	StateFailed     State = "failed"   // a local fault; recovery may be running
	StateUnknown    State = "unknown"
)

// NetworkStatus is one network's summary.
type NetworkStatus struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Run   bool   `json:"run"`
	State State  `json:"state"`

	Level     health.Level         `json:"level"`
	Cause     health.Cause         `json:"cause"`
	Reason    string               `json:"reason,omitempty"`
	Lifecycle health.Lifecycle     `json:"lifecycle"`
	Recovery  health.RecoveryState `json:"recovery"`

	NetworkName string `json:"network_name,omitempty"`
	Hostname    string `json:"hostname,omitempty"`
	VirtualIPv4 string `json:"virtual_ipv4,omitempty"`
	PeerCount   int    `json:"peer_count"`
	NATType     string `json:"nat_type,omitempty"`
	CoreVersion string `json:"core_version,omitempty"`
	NoTUN       bool   `json:"no_tun"`
	// MagicDNSZone is the saved document's MagicDNS zone ("et.net"); empty
	// when accept_dns is off. Peers resolve as <hostname>.<zone>.
	MagicDNSZone string    `json:"magic_dns_zone,omitempty"`
	StartedAt    time.Time `json:"started_at,omitzero"`
	CheckedAt    time.Time `json:"checked_at,omitzero"`

	RxRate  float64 `json:"rx_rate"` // bytes/s of the latest sample
	TxRate  float64 `json:"tx_rate"`
	LastSeq uint64  `json:"last_seq"`

	// Applied is the running revision; Saved the document's. They differ
	// when saved changes wait to be applied.
	Applied string `json:"applied,omitempty"`
	Saved   string `json:"saved,omitempty"`
	// Error is the last start/apply failure, scrubbed.
	Error string `json:"error,omitempty"`
}

// NetworkDetail is one network's peers and routes.
type NetworkDetail struct {
	Status     NetworkStatus `json:"status"`
	Peers      []PeerInfo    `json:"peers"`
	Listeners  []string      `json:"listeners,omitempty"`
	ProxyCIDRs []string      `json:"proxy_cidrs,omitempty"`
	PublicIPs  []string      `json:"public_ips,omitempty"`
	SampledAt  time.Time     `json:"sampled_at,omitzero"`
}

// PeerInfo is one node of the network.
type PeerInfo struct {
	Hostname  string  `json:"hostname"`
	IPv4      string  `json:"ipv4,omitempty"`
	Local     bool    `json:"local,omitempty"`
	Direct    bool    `json:"direct"` // p2p rather than relayed
	Cost      int     `json:"cost"`
	LatencyMS float64 `json:"latency_ms"`
	LossRate  float64 `json:"loss_rate"`
	Tunnel    string  `json:"tunnel,omitempty"`
	NATType   string  `json:"nat_type,omitempty"`
	Version   string  `json:"version,omitempty"`
	// PublicServer marks a shared relay server rather than a member.
	PublicServer bool `json:"public_server,omitempty"`
	// Subnets are the local networks the node shares.
	Subnets []string   `json:"subnets,omitempty"`
	RxBytes uint64     `json:"rx_bytes"`
	TxBytes uint64     `json:"tx_bytes"`
	Conns   []ConnInfo `json:"conns,omitempty"`
}

// ConnInfo is one tunnel connection to a peer.
type ConnInfo struct {
	Tunnel    string  `json:"tunnel"`
	Local     string  `json:"local,omitempty"`
	Remote    string  `json:"remote,omitempty"`
	LatencyMS float64 `json:"latency_ms"`
	LossRate  float64 `json:"loss_rate"`
	RxBytes   uint64  `json:"rx_bytes"`
	TxBytes   uint64  `json:"tx_bytes"`
}

// ConfigDoc is a network's raw document.
type ConfigDoc struct {
	Text     string `json:"text"`
	Revision string `json:"revision"`
	Applied  string `json:"applied,omitempty"`
}

// TrafficPoint is one sample of a network's aggregate rate.
type TrafficPoint struct {
	Seq  uint64    `json:"seq"`
	Time time.Time `json:"time"`
	Rx   float64   `json:"rx"` // bytes/s
	Tx   float64   `json:"tx"`
	// Gap marks a sample without a trustworthy measurement.
	Gap bool `json:"gap,omitempty"`
}

// Push is one frame a session receives.
type Push struct {
	Session  string                    `json:"session,omitempty"` // first frame only
	Networks []NetworkStatus           `json:"networks,omitempty"`
	Traffic  map[string][]TrafficPoint `json:"traffic,omitempty"`
	Events   []health.Event            `json:"events,omitempty"`
}

// ErrNotRunning is returned when no daemon answers on the socket.
var ErrNotRunning = errors.New("daemon not running")

// RequestError is a daemon-side failure with its code.
type RequestError struct {
	Code    ErrorCode
	Message string
}

func (e *RequestError) Error() string { return e.Message }

// CodeOf returns the code of a RequestError, or "".
func CodeOf(err error) ErrorCode {
	var re *RequestError
	if errors.As(err, &re) {
		return re.Code
	}
	return ""
}

// Client talks to one daemon socket. Safe for concurrent use.
type Client struct {
	SocketPath string
	// Timeout bounds one request; Apply and Connect may take longer and
	// get LongTimeout.
	Timeout, LongTimeout time.Duration
}

// NewClient returns a client for path.
func NewClient(path string) *Client {
	return &Client{SocketPath: path, Timeout: 15 * time.Second, LongTimeout: 10 * time.Minute}
}

// Do sends one request and returns the response.
// Returns: ErrNotRunning (wrapped) when nothing listens; a *RequestError
// when the daemon refused the request.
func (c *Client) Do(ctx context.Context, req Request) (*Response, error) {
	timeout := c.Timeout
	switch req.Type {
	case RequestApply, RequestConnect, RequestDisconnect, RequestNetworkDelete, RequestShutdown, RequestCoreInstall, RequestSettingsSet:
		timeout = c.LongTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	if err := writeRequest(conn, req); err != nil {
		return nil, err
	}
	resp, err := readResponse(bufio.NewReaderSize(conn, maxResponseSize))
	if err != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return resp, err
}

// Attach opens a session. fn receives every pushed frame until ctx ends,
// the daemon goes away, or fn returns an error.
// Returns: the reason the session ended.
func (c *Client) Attach(ctx context.Context, watch []string, fn func(*Push) error) error {
	conn, err := c.dial(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	if err := writeRequest(conn, Request{Type: RequestSessionAttach, Watch: watch}); err != nil {
		return err
	}
	r := bufio.NewReaderSize(conn, maxResponseSize)
	for {
		resp, err := readResponse(r)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		if resp.Push != nil {
			if err := fn(resp.Push); err != nil {
				return err
			}
		}
	}
}

func (c *Client) dial(ctx context.Context) (net.Conn, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", c.SocketPath)
	if err != nil {
		var opErr *net.OpError
		if errors.Is(err, os.ErrNotExist) || (errors.As(err, &opErr) && opErr.Op == "dial") {
			return nil, fmt.Errorf("%w: %v", ErrNotRunning, err)
		}
		return nil, err
	}
	return conn, nil
}

// maxResponseSize bounds one response line (a detail with many peers).
const maxResponseSize = 8 << 20

func writeRequest(conn net.Conn, req Request) error {
	req.Version = ProtocolVersion
	payload, err := json.Marshal(req)
	if err != nil {
		return err
	}
	_, err = conn.Write(append(payload, '\n'))
	return err
}

func readResponse(r *bufio.Reader) (*Response, error) {
	line, err := r.ReadBytes('\n')
	if err != nil {
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
		return &resp, &RequestError{Code: resp.Code, Message: resp.Error}
	}
	return &resp, nil
}

// Fail builds an error response.
func Fail(code ErrorCode, format string, args ...any) Response {
	return Response{OK: false, Code: code, Error: fmt.Sprintf(format, args...)}
}

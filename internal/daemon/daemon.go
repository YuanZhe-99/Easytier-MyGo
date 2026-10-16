// Package daemon is the resident management service behind the GUI: it
// runs (or watches) an EasyTier core, checks its health on a fixed
// cadence, persists the health timeline, executes bounded recovery and
// answers the GUI's local API. It keeps working when no GUI is open.
package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/configdoc"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/coreadapter"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/diagnostics"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/health"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/localapi"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/sdnotify"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/telemetry"
)

// Options configures the daemon.
type Options struct {
	Logging LoggingOptions
	// Version is the application version, reported to the GUI.
	Version string
	// SocketPath is the local API socket.
	SocketPath string
	// SocketGroup is the gid of the authorization group on a system
	// socket. Non-zero also chowns the socket and its directory. Zero
	// keeps the per-user socket (owner-only, every connection writable).
	SocketGroup int
	// SocketReadGroup is an optional second gid granted read-only access;
	// it only matters when ReadOthers is false and the socket is not
	// owner-only.
	SocketReadGroup int
	// ReadOthers lets users who can reach a system socket but are not in
	// the write group connect read-only. It has no effect on a per-user
	// socket, where only the owner can reach the socket at all.
	ReadOthers bool
	// IdlePeersEvery samples the peer list every Nth round when no client
	// is attached (default 2). The core-liveness query always runs at the
	// check cadence; the peer list only informs the "no peers" verdict
	// and the GUI.
	IdlePeersEvery int
	// IdleTrafficEvery samples the connection counters every Nth round
	// unless a client asks for the traffic curve (default 6). Each skipped
	// round is recorded as a gap, so the curve states when it was not
	// sampled instead of drawing a line over it.
	IdleTrafficEvery int
	// ClientWindow is how long after the last local API request a client
	// is still considered attached (default 10 s).
	ClientWindow time.Duration
	// StateDir holds the durable event log and the core's log file.
	StateDir string
	// CoreBin and CLIBin are the easytier binaries. Empty values look up
	// "easytier-core"/"easytier-cli" in PATH.
	CoreBin, CLIBin string
	// RPC is the core's management portal address.
	RPC string
	// ConfigFile is the network configuration TOML the daemon passes to
	// the core. Empty means monitor mode unless ConfigDir holds a
	// previously activated config.
	ConfigFile string
	// ConfigDir is the directory of config documents the GUI manages
	// (default: ~/.config/easytier-mygo/networks).
	ConfigDir string
	// MonitorOnly forces monitor mode even when ConfigFile is set.
	MonitorOnly bool
	// CheckInterval is the cadence of check rounds.
	CheckInterval time.Duration
	// Policy is the recovery policy.
	Policy health.Policy
	// Logf receives daemon logs (default: log.Printf).
	Logf func(format string, args ...any)
}

func (o *Options) normalize() error {
	if err := o.Logging.Validate(); err != nil {
		return err
	}
	if o.CheckInterval <= 0 {
		o.CheckInterval = 5 * time.Second
	}
	if o.StateDir == "" {
		return errors.New("daemon: StateDir is required")
	}
	if o.SocketPath == "" {
		o.SocketPath = localapi.DefaultSocketPath()
	}
	if o.Logf == nil {
		o.Logf = func(string, ...any) {}
	}
	if o.RPC == "" {
		o.RPC = coreadapter.DefaultRPC
	}
	if o.ConfigDir == "" {
		o.ConfigDir = defaultConfigDir()
	}
	if o.Policy == (health.Policy{}) {
		o.Policy = health.DefaultPolicy()
	}
	// Sampling tiers: while a client is attached every round is
	// collected; without one the informational queries are taken less
	// often (the core-liveness query stays at the cadence). Zero means
	// the defaults.
	if o.IdlePeersEvery <= 0 {
		o.IdlePeersEvery = 2
	}
	if o.IdleTrafficEvery <= 0 {
		o.IdleTrafficEvery = 6
	}
	if o.ClientWindow <= 0 {
		o.ClientWindow = 10 * time.Second
	}
	if !o.monitorMode() && o.ConfigFile == "" && loadPersistedActive(o.StateDir) == "" {
		return errors.New("daemon: managed mode needs --config or a previously activated config")
	}
	return nil
}

func (o *Options) monitorMode() bool {
	return o.MonitorOnly || o.ConfigFile == ""
}

// coreCLI is the subset of the adapter the daemon uses; tests fake it.
type coreCLI interface {
	Node(ctx context.Context) (*coreadapter.NodeInfo, error)
	Peers(ctx context.Context) ([]coreadapter.Peer, error)
	PeersVerbose(ctx context.Context) ([]coreadapter.PeerRoutePair, error)
}

// coreProc is the managed core process; tests fake it.
type coreProc interface {
	Start(configPath string) error
	Stop(timeout time.Duration) error
	Alive() (bool, string)
	Pid() int
}

// Daemon owns the health loop.
type Daemon struct {
	opts   Options
	engine *health.Engine
	cli    coreCLI
	proc   coreProc
	events *eventStore
	store  configdoc.Store
	// operationMu serializes checks and target mutations; snapshots use mu only.
	operationMu       sync.Mutex
	intent            runIntent
	autoBlocked       bool
	stopRequested     atomic.Bool
	shutdownRequested atomic.Bool
	applied           appliedConfig
	appliedRevision   atomic.Value
	activationState   atomic.Value
	activationError   atomic.Value
	activationTrace   activationTrace // operationMu owns transaction metadata
	leases            map[string]watchLease

	mu           sync.Mutex
	round        int64
	checkedAt    time.Time
	node         *coreadapter.NodeInfo
	peers        []coreadapter.Peer
	collectorErr string
	startedAt    time.Time
	pending      map[health.Component]bool
	// activePath is the config file the core runs with. It follows
	// activations and is persisted across daemon restarts.
	activePath string
	// coreUserMode is whether the active config runs without a TUN device
	// (no_tun=true). Nil when unknown; refreshed whenever the active
	// config changes, not on every snapshot.
	coreUserMode *bool
	// traffic computes rate curves from the connections' raw counters.
	traffic          *telemetry.Tracker
	history          *telemetry.HistoryRecorder // optional, operationMu owns it
	historyError     string
	historyPersistMu sync.Mutex
	// clientSeen is the unix-nano time of the last local API request;
	// wantTraffic records whether that client was displaying the traffic
	// curve. Together they pick the sampling tier (see Options).
	clientSeen  atomic.Int64
	wantDetails atomic.Bool
	wantTraffic atomic.Bool
	// peersAt / trafficAt are when the detail query and the counter query
	// last ran; the idle tiers never delay the first sample (an empty
	// cache must not read as "no peers").
	peersAt          time.Time
	trafficAt        time.Time
	nodeAt           time.Time
	trafficSuccessAt time.Time
	peerError        string
	probeResults     []diagnostics.Result
	probeSettingsMu  sync.Mutex
	probeChanged     chan struct{}
	// notifier talks to the service manager (nil outside systemd).
	notifier sdNotifier
	// notifyFailOnce keeps a broken manager from flooding the log.
	notifyFailOnce sync.Once

	stopCh         chan struct{}
	stopOnce       sync.Once
	recoveryWG     sync.WaitGroup // drained after check/API producers have stopped
	recoveryMu     sync.Mutex
	recoveryClosed bool
}

// New creates a daemon; call Run to serve.
func New(opts Options) (*Daemon, error) {
	if err := opts.normalize(); err != nil {
		return nil, err
	}
	if err := loadLoggingSettings(&opts); err != nil {
		return nil, err
	}
	events, err := newEventStore(opts.StateDir)
	if err != nil {
		return nil, err
	}
	d := &Daemon{
		opts:         opts,
		events:       events,
		store:        configdoc.Store{Dir: opts.ConfigDir},
		startedAt:    time.Now(),
		pending:      make(map[health.Component]bool),
		stopCh:       make(chan struct{}),
		probeChanged: make(chan struct{}, 1),
		// The curve window is generous enough for a chart; 2 s * 240 is
		// about eight minutes, refreshed as new rounds arrive.
		traffic: telemetry.NewTracker(opts.CheckInterval, 240),
	}
	if err := d.initTrafficHistory(); err != nil {
		d.historyError = "traffic history unavailable"
		d.opts.Logf("daemon: optional traffic history disabled: %v", err)
	}
	if err := d.store.Ensure(); err != nil {
		return nil, err
	}

	// Resolve the active config: the persisted activation wins, then
	// --config. A managed daemon cannot start without one.
	if p := loadPersistedActive(opts.StateDir); p != "" {
		d.activePath = p
	} else {
		d.activePath = opts.ConfigFile
	}
	if d.managed() && d.activePath != "" {
		if _, err := os.Stat(d.activePath); err != nil {
			return nil, fmt.Errorf("daemon: active config %s: %w", d.activePath, err)
		}
		if err := persistActive(opts.StateDir, d.activePath); err != nil {
			return nil, err
		}
	}
	d.refreshCoreUserMode()

	d.engine = health.New(opts.Policy)
	d.engine.SetEventSink(events.appendEvent)
	// Attempts spent by a previous daemon process still count: the OS may
	// restart the daemon (watchdog, upgrade, crash) but must not thereby
	// hand out a fresh recovery budget.
	d.restoreBudget()
	if err := d.loadIntent(); err != nil {
		d.autoBlocked = true
		d.intent = runIntent{Version: 1, Desired: health.DesiredStop}
		d.engine.SetDesired(d.coreComponent(), health.DesiredStop)
		d.engine.SetDesired(d.instanceComponent(), health.DesiredStop)
		d.opts.Logf("daemon: automatic startup inhibited: %v", err)
	}
	if d.autoBlocked {
		d.engine.SetDesired(d.coreComponent(), health.DesiredStop)
		d.engine.SetDesired(d.instanceComponent(), health.DesiredStop)
	}
	// Managed mode may be resumed from persisted selection without --config.
	if !opts.MonitorOnly && d.activePath != "" {
		d.opts.ConfigFile = d.activePath
	}
	d.cli = coreadapter.CLI{Bin: opts.CLIBin, RPC: opts.RPC}
	if !opts.MonitorOnly && d.activePath != "" {
		d.proc = newCoreProcess(opts.CoreBin, opts.RPC, filepath.Join(opts.StateDir, "core.log"), opts.Logf)
		d.proc.(*coreProcess).logging = opts.Logging
		if err := d.initApplied(); err != nil {
			return nil, fmt.Errorf("load applied configuration: %w", err)
		}
		if d.autoBlocked {
			d.engine.SetDesired(d.coreComponent(), health.DesiredStop)
			d.engine.SetDesired(d.instanceComponent(), health.DesiredStop)
		}
	}
	return d, nil
}

func (d *Daemon) coreComponent() health.Component {
	return health.Component{Kind: health.KindCore, ID: "core"}
}

// refreshCoreUserMode reads the active config and records whether it runs
// in user mode (no_tun=true, no TUN device, no elevated rights needed).
// It is called when the active config changes rather than on every
// snapshot, because it touches the filesystem.
func (d *Daemon) refreshCoreUserMode() {
	path := d.activeConfigPath()
	var mode *bool
	if path != "" {
		if b, err := configNeedsNoRoot(path); err == nil {
			mode = &b
		}
	}
	d.mu.Lock()
	d.coreUserMode = mode
	d.mu.Unlock()
}

// configNeedsNoRoot reports whether the config at path runs the core in
// user mode: TOML `[flags] no_tun = true`. A config without the key (or an
// unreadable one) is not user mode.
func configNeedsNoRoot(path string) (bool, error) {
	text, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	v, err := configdoc.Extract(string(text))
	if err != nil {
		return false, err
	}
	return v.Flags["no_tun"], nil
}

// activeConfigPath returns the config the core runs with.
func (d *Daemon) activeConfigPath() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.activePath
}

// instanceComponent names the instance after the active config file, which
// makes the identity stable across restarts of the same config.
func (d *Daemon) instanceComponent() health.Component {
	path := d.activeConfigPath()
	name := "default"
	if path != "" {
		name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	return health.Component{Kind: health.KindInstance, ID: name}
}

// instanceNameOf mirrors instanceComponent for a given path.
func instanceNameOf(path string) string {
	if path == "" {
		return "default"
	}
	return strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
}

func (d *Daemon) managed() bool { return d.proc != nil }

// Run serves the local API and the check loop until ctx is cancelled or
// the GUI asks the daemon to shut down.
func (d *Daemon) Run(ctx context.Context) error {
	if ctx.Err() != nil {
		return nil
	}
	release, err := lockState(d.opts.StateDir)
	if err != nil {
		return err
	}
	defer release()
	return d.run(ctx)
}

func (d *Daemon) run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-d.stopCh:
			cancel()
		case <-ctx.Done():
		}
	}()

	ln, err := localapi.ListenWith(localapi.ListenOptions{
		Path:       d.opts.SocketPath,
		Mode:       d.socketMode(),
		DirMode:    d.socketDirMode(),
		Group:      d.opts.SocketGroup,
		Authorizer: d.authorizer(),
	})
	if err != nil {
		return err
	}
	defer os.Remove(d.opts.SocketPath)

	if ctx.Err() != nil {
		d.shutdownRequested.Store(true)
		return nil
	}
	if d.managed() {
		if err := d.initialStart(); err != nil {
			d.opts.Logf("daemon: initial core start failed: %v", err)
		}
	}
	d.runRound(ctx)

	// The API is up: tell the service manager startup finished. With
	// Type=notify this is what makes `systemctl start` return and starts
	// the watchdog clock.
	if n := sdnotify.FromEnv(); n != nil {
		d.notifier = n
	}
	d.notifierReady()
	go d.watchdogStopHook(ctx)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); d.runProbes(ctx) }()
	wg.Add(1)
	go func() { defer wg.Done(); d.runHistoryPersistence(ctx) }()
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := localapi.ServeWith(ctx, ln, d, d.authorizer()); err != nil {
			d.opts.Logf("daemon: local API stopped: %v", err)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(d.opts.CheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				d.runRound(ctx)
			}
		}
	}()

	<-ctx.Done()
	d.shutdownRequested.Store(true)
	wg.Wait()
	d.waitRecoveries()
	d.operationMu.Lock()
	d.autoBlocked = true
	if d.managed() {
		if err := d.proc.Stop(10 * time.Second); err != nil {
			d.opts.Logf("daemon: stopping core: %v", err)
		}
	}
	if !d.autoBlocked {
		d.persistBudget()
	}
	d.operationMu.Unlock()
	return nil
}

// runRound performs one check round and runs the recovery it decided on.
func (d *Daemon) runRound(ctx context.Context) {
	d.operationMu.Lock()
	actions := d.checkRoundLocked(ctx)
	generation := d.intent.Generation
	d.operationMu.Unlock()
	// A completed round is progress: report it to the service manager's
	// watchdog (no-op when not running under one). A round that never
	// completes therefore stops the reports, which is what systemd waits
	// for before restarting a hung service.
	if ctx.Err() == nil {
		d.reportProgress()
	}
	for _, a := range actions {
		if a.Kind == health.ActionNone {
			continue
		}
		key := a.Component
		d.mu.Lock()
		if d.pending[key] {
			d.mu.Unlock()
			continue
		}
		d.pending[key] = true
		d.mu.Unlock()
		d.recoveryMu.Lock()
		if d.recoveryClosed || ctx.Err() != nil || d.shutdownRequested.Load() {
			d.recoveryMu.Unlock()
			d.mu.Lock()
			delete(d.pending, key)
			d.mu.Unlock()
			continue
		}
		d.recoveryWG.Add(1)
		d.recoveryMu.Unlock()
		go func(a health.Action) {
			defer d.recoveryWG.Done()
			defer func() {
				d.mu.Lock()
				delete(d.pending, a.Component)
				d.mu.Unlock()
			}()
			if a.After > 0 {
				timer := time.NewTimer(a.After)
				defer timer.Stop()
				select {
				case <-ctx.Done():
					return
				case <-timer.C:
				}
			}
			d.operationMu.Lock()
			defer d.operationMu.Unlock()
			if ctx.Err() == nil && generation == d.intent.Generation {
				d.performActionLocked(a)
			}
		}(a)
	}
}

func (d *Daemon) waitRecoveries() {
	d.recoveryMu.Lock()
	d.recoveryClosed = true
	d.recoveryMu.Unlock()
	d.recoveryWG.Wait()
}

// checkRound collects observations and asks the engine for their verdict.
// Exposed for tests through runRound.
func (d *Daemon) checkRound(ctx context.Context) []health.Action {
	d.operationMu.Lock()
	defer d.operationMu.Unlock()
	return d.checkRoundLocked(ctx)
}

func (d *Daemon) checkRoundLocked(ctx context.Context) []health.Action {
	if ctx.Err() != nil {
		return nil
	}
	now := time.Now()
	trafficObserved := false
	defer func() {
		if !trafficObserved && d.traffic != nil {
			d.observeTraffic(telemetry.Sample{Time: now, Generation: d.trafficGeneration(), Missing: true})
		}
	}()
	d.mu.Lock()
	d.round++
	d.mu.Unlock()

	coreComp := d.coreComponent()
	instComp := d.instanceComponent()
	var actions []health.Action
	if d.managed() {
		if state, ok := d.engine.State(coreComp); ok && state.Desired == health.DesiredStop {
			d.engine.Observe(health.Observation{Component: coreComp, Lifecycle: health.LifecycleStopped, Level: health.LevelUnknown, Reason: "manual-stop"})
			d.engine.Observe(health.Observation{Component: instComp, Lifecycle: health.LifecycleStopped, Level: health.LevelUnknown, Reason: "manual-stop"})
			d.setCheckpoint(now, nil, nil, "")
			return nil
		}
	}

	lifecycle := health.LifecycleRunning
	if d.managed() {
		alive, detail := d.proc.Alive()
		if !alive {
			lifecycle = health.LifecycleStopped
			actions = append(actions, d.engine.Observe(health.Observation{
				Component: coreComp, Lifecycle: lifecycle,
				Level: health.LevelFailed, Cause: health.CauseLocalFault,
				Reason: "process-exited", Detail: detail,
			})...)
			// The instance's state is contained in the core's recovery;
			// reporting it as an independent failure would schedule a
			// second restart for the same fault.
			actions = append(actions, d.engine.Observe(health.Observation{
				Component: instComp, Lifecycle: health.LifecycleStopped,
				Level: health.LevelUnknown, Cause: health.CauseCollector,
				Reason: "core-not-running", Detail: "contained in core recovery",
			})...)
			d.setCheckpoint(now, nil, nil, "")
			return actions
		}
	}

	node, err := d.cli.Node(ctx)
	if ctx.Err() != nil {
		return nil
	}
	if err != nil {
		obs := d.observationFromError(coreComp, lifecycle, err)
		actions = append(actions, d.engine.Observe(obs)...)
		// Watch the instance as unknown: its health cannot be judged
		// while the core does not answer, and a second recovery would
		// compete with the core's.
		actions = append(actions, d.engine.Observe(health.Observation{
			Component: instComp, Lifecycle: health.LifecycleUnknown,
			Level: health.LevelUnknown, Cause: health.CauseCollector,
			Reason: "core-unreachable",
		})...)
		d.setCheckpoint(now, nil, nil, shortError(err))
		return actions
	}

	if node == nil {
		actions = append(actions, d.engine.Observe(health.Observation{Component: coreComp, Lifecycle: health.LifecycleUnknown, Level: health.LevelUnknown, Cause: health.CauseCollector, Reason: "collector-empty-node"})...)
		d.setCheckpoint(now, nil, nil, "collector returned no node")
		return actions
	}
	copyNode := *node
	core := &copyNode
	d.mu.Lock()
	d.nodeAt = time.Now()
	d.mu.Unlock()
	core.Config = "" // raw TOML may hold secrets: never cache or forward it
	actions = append(actions, d.engine.Observe(health.Observation{
		Component: coreComp, Lifecycle: lifecycle,
		Level: health.LevelHealthy, Cause: health.CauseNone, Reason: "rpc-ok",
		Detail: health.FormatDetail("version "+node.Version, fmt.Sprintf("peer-id %d", node.PeerID)),
	})...)

	// The instance gets exactly one combined verdict per round: observing
	// it twice with contradictory levels would oscillate healthy and
	// degraded on alternate rounds.
	inst := health.Observation{
		Component: instComp, Lifecycle: health.LifecycleRunning,
		Level: health.LevelHealthy, Cause: health.CauseNone, Reason: "instance-running",
	}
	switch {
	case node.IPv4Addr == "":
		// No virtual address yet: normal while starting, waiting for the
		// peer that assigns it, or in a no-TUN dev setup. Not a fault.
		inst.Level = health.LevelDegraded
		inst.Cause = health.CauseAwaiting
		inst.Reason = "awaiting-address"
	default:
		inst.Detail = "address " + node.IPv4Addr
	}

	// The peer list informs the "no peers" verdict and the GUI; without a
	// client it is sampled on a slower tier (the core-liveness query above
	// always runs at the cadence).
	var peers []coreadapter.Peer
	var perr error
	if d.samplePeersThisRound() {
		peers, perr = d.cli.Peers(ctx)
		if ctx.Err() != nil {
			return nil
		}
		d.mu.Lock()
		d.peerError = ""
		if perr != nil {
			d.peerError = shortError(perr)
		}
		d.mu.Unlock()
		if perr == nil {
			d.mu.Lock()
			d.peersAt = now
			d.mu.Unlock()
		}
	} else {
		d.mu.Lock()
		peers = d.peers
		if d.peerError != "" {
			perr = errors.New(d.peerError)
		}
		d.mu.Unlock()
	}
	if perr != nil {
		inst.Level = health.LevelUnknown
		inst.Cause = health.CauseCollector
		inst.Reason = "peers-unavailable"
		inst.Detail = health.FormatDetail(inst.Detail, "peers unavailable: "+shortError(perr))
	} else {
		remote := 0
		for _, p := range peers {
			if !p.IsLocal() {
				remote++
			}
		}
		if remote == 0 {
			// Reachable core, running instance, no peers: the network is
			// waiting. Degraded but never a local fault.
			if inst.Reason != "awaiting-address" {
				// The address wait is the more precise story when both
				// are true.
				inst.Level = health.LevelDegraded
				inst.Cause = health.CauseAwaiting
				inst.Reason = "no-peers"
			}
			inst.Detail = health.FormatDetail(inst.Detail, "no peers connected yet")
		} else {
			inst.Detail = health.FormatDetail(inst.Detail, fmt.Sprintf("%d peer(s)", remote))
		}
	}
	actions = append(actions, d.engine.Observe(inst)...)

	// Traffic sampling is a separate, best-effort query: it never changes
	// a health verdict, and a failed sample is recorded as a gap.
	d.sampleTraffic(ctx, now)
	trafficObserved = true

	if perr != nil {
		d.setCheckpoint(now, core, nil, "peers: "+shortError(perr))
		return actions
	}
	d.setCheckpoint(now, core, peers, "")
	return actions
}

// --- sampling tier -------------------------------------------------------

// clientAttached reports whether a client asked for something recently.
func (d *Daemon) clientAttached(now time.Time) bool {
	d.mu.Lock()
	if d.leases != nil {
		var details, traffic bool
		for id, lease := range d.leases {
			if !now.Before(lease.Until) {
				delete(d.leases, id)
				continue
			}
			details = details || lease.Details
			traffic = traffic || lease.Traffic
		}
		d.wantDetails.Store(details)
		d.wantTraffic.Store(traffic)
		attached := len(d.leases) > 0
		d.mu.Unlock()
		return attached
	}
	d.mu.Unlock()
	seen := d.clientSeen.Load()
	if seen == 0 {
		return false
	}
	return now.Sub(time.Unix(0, seen)) < d.opts.ClientWindow
}

// samplePeersThisRound decides the peers tier: every round while an
// attached client has a window on screen, every IdlePeersEvery-th round
// otherwise (hidden window, or no client at all). The first sample is
// never delayed: an empty cache must not read as "no peers".
func (d *Daemon) samplePeersThisRound() bool {
	if d.clientAttached(time.Now()) && d.wantDetails.Load() {
		return true
	}
	d.mu.Lock()
	last := d.peersAt
	d.mu.Unlock()
	return d.idleSampleDue(last, d.opts.IdlePeersEvery)
}

// idleSampleDue applies the idle tier: sample when the tier's interval has
// come around, or when nothing was ever sampled.
func (d *Daemon) idleSampleDue(last time.Time, every int) bool {
	if every <= 1 || last.IsZero() {
		return true
	}
	return int(d.round)%every == 0
}

// sampleTrafficThisRound decides the traffic tier: every round while an
// attached client displays the curve, every IdleTrafficEvery-th round
// otherwise. Like the peer list, the first sample is never delayed, so
// the tracker has a baseline the moment anyone looks.
func (d *Daemon) sampleTrafficThisRound(now time.Time) bool {
	if d.clientAttached(now) && d.wantTraffic.Load() {
		return true
	}
	d.mu.Lock()
	last := d.trafficAt
	d.mu.Unlock()
	return d.idleSampleDue(last, d.opts.IdleTrafficEvery)
}

// sampleTraffic reads the connections' raw counters and feeds the rate
// tracker. A skipped or failed round is recorded as a gap, so the curve
// states when it had no data instead of drawing a line over the stretch.
func (d *Daemon) sampleTraffic(ctx context.Context, now time.Time) {
	if d.traffic == nil {
		return
	}
	sample := telemetry.Sample{
		Time:       now,
		Generation: d.trafficGeneration(),
	}
	if !d.sampleTrafficThisRound(now) {
		sample.Missing = true
		d.observeTraffic(sample)
		return
	}
	d.mu.Lock()
	d.trafficAt = now
	d.mu.Unlock()
	pairs, err := d.cli.PeersVerbose(ctx)
	if err != nil {
		sample.Missing = true
		d.observeTraffic(sample)
		return
	}
	d.mu.Lock()
	d.trafficSuccessAt = time.Now()
	d.mu.Unlock()
	for _, p := range pairs {
		for _, c := range p.Peer.Conns {
			if c.IsClosed {
				continue
			}
			sample.Conns = append(sample.Conns, telemetry.ConnSample{
				ConnID:    c.ConnID,
				PeerID:    c.PeerID,
				RxBytes:   c.Stats.RxBytes,
				TxBytes:   c.Stats.TxBytes,
				RxPackets: c.Stats.RxPackets,
				TxPackets: c.Stats.TxPackets,
			})
		}
	}
	d.observeTraffic(sample)
}

// trafficGeneration identifies the instance's current run. The core's pid
// is enough: a restart makes a new generation whose counters are not
// comparable with the old ones.
func (d *Daemon) trafficGeneration() string {
	if d.proc == nil {
		return "monitor"
	}
	alive, _ := d.proc.Alive()
	if !alive {
		return "stopped"
	}
	return fmt.Sprintf("core-%d", d.proc.Pid())
}

// observationFromError maps an adapter error onto a health observation.
func (d *Daemon) observationFromError(c health.Component, lifecycle health.Lifecycle, err error) health.Observation {
	obs := health.Observation{Component: c, Lifecycle: lifecycle, Detail: shortError(err)}
	switch coreadapter.KindOf(err) {
	case coreadapter.ErrTimeout:
		obs.Level, obs.Cause, obs.Reason = health.LevelFailed, health.CauseUnresponsive, "rpc-timeout"
	case coreadapter.ErrRPCUnreachable:
		obs.Level, obs.Cause, obs.Reason = health.LevelFailed, health.CauseLocalFault, "rpc-unreachable"
	case coreadapter.ErrNoInstances:
		obs.Level, obs.Cause, obs.Reason = health.LevelFailed, health.CauseLocalFault, "no-instances"
	default:
		// Missing binary, parse failure, version mismatch: the adapter
		// cannot judge the core, so the verdict is unknown, not failed.
		obs.Level, obs.Cause, obs.Reason = health.LevelUnknown, health.CauseCollector, "collector-"+coreadapter.KindOf(err).String()
	}
	return obs
}

func (d *Daemon) setCheckpoint(at time.Time, node *coreadapter.NodeInfo, peers []coreadapter.Peer, collectorErr string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.checkedAt = time.Now()
	d.node = node
	d.peers = peers
	d.collectorErr = collectorErr
}

// performAction executes one recovery step synchronously. Instance and
// core restarts are the same operation while a core hosts a single
// instance; the reported scope stays explicit for the day that changes.
func (d *Daemon) performAction(a health.Action) {
	d.operationMu.Lock()
	defer d.operationMu.Unlock()
	d.performActionLocked(a)
}

func (d *Daemon) performActionLocked(a health.Action) {
	if d.autoBlocked || d.stopRequested.Load() || d.shutdownRequested.Load() {
		return
	}
	attempt, ok := d.engine.BeginRecovery(a.Component)
	if !ok {
		return
	}
	d.opts.Logf("daemon: recovery %s attempt %d (%s)", a.Component, attempt, a.Reason)
	// Persist before running the action: the attempt is spent from now on,
	// and a daemon restart during the action must not restore the budget.
	if err := d.persistBudget(); err != nil {
		d.engine.EndRecovery(a.Component, err)
		return
	}

	var err error
	if d.managed() {
		err = d.restartCore()
		if err == nil {
			d.engine.Grace(d.coreComponent())
			d.engine.Grace(d.instanceComponent())
		}
	} else {
		err = errors.New("monitor mode: core is not managed here")
	}
	if err != nil {
		d.opts.Logf("daemon: recovery %s failed: %v", a.Component, err)
	}
	d.engine.EndRecovery(a.Component, err)
	d.persistBudget()
}

func (d *Daemon) restartCore() error {
	if err := d.checkRuntime(); err != nil {
		return err
	}
	if d.proc.Pid() != 0 {
		if err := d.proc.Stop(10 * time.Second); err != nil {
			return err
		}
	}
	if d.stopRequested.Load() || d.shutdownRequested.Load() {
		return errors.New("operation cancelled by manual stop")
	}
	return d.proc.Start(d.runtimePath())
}

func (d *Daemon) startCore() error {
	d.stopRequested.Store(false)
	return d.startCoreAfterRequest()
}

// startCoreAfterRequest preserves cancellation published after the request was
// accepted, including while a manager validates cross-network resources.
func (d *Daemon) startCoreAfterRequest() error {
	d.operationMu.Lock()
	defer d.operationMu.Unlock()
	if !d.managed() {
		return errors.New("monitor mode: nothing to start")
	}
	if alive, _ := d.proc.Alive(); alive && !d.autoBlocked {
		return nil
	}
	wasBlocked := d.autoBlocked
	if err := d.checkRuntime(); err != nil {
		return err
	}
	d.engine.GrantRetry(d.coreComponent())
	d.engine.GrantRetry(d.instanceComponent())
	if err := d.persistBudget(); err != nil {
		return err
	}
	d.autoBlocked = false
	if d.applied.Runtime != "" {
		if err := d.saveApplied(d.applied); err != nil {
			d.autoBlocked = true
			return err
		}
		d.mu.Lock()
		d.activePath = d.applied.Source
		d.mu.Unlock()
		d.appliedRevision.Store(d.applied.Revision)
	}
	if d.stopRequested.Load() || d.shutdownRequested.Load() {
		return errors.New("start cancelled before authorization")
	}
	if err := d.writeIntent(health.DesiredRun); err != nil {
		return err
	}
	if alive, _ := d.proc.Alive(); alive && wasBlocked {
		if err := d.restartCore(); err != nil {
			d.autoBlocked = true
			return err
		}
		d.engine.Grace(d.coreComponent())
		d.engine.Grace(d.instanceComponent())
	} else if !alive {
		if err := d.startCoreLocked(); err != nil {
			return err
		}
	}
	if err := os.Remove(filepath.Join(d.opts.StateDir, "activation.json")); err != nil && !os.IsNotExist(err) {
		d.autoBlocked = true
		return err
	}
	d.engine.SetDesired(d.coreComponent(), health.DesiredRun)
	d.engine.SetDesired(d.instanceComponent(), health.DesiredRun)
	d.activationState.Store("applied")
	d.activationError.Store("")
	return nil
}

func (d *Daemon) startCoreLocked() error {
	if d.stopRequested.Load() || d.shutdownRequested.Load() {
		return errors.New("operation cancelled by manual stop")
	}
	d.engine.SetDesired(d.coreComponent(), health.DesiredRun)
	d.engine.SetDesired(d.instanceComponent(), health.DesiredRun)
	if !d.managed() {
		return errors.New("monitor mode: nothing to start")
	}
	path := d.runtimePath()
	if path == "" {
		return errors.New("no active config to start the core with")
	}
	if err := d.checkRuntime(); err != nil {
		return err
	}
	if err := d.proc.Start(path); err != nil {
		return err
	}
	d.engine.Grace(d.coreComponent())
	d.engine.Grace(d.instanceComponent())
	return nil
}

func (d *Daemon) stopCore() error {
	// Publish cancellation before waiting for an ongoing stop/apply operation.
	d.stopRequested.Store(true)
	d.operationMu.Lock()
	defer d.operationMu.Unlock()
	if !d.managed() {
		return errors.New("monitor mode: nothing to stop")
	}
	d.engine.SetDesired(d.coreComponent(), health.DesiredStop)
	d.engine.SetDesired(d.instanceComponent(), health.DesiredStop)
	intentErr := d.writeIntent(health.DesiredStop)
	return errors.Join(intentErr, d.proc.Stop(10*time.Second))
}

// snapshot assembles the GUI-facing status.
func (d *Daemon) snapshot() *localapi.Status {
	d.mu.Lock()
	mode := "monitor"
	if d.managed() {
		mode = "managed"
	}
	info := localapi.DaemonInfo{
		Version:         d.opts.Version,
		PID:             os.Getpid(),
		StartedAt:       d.startedAt,
		Mode:            mode,
		CheckIntervalMS: int(d.opts.CheckInterval / time.Millisecond),
		Policy: localapi.PolicyInfo{
			FailThreshold:   d.opts.Policy.FailThreshold,
			OKThreshold:     d.opts.Policy.OKThreshold,
			GraceSeconds:    int(d.opts.Policy.GracePeriod / time.Second),
			BackoffBaseMS:   int(d.opts.Policy.BackoffBase / time.Millisecond),
			BackoffMaxMS:    int(d.opts.Policy.BackoffMax / time.Millisecond),
			MaxAttempts:     d.opts.Policy.MaxAttempts,
			WindowSeconds:   int(d.opts.Policy.Window / time.Second),
			RecoveryEnabled: d.opts.Policy.RecoveryEnabled,
		},
		SocketPath:     d.opts.SocketPath,
		ServiceManaged: underServiceManager(),
		Logging:        localapi.CoreLogging{ConsoleLevel: d.opts.Logging.ConsoleLevel, FileLevel: d.opts.Logging.FileLevel, FileSizeMB: d.opts.Logging.FileSizeMB, FileCount: d.opts.Logging.FileCount},
	}
	if d.node != nil {
		info.CoreVersion = d.node.Version
	}
	info.CoreUserMode = d.coreUserMode
	if d.coreUserMode != nil {
		needsRoot := !*d.coreUserMode
		info.CoreNeedsRoot = &needsRoot
	}
	activePath := d.activePath
	status := &localapi.Status{
		Daemon:                info,
		Round:                 d.round,
		CheckedAt:             d.checkedAt,
		Health:                d.engine.Snapshot(),
		RecentEvents:          d.events.recent(50),
		EventPersistenceError: d.events.persistenceError(),
		Node:                  d.node,
		Peers:                 d.peers,
		CollectorError:        d.collectorErr,
		PeerCollectorError:    d.peerError,
		ProbeResults:          append([]diagnostics.Result(nil), d.probeResults...),
		ConfigDir:             d.opts.ConfigDir,
		ActiveConfig:          displayActiveConfig(activePath, d.opts.ConfigDir),
		NodeSampledAt:         d.nodeAt, PeerSampledAt: d.peersAt, TrafficSampledAt: d.trafficSuccessAt,
	}
	d.mu.Unlock()

	// The config list reads the filesystem; keep it outside the daemon
	// lock so a slow disk never blocks the health loop.
	status.Configs = d.configEntriesFor(activePath)
	if revision, ok := d.appliedRevision.Load().(string); ok {
		status.AppliedRevision = revision
	}
	if state, ok := d.activationState.Load().(string); ok {
		status.ActivationState = state
	}
	if detail, ok := d.activationError.Load().(string); ok {
		status.ActivationError = detail
	}
	if d.traffic != nil {
		pts := d.traffic.Points()
		status.Traffic = make([]localapi.TrafficPoint, 0, len(pts))
		for _, p := range pts {
			status.Traffic = append(status.Traffic, localapi.TrafficPoint{
				Time:        p.Time,
				RxBytesPS:   p.RxBytesPS,
				TxBytesPS:   p.TxBytesPS,
				RxPacketsPS: p.RxPacketsPS,
				TxPacketsPS: p.TxPacketsPS,
				Gap:         p.Gap,
				Partial:     p.Partial,
			})
		}
		status.TrafficIntervalMS = int(d.traffic.Interval() / time.Millisecond)
	}
	return status
}

// displayActiveConfig names the active config by store name when it lives
// in the config directory, and by path otherwise.
func displayActiveConfig(path, dir string) string {
	if path == "" {
		return ""
	}
	if filepath.Dir(path) == dir {
		return strings.TrimSuffix(filepath.Base(path), configdoc.Ext)
	}
	return path
}

// Handle answers one local API request.
func (d *Daemon) Handle(ctx context.Context, req localapi.Request, access localapi.Access) localapi.Response {
	if !access.Allows(req.Type) {
		return localapi.Response{Version: localapi.ProtocolVersion, OK: false, Error: localapi.ErrForbidden{Tier: access, Type: req.Type}.Error()}
	}
	if req.ProfileID != "" {
		return localapi.Response{Version: localapi.ProtocolVersion, OK: false, Error: "unknown profile target; multi-network runtime is not enabled"}
	}
	if req.Type == localapi.RequestLoggingCheck {
		return d.handleLoggingCheck(ctx)
	}
	if req.Type == localapi.RequestLoggingGet || req.Type == localapi.RequestLoggingSave {
		return d.handleLoggingSettings(ctx, req)
	}
	if req.Type == localapi.RequestProbeGet || req.Type == localapi.RequestProbeSave {
		return d.handleProbeSettings(req)
	}
	if req.Type == localapi.RequestHistory {
		return d.historyResponse()
	}
	if req.Type == localapi.RequestDiagnosticBundle {
		return localapi.Response{Version: localapi.ProtocolVersion, OK: true, DiagnosticBundle: localapi.DiagnosticNetworks(map[string]*localapi.Status{"single": d.snapshot()})}
	}
	if req.Type == localapi.RequestHistorySave || req.Type == localapi.RequestHistoryDelete {
		return d.handleHistoryControl(req)
	}
	// Any request marks a client as attached, which raises the sampling
	// tier; a status request also carries whether that client displays
	// the traffic curve.
	d.clientSeen.Store(time.Now().UnixNano())
	if req.Type == localapi.RequestStatus {
		d.updateLease(req.ClientID, req.WantDetails, req.WantTraffic, time.Now())
	}
	// Stamp the connection's tier onto every snapshot, so the GUI can say
	// "read-only" instead of showing buttons that will be refused. The
	// snapshot itself is shared, so the tier is set on the copy returned
	// to this caller, never stored.
	withAccess := func(st *localapi.Status) *localapi.Status {
		if st != nil {
			st.Daemon.Access = string(access)
		}
		return st
	}
	reply := func(err error) localapi.Response {
		if err != nil {
			return localapi.Response{Version: localapi.ProtocolVersion, OK: false, Error: err.Error()}
		}
		return localapi.Response{Version: localapi.ProtocolVersion, OK: true, Status: withAccess(d.snapshot())}
	}
	replyConfig := func(payload *localapi.ConfigPayload, err error) localapi.Response {
		if err != nil {
			return localapi.Response{Version: localapi.ProtocolVersion, OK: false, Error: err.Error()}
		}
		return localapi.Response{Version: localapi.ProtocolVersion, OK: true, Config: payload, Status: withAccess(d.snapshot())}
	}
	switch req.Type {
	case localapi.RequestStatus:
		return reply(nil)
	case localapi.RequestDiagnostics:
		return localapi.Response{Version: localapi.ProtocolVersion, OK: true, Diagnostics: localapi.DiagnosticSnapshot(d.snapshot())}
	case localapi.RequestCheck:
		d.runRound(ctx)
		return reply(nil)
	case localapi.RequestCoreStart:
		err := d.startCore()
		return reply(err)
	case localapi.RequestCoreStop:
		return reply(d.stopCore())
	case localapi.RequestGrantRetry:
		d.operationMu.Lock()
		defer d.operationMu.Unlock()
		comp, err := parseComponent(req.Component)
		if err != nil {
			return reply(err)
		}
		d.engine.GrantRetry(comp)
		d.opts.Logf("daemon: recovery budget renewed for %s", comp)
		// A renewal is a budget change: persist it so a restart keeps the
		// renewed state instead of resurrecting the spent attempts.
		return reply(d.persistBudget())
	case localapi.RequestConfigGet, localapi.RequestConfigSave, localapi.RequestConfigCreate,
		localapi.RequestConfigDelete, localapi.RequestConfigValidate:
		// The transport already refuses these to read-only connections;
		// check again so the config text never depends on the transport
		// layer alone (config carries the network secret).
		if access != localapi.AccessWrite {
			return localapi.Response{Version: localapi.ProtocolVersion, OK: false,
				Error: localapi.ErrForbidden{Tier: access, Type: req.Type}.Error()}
		}
		switch req.Type {
		case localapi.RequestConfigGet:
			return replyConfig(d.configGet(req.Name))
		case localapi.RequestConfigSave:
			return replyConfig(d.configSave(req.Name, req.Content, req.ExpectedRevision))
		case localapi.RequestConfigCreate:
			return replyConfig(d.configCreate(req.Name, req.Content))
		case localapi.RequestConfigDelete:
			return reply(d.configDelete(req.Name))
		default: // validate
			return replyConfig(d.configValidate(req.Name, req.Content, req.RunCoreCheck))
		}
	case localapi.RequestConfigActivate:
		return reply(d.configActivate(ctx, req.Name, req.ExpectedRevision))
	case localapi.RequestShutdown:
		d.stopOnce.Do(func() { close(d.stopCh) })
		return localapi.Response{Version: localapi.ProtocolVersion, OK: true}
	default:
		return reply(fmt.Errorf("unknown request type %q", req.Type))
	}
}

// --- config store handlers ---------------------------------------------

func (d *Daemon) configGet(name string) (*localapi.ConfigPayload, error) {
	text, entry, err := d.store.Read(name)
	if err != nil {
		return nil, err
	}
	return d.configPayload(entry, text, false, true), nil
}

func (d *Daemon) configSave(name, content, expected string) (*localapi.ConfigPayload, error) {
	entry, err := d.store.Save(name, content, expected)
	if err != nil {
		return nil, err
	}
	d.opts.Logf("daemon: config %s saved (%d bytes)", name, len(content))
	// Validate the stored text but do not echo the content back: the
	// caller just sent it.
	return d.configPayload(entry, content, false, false), nil
}

func (d *Daemon) configCreate(name, content string) (*localapi.ConfigPayload, error) {
	if content == "" {
		content = configdoc.Template()
	}
	entry, err := d.store.Create(name, content)
	if err != nil {
		return nil, err
	}
	d.opts.Logf("daemon: config %s created", name)
	return d.configPayload(entry, content, false, true), nil
}

func (d *Daemon) configDelete(name string) error {
	if d.isActiveConfig(name) {
		return errors.New("the active config cannot be deleted; activate another one first")
	}
	if err := d.store.Delete(name); err != nil {
		return err
	}
	d.opts.Logf("daemon: config %s deleted (backed up)", name)
	return nil
}

func (d *Daemon) configValidate(name, content string, runCoreCheck bool) (*localapi.ConfigPayload, error) {
	if err := configdoc.ValidName(name); err != nil {
		return nil, err
	}
	entry := configdoc.Entry{Name: name, Revision: configdoc.Revision(content)}
	return d.configPayload(entry, "", runCoreCheck, false), nil
}

// configActivate switches the active config and restarts the core with
// it. This is an explicit user action: it also clears a previous manual
// stop, because running the chosen network is what "apply" means.
func (d *Daemon) configActivate(ctx context.Context, name string, expected ...string) (result error) {
	d.stopRequested.Store(false)
	return d.configActivateAfterRequest(ctx, name, expected...)
}

func (d *Daemon) configActivateAfterRequest(ctx context.Context, name string, expected ...string) (result error) {
	d.operationMu.Lock()
	defer d.operationMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if d.shutdownRequested.Load() {
		return errors.New("daemon is shutting down")
	}
	d.activationState.Store("preflight")
	d.activationError.Store("")
	operationID, err := newProfileID()
	if err != nil {
		return errors.New("cannot allocate activation identity")
	}
	d.activationTrace = activationTrace{OperationID: operationID, PreviousRevision: d.applied.Revision, Generation: d.intent.Generation}
	defer func() { d.activationTrace = activationTrace{} }()
	d.activationEvent(health.Event{Time: time.Now(), Component: d.coreComponent(), Kind: health.EventConfigStarted, Reason: "config-started", Detail: name})
	defer func() {
		if result != nil {
			d.activationEvent(health.Event{Time: time.Now(), Component: d.coreComponent(), Kind: health.EventConfigFailed, Reason: "config-failed", Detail: shortError(result)})
			state, _ := d.activationState.Load().(string)
			if state != "rolled-back" && state != "rollback-failed" && state != "unconfirmed" && state != "cancelled" {
				d.activationState.Store("failed")
			}
			d.activationError.Store(shortError(result))
		} else {
			d.activationState.Store("applied")
		}
	}()
	if d.stopRequested.Load() {
		return errors.New("operation cancelled by manual stop; start explicitly before applying")
	}
	text, entry, err := d.store.Read(name)
	if err != nil {
		return err
	}
	d.activationTrace.Revision = entry.Revision
	if len(expected) > 0 && expected[0] != "" && expected[0] != entry.Revision {
		return errors.New("configuration revision conflict; reload before applying")
	}
	if _, err := configdoc.Parse(text); err != nil {
		return fmt.Errorf("cannot activate an invalid config: %w", err)
	}
	if err := configdoc.ValidateSecureMode(text); err != nil {
		return err
	}
	if d.opts.monitorMode() {
		return errors.New("this daemon does not manage a core (monitor mode): activate is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	candidate, err := d.freezeConfig(entry.Path, text)
	if err != nil {
		return err
	}
	if d.opts.CoreBin != "" {
		if ok, note := d.coreCheckConfigContext(ctx, text); !ok {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return errors.New(note)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Freeze the requested revision; a concurrent save during preflight must
	// not silently change the meaning of apply.
	if _, current, err := d.store.Read(name); err != nil {
		return err
	} else if current.Revision != entry.Revision {
		return errors.New("configuration changed during preflight; retry apply")
	}
	previous := d.applied
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := d.recordActivation(previous, candidate, "starting"); err != nil {
		return err
	}
	d.applied = candidate
	if err := d.restartCore(); err != nil {
		return d.rollbackCandidate(previous, candidate, err)
	}
	if err := d.confirmCandidate(ctx); err != nil {
		if errors.Is(err, errCandidateUnconfirmed) {
			d.autoBlocked = true
			return errors.Join(err, d.recordActivation(previous, candidate, "unconfirmed"))
		}
		return d.rollbackCandidate(previous, candidate, err)
	}
	if err := d.saveApplied(candidate); err != nil {
		return d.rollbackCandidate(previous, candidate, err)
	}
	if d.stopRequested.Load() || d.shutdownRequested.Load() {
		return d.rollbackCandidate(previous, candidate, errors.New("activation cancelled before commit"))
	}

	oldInstance := d.instanceComponent()
	d.mu.Lock()
	d.activePath = entry.Path
	d.mu.Unlock()
	d.refreshCoreUserMode()
	if err := persistActive(d.opts.StateDir, entry.Path); err != nil {
		return d.rollbackCandidate(previous, candidate, err)
	}
	newInstance := d.instanceComponent()
	if d.stopRequested.Load() || d.shutdownRequested.Load() {
		return d.rollbackCandidate(previous, candidate, errors.New("activation cancelled before commit"))
	}
	if err := d.writeIntent(health.DesiredRun); err != nil {
		return d.rollbackCandidate(previous, candidate, err)
	}
	if err := d.recordActivation(previous, candidate, "committed"); err != nil {
		return d.rollbackCandidate(previous, candidate, err)
	}
	d.autoBlocked = false
	d.appliedRevision.Store(candidate.Revision)
	if oldInstance != newInstance {
		d.engine.Remove(oldInstance)
	}

	d.activationEvent(health.Event{
		Time:      time.Now(),
		Component: d.coreComponent(),
		Kind:      health.EventConfigApplied,
		Reason:    "config-activated",
		Detail:    name,
	})

	d.engine.SetDesired(d.coreComponent(), health.DesiredRun)
	d.engine.SetDesired(newInstance, health.DesiredRun)
	d.engine.Grace(d.coreComponent())
	d.engine.Grace(newInstance)
	d.checkRoundLocked(ctx)
	d.opts.Logf("daemon: config %s activated", name)
	return os.Remove(filepath.Join(d.opts.StateDir, "activation.json"))
}

// configPayload assembles the API answer for one config text.
func (d *Daemon) configPayload(entry configdoc.Entry, content string, runCoreCheck, includeContent bool) *localapi.ConfigPayload {
	p := &localapi.ConfigPayload{
		Entry:   d.apiEntry(entry),
		Valid:   true,
		Content: content,
	}
	if !includeContent {
		p.Content = ""
	}
	if _, err := configdoc.Parse(content); err != nil && content != "" {
		p.Valid = false
		p.ParseError = err.Error()
	}
	if runCoreCheck {
		p.CoreCheck = true
		p.CoreCheckOK, p.CoreCheckNote = d.coreCheckConfig(content)
	}
	return p
}

func (d *Daemon) apiEntry(e configdoc.Entry) localapi.ConfigEntry {
	return apiEntryFor(e, d.activeConfigPath())
}

func apiEntryFor(e configdoc.Entry, activePath string) localapi.ConfigEntry {
	return localapi.ConfigEntry{
		Name:     e.Name,
		Size:     e.Size,
		Modified: e.Modified,
		Revision: e.Revision,
		Active:   e.Path != "" && e.Path == activePath,
	}
}

// configEntriesFor lists the store for the GUI snapshot. The snapshot
// passes the active path in because it holds the daemon lock while
// assembling; this function must not take it again.
func (d *Daemon) configEntriesFor(activePath string) []localapi.ConfigEntry {
	entries, err := d.store.List()
	if err != nil {
		return nil
	}
	out := make([]localapi.ConfigEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, apiEntryFor(e, activePath))
	}
	return out
}

func (d *Daemon) isActiveConfig(name string) bool {
	path := d.activeConfigPath()
	if path == "" {
		return false
	}
	return filepath.Base(path) == name+configdoc.Ext
}

// coreCheckConfig runs the core's own --check-config on raw text. The
// core reports only through its exit code (its error log is not attached
// to this mode), so the note says so instead of inventing detail.
func (d *Daemon) coreCheckConfig(text string) (bool, string) {
	return d.coreCheckConfigContext(context.Background(), text)
}

func (d *Daemon) coreCheckConfigContext(parent context.Context, text string) (bool, string) {
	if d.opts.CoreBin == "" {
		return false, "core binary path unknown; upstream check skipped"
	}
	dir, err := os.MkdirTemp("", "easytier-mygo-check-")
	if err != nil {
		return false, "cannot prepare check: " + err.Error()
	}
	defer os.RemoveAll(dir)
	file := filepath.Join(dir, "check.toml")
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		return false, "cannot prepare check: " + err.Error()
	}
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	// Validator output may contain secrets and is never needed for the
	// verdict. Discard it instead of retaining an unbounded CombinedOutput.
	err = exec.CommandContext(ctx, d.opts.CoreBin, "--check-config", "-c", file).Run()
	if err != nil {
		if ctx.Err() != nil {
			return false, "upstream check timed out"
		}
		return false, "upstream rejected the config; raw validator output withheld"
	}
	return true, "upstream accepted the config"
}

// --- active config persistence ------------------------------------------

type activeState struct {
	Path string `json:"path"`
}

func persistActive(stateDir, path string) error {
	b, err := json.Marshal(activeState{Path: path})
	if err != nil {
		return err
	}
	return atomicState(filepath.Join(stateDir, "active-config.json"), b)
}

func loadPersistedActive(stateDir string) string {
	b, err := os.ReadFile(filepath.Join(stateDir, "active-config.json"))
	if err != nil {
		return ""
	}
	var st activeState
	if err := json.Unmarshal(b, &st); err != nil {
		return ""
	}
	if st.Path == "" {
		return ""
	}
	if _, err := os.Stat(st.Path); err != nil {
		return ""
	}
	return st.Path
}

func defaultConfigDir() string {
	if base := os.Getenv("XDG_CONFIG_HOME"); base != "" {
		return filepath.Join(base, "easytier-mygo", "networks")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config", "easytier-mygo", "networks")
	}
	return filepath.Join(os.TempDir(), "easytier-mygo", "networks")
}

func parseComponent(s string) (health.Component, error) {
	kind, id, found := strings.Cut(s, "/")
	if !found || id == "" {
		return health.Component{}, fmt.Errorf("component %q is not kind/id", s)
	}
	switch kind {
	case "core":
		return health.Component{Kind: health.KindCore, ID: id}, nil
	case "instance":
		return health.Component{Kind: health.KindInstance, ID: id}, nil
	case "service":
		return health.Component{Kind: health.KindService, ID: id}, nil
	case "probe":
		return health.Component{Kind: health.KindProbe, ID: id}, nil
	default:
		return health.Component{}, fmt.Errorf("unknown component kind %q", kind)
	}
}

// shortError renders an adapter error for details and logs: the
// CommandError message is already a single line without raw config.
func shortError(err error) string {
	if err == nil {
		return ""
	}
	return scrubDetail(err.Error())
}

// underServiceManager reports whether a service manager started this
// process. Two signals: the marker our generated unit sets explicitly,
// and the process's own cgroup path containing the unit name. The generic
// systemd INVOCATION_ID is deliberately not used — a shell that happens to
// run inside any systemd unit would pass it on to children and make a
// manual start look like a service.
func underServiceManager() bool {
	if os.Getenv("EASYTIER_MYGO_SERVICE") != "" {
		return true
	}
	if b, err := os.ReadFile("/proc/self/cgroup"); err == nil {
		return strings.Contains(string(b), "easytier-mygo-svc.service")
	}
	return false
}

// socketMode is the file mode of the local socket. A per-user socket is
// owner-only; a system socket is group-readable so its authorization
// group can connect (the group itself is checked per connection). With
// ReadOthers the socket is world-reachable, and the API tier decides:
// group members write, everyone else read-only.
func (d *Daemon) socketMode() os.FileMode {
	switch {
	case d.opts.SocketGroup == 0:
		return 0o600
	case d.opts.ReadOthers:
		return 0o666
	default:
		return 0o660
	}
}

// socketDirMode is the socket directory's mode, paired with socketMode.
func (d *Daemon) socketDirMode() os.FileMode {
	if d.opts.ReadOthers {
		return 0o755
	}
	return 0o750
}

// authorizer describes who may talk to this daemon and how.
//
// A per-user socket (no SocketGroup) lives in a 0700 directory and is
// 0600, so only its owner can reach it: every connection is that owner
// and gets write access. A system socket is root-owned with a group; the
// write group is the authorization set, an optional read group is
// read-only, and (when AllowOthersRead is set) any other local user that
// can reach the socket is read-only.
func (d *Daemon) authorizer() localapi.Authorizer {
	if d.opts.SocketGroup == 0 {
		return localapi.Authorizer{OwnerUID: uint32(os.Getuid()), Private: true}
	}
	authz := localapi.Authorizer{
		OwnerUID:        uint32(os.Getuid()),
		WriteGIDs:       []uint32{uint32(d.opts.SocketGroup)},
		AllowOthersRead: d.opts.ReadOthers,
	}
	if d.opts.SocketReadGroup != 0 {
		authz.ReadGIDs = []uint32{uint32(d.opts.SocketReadGroup)}
	}
	return authz
}

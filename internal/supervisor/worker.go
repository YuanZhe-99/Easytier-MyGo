// Package supervisor runs one EasyTier network: it owns the network's
// easytier-core process, checks it on a cadence through easytier-cli,
// feeds the health engine, performs the engine's bounded recovery actions,
// applies new configuration revisions with rollback, and keeps the traffic
// curve. The daemon holds one Worker per network.
//
// All state changes run on the worker's own goroutine (Run); the exported
// methods send commands to it or read a snapshot under a lock.
package supervisor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/coreadapter"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/fsutil"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/health"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/localapi"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/telemetry"
)

// Config describes one network's worker.
type Config struct {
	ID      string
	CoreBin string
	CLIBin  string
	// RPC is the core's management portal ("127.0.0.1:port"), always
	// explicit so the EasyTier default (15888) is never assumed.
	RPC string
	// Dir is the network's state directory (core.log, recovery.json).
	Dir string
	// ExtraArgs are appended to every core start (logging options).
	ExtraArgs []string
	Policy    health.Policy

	// FastInterval is the check cadence while a client displays the
	// network; SlowInterval otherwise.
	FastInterval, SlowInterval time.Duration
	// StartupWindow treats an unreachable portal as "starting" after a
	// start; ConfirmWindow is how long a new revision must survive.
	StartupWindow, ConfirmWindow time.Duration
	// TrafficPoints is the curve's capacity.
	TrafficPoints int

	// OnEvent receives health and configuration events (synchronously).
	OnEvent func(health.Event)
	// OnApplied reports a revision confirmed running, so the store can
	// record it as applied.
	OnApplied func(revision string)
	// OnChange is called after each check round (status may have changed).
	OnChange func()
	Logf     func(string, ...any)
	// Now is the clock (tests).
	Now func() time.Time
}

func (c *Config) defaults() {
	if c.FastInterval <= 0 {
		c.FastInterval = time.Second
	}
	if c.SlowInterval <= 0 {
		c.SlowInterval = 10 * time.Second
	}
	if c.StartupWindow <= 0 {
		c.StartupWindow = 15 * time.Second
	}
	if c.ConfirmWindow <= 0 {
		c.ConfirmWindow = 30 * time.Second
	}
	if c.TrafficPoints <= 0 {
		c.TrafficPoints = 900
	}
	if c.Logf == nil {
		c.Logf = func(string, ...any) {}
	}
	if c.OnEvent == nil {
		c.OnEvent = func(health.Event) {}
	}
	if c.OnApplied == nil {
		c.OnApplied = func(string) {}
	}
	if c.OnChange == nil {
		c.OnChange = func() {}
	}
	if c.Now == nil {
		c.Now = time.Now
	}
}

// revision is a frozen configuration copy the core can be started with.
type revision struct {
	Path, Rev string
}

// candidate is a revision being confirmed after a start or an apply.
type candidate struct {
	revision
	previous revision // zero: nothing to roll back to
	deadline time.Time
}

type command struct {
	kind   string // "connect", "disconnect", "apply", "retry", "binaries"
	rev    revision
	prev   revision
	core   string
	cli    string
	result chan error
}

// Worker supervises one network.
type Worker struct {
	cfg    Config
	cli    coreadapter.CLI
	proc   *coreProcess
	engine *health.Engine
	ring   *telemetry.Ring
	track  *telemetry.Tracker
	core   health.Component
	inst   health.Component

	cmds chan command
	fast chan bool

	// Owned by the Run goroutine.
	run        bool
	current    revision
	pending    *candidate
	recoveryAt time.Time
	recoveryOf health.Component
	isFast     bool
	// rawCore/rawInst are the latest round's observations; the displayed
	// state follows them, while recovery follows the settled verdict.
	rawCore, rawInst health.Observation

	mu       sync.Mutex
	snapshot snapshot
}

// snapshot is what readers see.
type snapshot struct {
	status localapi.NetworkStatus
	detail localapi.NetworkDetail
}

// New creates a worker; Run starts it.
func New(cfg Config) *Worker {
	cfg.defaults()
	w := &Worker{
		cfg:   cfg,
		cli:   coreadapter.CLI{Bin: cfg.CLIBin, RPC: cfg.RPC},
		proc:  newCoreProcess(cfg.CoreBin, filepath.Join(cfg.Dir, "core.log")),
		ring:  telemetry.NewRing(cfg.TrafficPoints),
		track: telemetry.NewTracker(cfg.FastInterval, 2),
		core:  health.Component{Kind: health.KindCore, ID: cfg.ID},
		inst:  health.Component{Kind: health.KindInstance, ID: cfg.ID},
		cmds:  make(chan command),
		fast:  make(chan bool, 1),
	}
	w.engine = health.New(cfg.Policy, health.WithClock(cfg.Now))
	w.engine.SetEventSink(func(e health.Event) {
		e.Detail = ScrubDetail(e.Detail)
		cfg.OnEvent(e)
	})
	w.engine.SetDesired(w.core, health.DesiredStop)
	w.engine.SetDesired(w.inst, health.DesiredStop)
	w.restoreAttempts()
	w.snapshot.status = localapi.NetworkStatus{ID: cfg.ID, State: localapi.StateStopped}
	return w
}

// Run is the worker's loop. It returns when ctx ends, after stopping the
// core.
func (w *Worker) Run(ctx context.Context) {
	if err := os.MkdirAll(w.cfg.Dir, 0o700); err != nil {
		w.cfg.Logf("network %s: state dir: %v", w.cfg.ID, err)
	}
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = w.proc.Stop(10 * time.Second)
			w.publish()
			return
		case cmd := <-w.cmds:
			cmd.result <- w.handle(ctx, cmd)
		case f := <-w.fast:
			if f == w.isFast {
				continue
			}
			w.isFast = f
		case <-w.proc.exitNotify:
		case <-timer.C:
		}
		w.check(ctx)
		timer.Reset(w.nextWake())
	}
}

// nextWake is the delay until the next check, recovery or confirmation.
func (w *Worker) nextWake() time.Duration {
	d := w.cfg.SlowInterval
	// Displayed networks, and any running network whose core is not
	// healthy yet, are checked at the fast cadence, so startup and faults
	// settle in seconds.
	if w.isFast || (w.run && w.rawCore.Level != health.LevelHealthy) {
		d = w.cfg.FastInterval
	}
	now := w.cfg.Now()
	for _, t := range []time.Time{w.recoveryAt, w.deadline()} {
		if !t.IsZero() {
			if until := t.Sub(now); until < d {
				d = max(until, 10*time.Millisecond)
			}
		}
	}
	return d
}

func (w *Worker) deadline() time.Time {
	if w.pending == nil {
		return time.Time{}
	}
	return w.pending.deadline
}

// send hands a command to the Run goroutine and waits for its result.
func (w *Worker) send(ctx context.Context, cmd command) error {
	cmd.result = make(chan error, 1)
	select {
	case w.cmds <- cmd:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-cmd.result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Connect starts the network with a frozen revision (no-op when it already
// runs that revision).
func (w *Worker) Connect(ctx context.Context, path, rev string) error {
	return w.send(ctx, command{kind: "connect", rev: revision{path, rev}})
}

// Disconnect stops the network; recovery will not restart it.
func (w *Worker) Disconnect(ctx context.Context) error {
	return w.send(ctx, command{kind: "disconnect"})
}

// Apply restarts a running network with a new revision. If the new
// revision fails within the confirmation window, the worker returns to prev.
func (w *Worker) Apply(ctx context.Context, path, rev, prevPath, prevRev string) error {
	return w.send(ctx, command{kind: "apply", rev: revision{path, rev}, prev: revision{prevPath, prevRev}})
}

// SetBinaries switches the core and CLI (another EasyTier version). A
// running network restarts with its current revision on the new core.
func (w *Worker) SetBinaries(ctx context.Context, core, cli string) error {
	return w.send(ctx, command{kind: "binaries", core: core, cli: cli})
}

// GrantRetry re-authorizes automatic recovery after the budget ran out.
func (w *Worker) GrantRetry(ctx context.Context) error {
	return w.send(ctx, command{kind: "retry"})
}

// SetFast switches between the fast (displayed) and slow cadence.
func (w *Worker) SetFast(fast bool) {
	select {
	case w.fast <- fast:
	default:
		select {
		case <-w.fast:
		default:
		}
		w.fast <- fast
	}
}

// Status returns the latest summary.
func (w *Worker) Status() localapi.NetworkStatus {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.snapshot.status
}

// Detail returns the latest peers and node facts.
func (w *Worker) Detail() localapi.NetworkDetail {
	w.mu.Lock()
	defer w.mu.Unlock()
	d := w.snapshot.detail
	d.Status = w.snapshot.status
	return d
}

// Traffic returns the curve's points after seq.
func (w *Worker) Traffic(since uint64) []localapi.TrafficPoint {
	pts := w.ring.Since(since)
	out := make([]localapi.TrafficPoint, len(pts))
	for i, p := range pts {
		out[i] = localapi.TrafficPoint{Seq: p.Seq, Time: p.Time, Rx: p.RxBytesPS, Tx: p.TxBytesPS, Gap: p.Gap}
	}
	return out
}

func (w *Worker) handle(ctx context.Context, cmd command) error {
	switch cmd.kind {
	case "connect":
		if alive, _ := w.proc.Alive(); alive && w.run && w.current.Rev == cmd.rev.Rev {
			return nil
		}
		if err := w.preflight(ctx, cmd.rev.Path); err != nil {
			w.setError(err.Error())
			return err
		}
		w.setRun(true)
		return w.start(cmd.rev, revision{})
	case "disconnect":
		w.setRun(false)
		w.pending = nil
		w.recoveryAt = time.Time{}
		err := w.proc.Stop(10 * time.Second)
		// The last round described the stopped core: a restart starts
		// from no evidence, never from the old verdict.
		w.rawCore, w.rawInst = health.Observation{}, health.Observation{}
		w.setError("")
		return err
	case "apply":
		if !w.run {
			return errors.New("network is not running")
		}
		if err := w.preflight(ctx, cmd.rev.Path); err != nil {
			return err
		}
		return w.start(cmd.rev, cmd.prev)
	case "binaries":
		if cmd.core == w.cfg.CoreBin && cmd.cli == w.cfg.CLIBin {
			return nil
		}
		w.cfg.CoreBin, w.cfg.CLIBin = cmd.core, cmd.cli
		w.cli.Bin = cmd.cli
		alive, _ := w.proc.Alive()
		_ = w.proc.Stop(10 * time.Second)
		w.proc = newCoreProcess(cmd.core, filepath.Join(w.cfg.Dir, "core.log"))
		if w.run && alive && w.current.Path != "" {
			if err := w.preflight(ctx, w.current.Path); err != nil {
				w.setError(err.Error())
				return err
			}
			return w.start(w.current, revision{})
		}
		return nil
	case "retry":
		w.engine.GrantRetry(w.core)
		w.engine.GrantRetry(w.inst)
		w.saveAttempts()
		return nil
	}
	return fmt.Errorf("unknown command %q", cmd.kind)
}

func (w *Worker) setRun(run bool) {
	w.run = run
	d := health.DesiredStop
	if run {
		d = health.DesiredRun
		w.engine.GrantRetry(w.core) // a manual start re-authorizes recovery
		w.engine.GrantRetry(w.inst)
	}
	w.engine.SetDesired(w.core, d)
	w.engine.SetDesired(w.inst, d)
}

// preflight runs the core's --check-config on a revision.
func (w *Worker) preflight(ctx context.Context, path string) error {
	return Preflight(ctx, w.cfg.CoreBin, path)
}

// Preflight runs `easytier-core --check-config -c path`, which parses a
// configuration without starting a network.
// Returns: a *ConfigError when the core rejects it.
func Preflight(ctx context.Context, coreBin, path string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, coreBin, "--check-config", "-c", path).CombinedOutput()
	if err != nil {
		detail := firstLine(string(out))
		if detail == "" {
			detail = err.Error()
		}
		return &ConfigError{Detail: ScrubDetail(detail)}
	}
	return nil
}

// ConfigError reports a revision the core refuses.
type ConfigError struct{ Detail string }

func (e *ConfigError) Error() string { return "the core rejected the configuration: " + e.Detail }

func firstLine(s string) string {
	for i, r := range s {
		if r == '\n' {
			return s[:i]
		}
	}
	return s
}

// start (re)starts the core with rev; prev, when set, is the rollback
// target if rev fails during the confirmation window.
func (w *Worker) start(rev, prev revision) error {
	_ = w.proc.Stop(10 * time.Second)
	args := append([]string{"-c", rev.Path, "--rpc-portal", w.cfg.RPC}, w.cfg.ExtraArgs...)
	if err := w.proc.Start(args); err != nil {
		w.setError(err.Error())
		return err
	}
	w.current = rev
	w.rawCore, w.rawInst = health.Observation{}, health.Observation{}
	w.pending = &candidate{revision: rev, previous: prev, deadline: w.cfg.Now().Add(w.cfg.ConfirmWindow)}
	w.engine.Grace(w.core)
	w.engine.Grace(w.inst)
	w.track = telemetry.NewTracker(w.cfg.FastInterval, 2)
	w.setError("")
	w.cfg.Logf("network %s: core started (revision %.12s)", w.cfg.ID, rev.Rev)
	return nil
}

// rollback returns from a failed candidate to its previous revision.
func (w *Worker) rollback(reason string) {
	c := w.pending
	w.pending = nil
	w.cfg.OnEvent(health.Event{Time: w.cfg.Now(), Component: w.inst, Kind: health.EventConfigRollback,
		Revision: c.Rev, PreviousRevision: c.previous.Rev, Reason: "rollback", Detail: ScrubDetail(reason)})
	msg := "The new configuration failed (" + reason + "); returned to the previous one."
	if err := w.start(c.previous, revision{}); err != nil {
		msg = "The new configuration failed and the previous one could not start: " + err.Error()
	}
	w.setError(msg)
}

// check runs one observation round.
func (w *Worker) check(ctx context.Context) {
	now := w.cfg.Now()
	if !w.run {
		w.publish()
		return
	}
	alive, why := w.proc.Alive()
	var node *coreadapter.NodeInfo
	var pairs []coreadapter.PeerRoutePair
	coreObs := health.Observation{Component: w.core, Lifecycle: health.LifecycleRunning}
	instObs := health.Observation{Component: w.inst, Lifecycle: health.LifecycleRunning}
	switch {
	case !alive:
		coreObs.Lifecycle, coreObs.Level, coreObs.Cause, coreObs.Reason, coreObs.Detail =
			health.LifecycleStopped, health.LevelFailed, health.CauseLocalFault, "process-exited", why
		instObs.Lifecycle, instObs.Level, instObs.Reason = health.LifecycleStopped, health.LevelUnknown, "core-not-running"
		if w.pending != nil && w.pending.previous.Path != "" {
			w.rollback("the core exited")
			w.publish()
			return
		}
	default:
		qctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		var err error
		node, err = w.cli.Node(qctx)
		if err == nil {
			var perr error
			if pairs, perr = w.cli.PeersVerbose(qctx); perr != nil {
				// The core answered; only the peer list is unreadable, which
				// is a collector problem and never a core fault.
				pairs = nil
				w.cfg.Logf("network %s: peer query: %v", w.cfg.ID, perr)
			}
		}
		cancel()
		w.observeCore(&coreObs, &instObs, node, pairs, err, now)
	}
	w.sampleTraffic(now, node != nil, pairs)
	w.rawCore, w.rawInst = coreObs, instObs

	actions := append(w.engine.Observe(coreObs), w.engine.Observe(instObs)...)
	if len(actions) > 0 && w.recoveryAt.IsZero() {
		a := actions[0]
		w.recoveryAt, w.recoveryOf = now.Add(a.After), a.Component
	}
	if !w.recoveryAt.IsZero() && !now.Before(w.recoveryAt) {
		w.recover()
	}
	if c := w.pending; c != nil {
		st, _ := w.engine.State(w.core)
		switch {
		case st.Level == health.LevelFailed && st.Cause == health.CauseLocalFault && c.previous.Path != "":
			w.rollback(st.Reason)
		case !now.Before(c.deadline):
			if alive, _ := w.proc.Alive(); alive {
				w.pending = nil
				w.cfg.OnApplied(c.Rev)
				w.cfg.OnEvent(health.Event{Time: now, Component: w.inst, Kind: health.EventConfigApplied,
					Revision: c.Rev, PreviousRevision: c.previous.Rev, Reason: "applied"})
			}
		}
	}
	w.publishWith(node, pairs, now)
}

func (w *Worker) observeCore(coreObs, instObs *health.Observation, node *coreadapter.NodeInfo, pairs []coreadapter.PeerRoutePair, err error, now time.Time) {
	if err != nil {
		inStartup := now.Sub(w.proc.StartedAt()) < w.cfg.StartupWindow
		kind := coreadapter.KindOf(err)
		if inStartup && kind != coreadapter.ErrUnavailable && kind != coreadapter.ErrParse && kind != coreadapter.ErrTimeout {
			// A core that just started answers nothing useful yet.
			coreObs.Lifecycle, coreObs.Level, coreObs.Cause, coreObs.Reason = health.LifecycleStarting, health.LevelDegraded, health.CauseAwaiting, "starting"
			instObs.Level, instObs.Reason = health.LevelUnknown, "core-starting"
			return
		}
		switch kind {
		case coreadapter.ErrTimeout:
			coreObs.Level, coreObs.Cause, coreObs.Reason = health.LevelFailed, health.CauseUnresponsive, "rpc-timeout"
		case coreadapter.ErrRPCUnreachable, coreadapter.ErrNoInstances:
			reason := "rpc-unreachable"
			if coreadapter.KindOf(err) == coreadapter.ErrNoInstances {
				reason = "no-instances"
			}
			if inStartup {
				coreObs.Lifecycle, coreObs.Level, coreObs.Cause, coreObs.Reason = health.LifecycleStarting, health.LevelDegraded, health.CauseAwaiting, "starting"
			} else {
				coreObs.Level, coreObs.Cause, coreObs.Reason = health.LevelFailed, health.CauseLocalFault, reason
			}
		default:
			coreObs.Level, coreObs.Cause, coreObs.Reason = health.LevelUnknown, health.CauseCollector, "collector-"+coreadapter.KindOf(err).String()
		}
		coreObs.Detail = ScrubDetail(err.Error())
		instObs.Level, instObs.Reason = health.LevelUnknown, "core-not-answering"
		return
	}
	coreObs.Level = health.LevelHealthy
	switch {
	case node.IPv4Addr == "":
		instObs.Level, instObs.Cause, instObs.Reason = health.LevelDegraded, health.CauseAwaiting, "awaiting-address"
	case remotePeers(node, pairs) == 0:
		instObs.Level, instObs.Cause, instObs.Reason = health.LevelDegraded, health.CauseAwaiting, "no-peers"
	default:
		instObs.Level = health.LevelHealthy
	}
}

// recover runs the scheduled recovery action: a core restart with the
// current revision.
func (w *Worker) recover() {
	comp := w.recoveryOf
	w.recoveryAt = time.Time{}
	if _, ok := w.engine.BeginRecovery(comp); !ok {
		return
	}
	w.saveAttempts()
	_ = w.proc.Stop(10 * time.Second)
	args := append([]string{"-c", w.current.Path, "--rpc-portal", w.cfg.RPC}, w.cfg.ExtraArgs...)
	err := w.proc.Start(args)
	w.engine.EndRecovery(comp, err)
	w.engine.Grace(w.core)
	w.engine.Grace(w.inst)
	w.track = telemetry.NewTracker(w.cfg.FastInterval, 2)
}

// sampleTraffic feeds the counters (or a gap) into the curve.
func (w *Worker) sampleTraffic(now time.Time, answered bool, pairs []coreadapter.PeerRoutePair) {
	sample := telemetry.Sample{Time: now, Generation: w.current.Rev + w.proc.StartedAt().String()}
	if !answered || pairs == nil {
		sample.Missing = true
	}
	for _, p := range pairs {
		for _, c := range p.Peer.Conns {
			if c.IsClosed {
				continue
			}
			sample.Conns = append(sample.Conns, telemetry.ConnSample{ConnID: c.ConnID, PeerID: c.PeerID,
				RxBytes: uint64(c.Stats.RxBytes), TxBytes: uint64(c.Stats.TxBytes), RxPackets: uint64(c.Stats.RxPackets), TxPackets: uint64(c.Stats.TxPackets)})
		}
	}
	if pt, ok := w.track.Observe(sample); ok {
		w.ring.Add(pt)
	}
}

func (w *Worker) setError(msg string) {
	w.mu.Lock()
	w.snapshot.status.Error = ScrubDetail(msg)
	w.mu.Unlock()
}

func (w *Worker) publish() { w.publishWith(nil, nil, w.cfg.Now()) }

// publishWith refreshes the snapshot from the engine and, when a round
// answered, from the node and peer data.
// Notes: a stopped network publishes no health verdict (level unknown,
// no reason), so a green state never outlives its evidence.
func (w *Worker) publishWith(node *coreadapter.NodeInfo, pairs []coreadapter.PeerRoutePair, now time.Time) {
	coreSt, _ := w.engine.State(w.core)
	instSt, _ := w.engine.State(w.inst)
	w.mu.Lock()
	s := &w.snapshot.status
	s.Run = w.run
	s.Applied = w.current.Rev
	s.Lifecycle = coreSt.Lifecycle
	s.Recovery = coreSt.Recovery
	s.StartedAt = w.proc.StartedAt()
	if w.run {
		s.CheckedAt = now
	}
	pick := w.rawCore
	if pick.Level == health.LevelHealthy {
		pick = w.rawInst
	}
	if coreSt.Level == health.LevelFailed {
		pick = health.Observation{Level: coreSt.Level, Cause: coreSt.Cause, Reason: coreSt.Reason}
	}
	s.Level, s.Cause, s.Reason = pick.Level, pick.Cause, pick.Reason
	_ = instSt
	s.State = stateOf(w.run, coreSt, w.rawCore, w.rawInst)
	if node != nil {
		node.Config = "" // raw TOML with the network secret: never kept
		s.Hostname = node.Hostname
		s.VirtualIPv4 = node.IPv4Addr
		s.CoreVersion = node.Version
		s.NATType = node.STUNInfo.UDPNATType.String()
		s.PeerCount = remotePeers(node, pairs)
		w.snapshot.detail = detailOf(node, pairs, now)
	}
	if !w.run {
		// A stopped network has no verdict; the engine keeps the last one
		// for recovery bookkeeping only.
		s.Lifecycle = health.LifecycleStopped
		if alive, _ := w.proc.Alive(); alive {
			s.Lifecycle = health.LifecycleStopping
		}
		s.Level, s.Cause, s.Reason = health.LevelUnknown, health.CauseNone, ""
		s.VirtualIPv4, s.PeerCount, s.RxRate, s.TxRate = "", 0, 0, 0
		w.snapshot.detail = localapi.NetworkDetail{}
	}
	if last, ok := w.ring.Last(); ok {
		s.LastSeq = last.Seq
		if w.run && !last.Gap {
			s.RxRate, s.TxRate = last.RxBytesPS, last.TxBytesPS
		}
	}
	w.mu.Unlock()
	w.cfg.OnChange()
}

// stateOf maps the latest round to the plain-language state. Failed
// needs the settled verdict, so one bad round never shows as a failure.
func stateOf(run bool, settled health.State, core, inst health.Observation) localapi.State {
	switch {
	case !run:
		return localapi.StateStopped
	case settled.Level == health.LevelFailed:
		return localapi.StateFailed
	case core.Level == health.LevelUnknown && core.Cause == health.CauseCollector:
		return localapi.StateUnknown
	case core.Level != health.LevelHealthy:
		return localapi.StateStarting
	case inst.Level == health.LevelHealthy:
		return localapi.StateConnected
	case inst.Cause == health.CauseAwaiting:
		return localapi.StateConnecting
	case inst.Level == health.LevelUnknown:
		return localapi.StateStarting
	default:
		return localapi.StateDegraded
	}
}

// remotePeers counts the other nodes in the network.
func remotePeers(node *coreadapter.NodeInfo, pairs []coreadapter.PeerRoutePair) int {
	n := 0
	for _, p := range pairs {
		if p.Route.PeerID != node.PeerID {
			n++
		}
	}
	return n
}

// detailOf builds the peer list (local node first) from one round.
func detailOf(node *coreadapter.NodeInfo, pairs []coreadapter.PeerRoutePair, now time.Time) localapi.NetworkDetail {
	d := localapi.NetworkDetail{
		Listeners:  node.Listeners,
		ProxyCIDRs: node.ProxyCIDRs,
		PublicIPs:  node.STUNInfo.PublicIP,
		SampledAt:  now,
	}
	d.Peers = append(d.Peers, localapi.PeerInfo{Hostname: node.Hostname, IPv4: node.IPv4Addr, Local: true,
		NATType: node.STUNInfo.UDPNATType.String(), Version: node.Version, Subnets: node.ProxyCIDRs})
	for _, p := range pairs {
		if p.Route.PeerID == node.PeerID {
			continue
		}
		pi := localapi.PeerInfo{
			Hostname: p.Route.Hostname,
			Cost:     int(p.Route.Cost),
			Direct:   len(p.Peer.Conns) > 0,
			NATType:  p.Route.STUNInfo.UDPNATType.String(),
			Version:  p.Route.Version,
			Subnets:  p.Route.ProxyCIDRs,

			PublicServer: p.Route.FeatureFlag["is_public_server"],
		}
		if p.Route.IPv4Addr != nil {
			pi.IPv4 = p.Route.IPv4Addr.String()
		}
		var lat, loss float64
		open := 0
		for _, c := range p.Peer.Conns {
			if c.IsClosed {
				continue
			}
			open++
			ci := localapi.ConnInfo{Tunnel: c.Tunnel.TunnelType, LatencyMS: float64(c.Stats.LatencyUS) / 1000,
				LossRate: c.LossRate, RxBytes: uint64(c.Stats.RxBytes), TxBytes: uint64(c.Stats.TxBytes)}
			if c.Tunnel.LocalAddr != nil {
				ci.Local = c.Tunnel.LocalAddr.URL
			}
			if c.Tunnel.RemoteAddr != nil {
				ci.Remote = c.Tunnel.RemoteAddr.URL
			}
			pi.Conns = append(pi.Conns, ci)
			pi.RxBytes += uint64(c.Stats.RxBytes)
			pi.TxBytes += uint64(c.Stats.TxBytes)
			lat += ci.LatencyMS
			loss += c.LossRate
			if pi.Tunnel == "" {
				pi.Tunnel = c.Tunnel.TunnelType
			}
		}
		if open > 0 {
			pi.LatencyMS, pi.LossRate = lat/float64(open), loss/float64(open)
		} else {
			pi.LatencyMS = float64(p.Route.PathLatency)
		}
		d.Peers = append(d.Peers, pi)
	}
	return d
}

// attemptsPath persists the recovery budget so a daemon restart cannot
// hand out fresh attempts.
func (w *Worker) attemptsPath() string { return filepath.Join(w.cfg.Dir, "recovery.json") }

func (w *Worker) saveAttempts() {
	if err := fsutil.WriteJSON(w.attemptsPath(), w.engine.Attempts()); err != nil {
		w.cfg.Logf("network %s: save recovery budget: %v", w.cfg.ID, err)
	}
}

func (w *Worker) restoreAttempts() {
	var snaps []health.AttemptSnapshot
	if ok, err := fsutil.ReadJSON(w.attemptsPath(), &snaps); !ok || err != nil {
		return
	}
	for _, s := range snaps {
		w.engine.SeedAttempts(s.Component, s.Times)
	}
}

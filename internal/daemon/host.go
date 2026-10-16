// Package daemon is the EasyTier MyGo daemon: it owns the network store,
// runs one supervisor.Worker per network, serves the local API, and decides
// which networks actually run.
//
// A network runs when the user wants it (profiles.Profile.Run) and the run
// mode allows it: a GUI session is attached, or the helper is in background
// mode. The same daemon runs in two hosts (localapi.Host): as the user's
// child of the GUI (no privileges, tied to the GUI by a lifeline) or as the
// privileged helper (started on demand by the OS, exits when idle unless in
// background mode).
package daemon

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/configdoc"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/fsutil"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/health"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/localapi"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/profiles"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/supervisor"
)

// Options configures a daemon.
type Options struct {
	Host     localapi.Host
	Version  string
	StateDir string
	// Listener, when set, is used instead of creating SocketPath (systemd
	// or launchd socket activation hands one over).
	Listener   net.Listener
	SocketPath string
	// Authorizer decides each connection's tier.
	Authorizer localapi.Authorizer
	// SocketGroup chowns a created socket to the helper's group (0: none).
	SocketGroup int
	CoreBin     string
	CLIBin      string
	// RPCPortBase is the first port tried for cores' management portals.
	RPCPortBase int
	// Privileged reports whether cores may create TUN devices.
	Privileged bool
	// Lifeline ends the daemon when it reaches EOF (the GUI's pipe).
	Lifeline *os.File
	// IdleExit ends a helper that has no session, no running network and
	// is not in background mode for this long (0: never).
	IdleExit time.Duration
	Policy   health.Policy
	// OnBackground persists background mode with the OS (helper: start at
	// boot or not). Nil means nothing to do.
	OnBackground func(on bool) error
	// Worker overrides cadences (tests); zero fields take defaults.
	Worker supervisor.Config
	Logf   func(string, ...any)
}

// Daemon is a running daemon.
type Daemon struct {
	opts      Options
	store     *profiles.Store
	events    *eventStore
	startedAt time.Time

	mu       sync.Mutex
	workers  map[string]*workerEntry
	sessions map[string]*session
	settings localapi.Settings
	// changed wakes the session pushers.
	changed chan struct{}
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	ports   map[int]string
	lastUse time.Time
}

type workerEntry struct {
	w      *supervisor.Worker
	cancel context.CancelFunc
	port   int
	// running is the daemon's decision: whether the worker should run now.
	running bool
}

// Run starts the daemon and blocks until ctx ends, the lifeline closes,
// the idle timer fires, or a shutdown request arrives.
func Run(ctx context.Context, opts Options) error {
	if opts.Logf == nil {
		opts.Logf = func(string, ...any) {}
	}
	if opts.RPCPortBase == 0 {
		opts.RPCPortBase = 25888
	}
	unlock, err := lockState(opts.StateDir)
	if err != nil {
		return err
	}
	defer unlock()
	store, err := profiles.Open(opts.StateDir)
	if err != nil {
		return err
	}
	events, err := newEventStore(opts.StateDir)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	d := &Daemon{
		opts: opts, store: store, events: events, startedAt: time.Now(),
		workers: map[string]*workerEntry{}, sessions: map[string]*session{},
		changed: make(chan struct{}, 1), ctx: ctx, cancel: cancel,
		ports: map[int]string{}, lastUse: time.Now(),
	}
	if _, err := fsutil.ReadJSON(d.settingsPath(), &d.settings); err != nil {
		opts.Logf("settings: %v", err)
	}

	ln := opts.Listener
	if ln == nil {
		ln, err = localapi.ListenWith(localapi.ListenOptions{Path: opts.SocketPath, Mode: socketMode(opts), Group: opts.SocketGroup})
		if err != nil {
			return err
		}
	}
	for _, p := range store.List() {
		d.addWorker(p.ID)
	}
	d.reconcile()

	if opts.Lifeline != nil {
		go func() {
			buf := make([]byte, 1)
			for {
				if _, err := opts.Lifeline.Read(buf); err != nil {
					opts.Logf("lifeline closed: stopping")
					cancel()
					return
				}
			}
		}()
	}
	if opts.IdleExit > 0 {
		go d.idleWatch()
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- localapi.ServeWith(ctx, ln, d, opts.Authorizer) }()

	select {
	case <-ctx.Done():
	case err = <-serveErr:
		cancel()
	}
	d.wg.Wait() // workers stop their cores
	if opts.Listener == nil {
		_ = os.Remove(opts.SocketPath)
	}
	return err
}

func socketMode(o Options) os.FileMode {
	if o.SocketGroup != 0 {
		return 0o660
	}
	return 0o600
}

func (d *Daemon) settingsPath() string { return filepath.Join(d.opts.StateDir, "settings.json") }

// addWorker creates and starts the worker for one stored network.
func (d *Daemon) addWorker(id string) {
	cfg := d.opts.Worker
	cfg.ID = id
	cfg.CoreBin, cfg.CLIBin = d.opts.CoreBin, d.opts.CLIBin
	cfg.Dir = filepath.Join(d.opts.StateDir, "run", id)
	cfg.Policy = d.opts.Policy
	cfg.Logf = d.opts.Logf
	cfg.OnEvent = d.events.appendEvent
	cfg.OnApplied = func(rev string) {
		if _, err := d.store.MarkApplied(id, rev); err != nil {
			d.opts.Logf("network %s: record applied revision: %v", id, err)
		}
	}
	cfg.OnChange = d.notify
	d.mu.Lock()
	port := d.allocPortLocked(id)
	d.mu.Unlock()
	cfg.RPC = fmt.Sprintf("127.0.0.1:%d", port)
	w := supervisor.New(cfg)
	wctx, wcancel := context.WithCancel(d.ctx)
	d.mu.Lock()
	d.workers[id] = &workerEntry{w: w, cancel: wcancel, port: port}
	d.mu.Unlock()
	d.wg.Add(1)
	go func() { defer d.wg.Done(); w.Run(wctx) }()
}

// allocPortLocked picks the lowest free management port from the base.
func (d *Daemon) allocPortLocked(id string) int {
	for port := d.opts.RPCPortBase; port < d.opts.RPCPortBase+1000; port++ {
		if _, used := d.ports[port]; used {
			continue
		}
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			continue
		}
		ln.Close()
		d.ports[port] = id
		return port
	}
	return d.opts.RPCPortBase
}

// removeWorker stops and forgets one network's worker.
func (d *Daemon) removeWorker(id string) {
	d.mu.Lock()
	e := d.workers[id]
	delete(d.workers, id)
	if e != nil {
		delete(d.ports, e.port)
	}
	d.mu.Unlock()
	if e != nil {
		_ = e.w.Disconnect(context.Background())
		e.cancel()
	}
}

// allowsRunning reports whether the run mode lets networks run now.
func (d *Daemon) allowsRunningLocked() bool {
	return len(d.sessions) > 0 || (d.opts.Host == localapi.HostHelper && d.settings.Background)
}

// reconcile starts and stops workers so that exactly the networks the user
// wants, and the mode allows, are running.
func (d *Daemon) reconcile() {
	d.mu.Lock()
	allowed := d.allowsRunningLocked()
	type change struct {
		id  string
		e   *workerEntry
		run bool
	}
	var changes []change
	for _, p := range d.store.List() {
		e := d.workers[p.ID]
		if e == nil {
			continue
		}
		want := p.Run && allowed
		if want != e.running {
			e.running = want
			changes = append(changes, change{p.ID, e, want})
		}
	}
	d.mu.Unlock()
	for _, c := range changes {
		var err error
		if c.run {
			if err = d.startWorker(c.id, c.e.w); err != nil {
				d.mu.Lock()
				c.e.running = false
				d.mu.Unlock()
			}
		} else {
			err = c.e.w.Disconnect(d.ctx)
		}
		if err != nil {
			d.opts.Logf("network %s: %v", c.id, err)
		}
	}
	d.notify()
}

// startWorker connects a network with its applied revision (or the saved
// document when it was never applied).
func (d *Daemon) startWorker(id string, w *supervisor.Worker) error {
	p, err := d.store.Get(id)
	if err != nil {
		return err
	}
	if err := d.checkPrivilege(id); err != nil {
		return err
	}
	path, rev := "", p.Applied
	if rev != "" {
		if fp, ok := d.store.FrozenPath(id, rev); ok {
			path = fp
		}
	}
	if path == "" {
		if path, rev, err = d.store.Freeze(id); err != nil {
			return err
		}
	}
	return w.Connect(d.ctx, path, rev)
}

// errNeedsHelper refuses a TUN network in a daemon without privileges.
var errNeedsHelper = errors.New("this network creates a virtual network device, which needs the privileged helper")

func (d *Daemon) checkPrivilege(id string) error {
	text, _, err := d.store.Config(id)
	if err != nil {
		return err
	}
	if d.opts.Host == localapi.HostHelper {
		if err := checkHelperSafety(text); err != nil {
			return &supervisor.ConfigError{Detail: err.Error()}
		}
	}
	if d.opts.Privileged {
		return nil
	}
	if sum, err := configdoc.Parse(text); err == nil && sum.NoTun {
		return nil
	}
	return errNeedsHelper
}

// notify wakes session pushers (coalesced).
func (d *Daemon) notify() {
	select {
	case d.changed <- struct{}{}:
	default:
	}
}

// idleWatch ends an on-demand helper nobody uses.
func (d *Daemon) idleWatch() {
	t := time.NewTicker(min(5*time.Second, max(d.opts.IdleExit/4, 10*time.Millisecond)))
	defer t.Stop()
	for {
		select {
		case <-d.ctx.Done():
			return
		case <-t.C:
		}
		d.mu.Lock()
		busy := d.allowsRunningLocked()
		for _, e := range d.workers {
			busy = busy || e.running
		}
		if busy {
			d.lastUse = time.Now()
		}
		idle := time.Since(d.lastUse)
		d.mu.Unlock()
		if idle >= d.opts.IdleExit {
			d.opts.Logf("idle for %v: exiting", idle.Round(time.Second))
			d.cancel()
			return
		}
	}
}

// touch records activity for the idle timer.
func (d *Daemon) touch() {
	d.mu.Lock()
	d.lastUse = time.Now()
	d.mu.Unlock()
}

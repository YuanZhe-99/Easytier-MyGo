package daemon

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/configdoc"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/localapi"
	"github.com/pelletier/go-toml/v2"
)

// networkManager is the B runtime coordinator. Registration is fixed before Run;
// each worker owns A's locks, core, health engine, budget and sampling evidence.
type networkManager struct {
	stateDir       string
	namesMu        sync.RWMutex
	names          map[*Daemon]string
	workers        map[string]*Daemon
	checks         chan struct{}
	stop           chan struct{}
	stopOnce       sync.Once
	mutations      sync.Mutex
	progressMu     sync.Mutex
	progress       map[*Daemon]localapi.NetworkProgress
	warningsMu     sync.Mutex
	warnings       map[*Daemon][]localapi.ResourceWarning
	notifier       sdNotifier
	heartbeatMu    sync.Mutex
	heartbeat      map[*Daemon]bool
	notifyFailOnce sync.Once
}

func newNetworkManager(workers map[string]*Daemon) (*networkManager, error) {
	if len(workers) == 0 || len(workers) > 4 {
		return nil, errors.New("network manager requires one to four workers")
	}
	copy := make(map[string]*Daemon, len(workers))
	states, rpcs := map[string]bool{}, map[string]bool{}
	var resources []profileResources
	for id, worker := range workers {
		if id == "" || worker == nil || states[worker.opts.StateDir] || rpcs[worker.opts.RPC] {
			return nil, errors.New("duplicate or invalid network runtime")
		}
		states[worker.opts.StateDir] = true
		endpoint, err := netip.ParseAddrPort(worker.opts.RPC)
		if err != nil || !endpoint.Addr().IsLoopback() || endpoint.Port() == 0 {
			return nil, errors.New("network RPC must use an explicit loopback address and nonzero port")
		}
		if worker.managed() {
			text, err := os.ReadFile(worker.runtimePath())
			if err != nil {
				return nil, err
			}
			if err := validateNetworkPrivilege(string(text)); err != nil {
				return nil, err
			}
			projected, err := projectResources(id, string(text), worker.opts.RPC)
			if err != nil {
				return nil, err
			}
			for _, previous := range resources {
				for _, conflict := range resourceConflicts(previous, projected) {
					if !conflict.Possible {
						return nil, fmt.Errorf("resource conflict: profile %s field %s and profile %s field %s (%s)", conflict.FirstID, conflict.FirstField, conflict.SecondID, conflict.SecondField, conflict.Resource)
					}
				}
			}
			resources = append(resources, projected)
		}
		rpcs[worker.opts.RPC] = true
		copy[id] = worker
	}
	m := &networkManager{workers: copy, checks: make(chan struct{}, 2), stop: make(chan struct{}), progress: make(map[*Daemon]localapi.NetworkProgress), warnings: make(map[*Daemon][]localapi.ResourceWarning)}
	m.setResourceWarnings(resources)
	return m, nil
}

func (m *networkManager) target(id string) (*Daemon, error) {
	if id != "" {
		if worker := m.workers[id]; worker != nil {
			return worker, nil
		}
		return nil, errors.New("unknown profile target")
	}
	if len(m.workers) != 1 {
		return nil, errors.New("profile_id is required with multiple networks")
	}
	for _, worker := range m.workers {
		return worker, nil
	}
	return nil, errors.New("no network target")
}

func (m *networkManager) Handle(ctx context.Context, req localapi.Request, access localapi.Access) localapi.Response {
	refuse := func(err error) localapi.Response {
		return localapi.Response{Version: localapi.ProtocolVersion, Error: err.Error()}
	}
	if !access.Allows(req.Type) {
		return refuse(localapi.ErrForbidden{Tier: access, Type: req.Type})
	}
	if req.Type == localapi.RequestProfileRename {
		if req.ProfileID == "" {
			return refuse(errors.New("profile.rename requires explicit profile_id"))
		}
		worker, err := m.target(req.ProfileID)
		if err != nil {
			return refuse(err)
		}
		if err := m.renameProfile(ctx, req.ProfileID, req.DisplayName); err != nil {
			return refuse(err)
		}
		return m.mutationReply(worker, access, nil)
	}
	if req.Type == localapi.RequestDiagnosticBundle {
		if req.ProfileID != "" {
			return refuse(errors.New("diagnostics.networks is global; omit profile_id"))
		}
		return localapi.Response{Version: localapi.ProtocolVersion, OK: true, DiagnosticBundle: localapi.DiagnosticNetworks(m.snapshots())}
	}
	if req.Type == localapi.RequestNetworks {
		if req.ProfileID != "" {
			return refuse(errors.New("networks.status is global; omit profile_id"))
		}
		states := m.snapshots()
		for _, state := range states {
			state.Daemon.Access = string(access)
		}
		return localapi.Response{Version: localapi.ProtocolVersion, OK: true, Networks: states}
	}
	if req.Type == localapi.RequestShutdown {
		if req.ProfileID != "" {
			return refuse(errors.New("shutdown is global; omit profile_id"))
		}
		m.stopOnce.Do(func() { close(m.stop) })
		return localapi.Response{Version: localapi.ProtocolVersion, OK: true}
	}
	worker, err := m.target(req.ProfileID)
	if err != nil {
		return refuse(err)
	}
	if req.Type == localapi.RequestConfigDelete {
		m.mutations.Lock()
		defer m.mutations.Unlock()
		_, entry, err := worker.store.Read(req.Name)
		if err != nil {
			return refuse(err)
		}
		for _, other := range m.workers {
			if !other.operationMu.TryLock() {
				return refuse(errors.New("another network is busy; retry deletion"))
			}
			used := other.activeConfigPath() == entry.Path || other.applied.Source == entry.Path
			other.operationMu.Unlock()
			if used {
				return refuse(errors.New("config is referenced by a network; apply another document first"))
			}
		}
		req.ProfileID = ""
		return worker.Handle(ctx, req, access)
	}
	if req.Type == localapi.RequestCoreStop {
		// Stop needs no resource reservation: profiles retain their reservations
		// while stopped. Do not queue it behind another network's slow apply.
		return m.mutationReply(worker, access, worker.stopCore())
	}
	if req.Type == localapi.RequestCoreStart || req.Type == localapi.RequestConfigActivate {
		worker.stopRequested.Store(false)
		m.mutations.Lock()
		defer m.mutations.Unlock()
		if ctx.Err() != nil {
			return refuse(ctx.Err())
		}
		if worker.stopRequested.Load() {
			return refuse(errors.New("operation cancelled by manual stop"))
		}
	}
	if req.Type == localapi.RequestConfigActivate {
		text, entry, err := worker.store.Read(req.Name)
		if err != nil {
			return refuse(err)
		}
		if req.ExpectedRevision != "" && req.ExpectedRevision != entry.Revision {
			return refuse(errors.New("configuration revision conflict; reload before applying"))
		}
		if err := m.validateCandidate(req.ProfileID, worker, text); err != nil {
			return refuse(err)
		}
		// The worker rechecks this frozen revision after its preflight.
		req.ExpectedRevision = entry.Revision
		err = worker.configActivateAfterRequest(ctx, req.Name, req.ExpectedRevision)
		m.refreshResourceWarnings()
		return m.mutationReply(worker, access, err)
	}
	if req.Type == localapi.RequestCoreStart {
		text, err := os.ReadFile(worker.runtimePath())
		if err != nil {
			return refuse(err)
		}
		if err := m.validateCandidate(req.ProfileID, worker, string(text)); err != nil {
			return refuse(err)
		}
		return m.mutationReply(worker, access, worker.startCoreAfterRequest())
	}
	req.ProfileID = ""
	response := worker.Handle(ctx, req, access)
	if response.Status != nil {
		m.attachProgress(worker, response.Status)
	}
	return response
}

func (m *networkManager) mutationReply(worker *Daemon, access localapi.Access, err error) localapi.Response {
	state := worker.snapshot()
	m.attachProgress(worker, state)
	state.Daemon.Access = string(access)
	r := localapi.Response{Version: localapi.ProtocolVersion, OK: err == nil, Status: state}
	if err != nil {
		r.Error = err.Error()
	}
	return r
}

func (m *networkManager) validateCandidate(id string, worker *Daemon, text string) error {
	if err := validateNetworkPrivilege(text); err != nil {
		return err
	}
	if err := configdoc.ValidateSecureMode(text); err != nil {
		return err
	}
	candidate, err := projectResources(id, text, worker.opts.RPC)
	if err != nil {
		return err
	}
	for otherID, other := range m.workers {
		if other == worker {
			continue
		}
		if !other.operationMu.TryLock() {
			return errors.New("another network is busy; retry resource validation")
		}
		otherText, readErr := os.ReadFile(other.runtimePath())
		other.operationMu.Unlock()
		if readErr != nil {
			return errors.New("cannot verify other network resources")
		}
		resources, err := projectResources(otherID, string(otherText), other.opts.RPC)
		if err != nil {
			return err
		}
		for _, conflict := range resourceConflicts(resources, candidate) {
			if !conflict.Possible {
				return fmt.Errorf("resource conflict: profile %s field %s and profile %s field %s (%s)", conflict.FirstID, conflict.FirstField, conflict.SecondID, conflict.SecondField, conflict.Resource)
			}
		}
	}
	return nil
}

func validateNetworkPrivilege(text string) error {
	if os.Geteuid() != 0 {
		var value struct {
			Flags struct {
				NoTun bool `toml:"no_tun"`
			} `toml:"flags"`
		}
		if err := toml.Unmarshal([]byte(text), &value); err != nil || !value.Flags.NoTun {
			return errors.New("user network requires no_tun=true; TUN activation needs an explicitly privileged service")
		}
	}
	return nil
}

func (m *networkManager) snapshots() map[string]*localapi.Status {
	result := make(map[string]*localapi.Status, len(m.workers))
	for id, worker := range m.workers {
		result[id] = worker.snapshot()
		m.attachProgress(worker, result[id])
	}
	return result
}

func (m *networkManager) attachProgress(worker *Daemon, state *localapi.Status) {
	m.namesMu.RLock()
	state.ProfileName = m.names[worker]
	m.namesMu.RUnlock()
	m.progressMu.Lock()
	p, ok := m.progress[worker]
	m.progressMu.Unlock()
	if !ok {
		p.Phase = "pending"
	}
	if p.Phase == "queued" {
		p.QueueDelayMS = time.Since(p.QueuedAt).Milliseconds()
	}
	state.NetworkProgress = &p
	m.warningsMu.Lock()
	state.ResourceWarnings = append([]localapi.ResourceWarning(nil), m.warnings[worker]...)
	m.warningsMu.Unlock()
}

// Called under the manager mutation lock, after apply has released the worker
// lock; background recovery never changes resource reservations.
func (m *networkManager) refreshResourceWarnings() {
	var resources []profileResources
	for id, worker := range m.workers {
		if !worker.operationMu.TryLock() {
			resources = append(resources, profileResources{ID: id, Unverified: []string{"runtime"}})
			continue
		}
		text, err := os.ReadFile(worker.runtimePath())
		worker.operationMu.Unlock()
		if err != nil {
			resources = append(resources, profileResources{ID: id, Unverified: []string{"runtime"}})
			continue
		}
		resource, err := projectResources(id, string(text), worker.opts.RPC)
		if err != nil {
			resource = profileResources{ID: id, Unverified: []string{"runtime"}}
		}
		resources = append(resources, resource)
	}
	m.setResourceWarnings(resources)
}

func (m *networkManager) setResourceWarnings(resources []profileResources) {
	sort.Slice(resources, func(i, j int) bool { return resources[i].ID < resources[j].ID })
	warnings := make(map[*Daemon][]localapi.ResourceWarning)
	for index, r := range resources {
		worker := m.workers[r.ID]
		for _, field := range r.Unverified {
			warnings[worker] = append(warnings[worker], localapi.ResourceWarning{ProfileID: r.ID, Field: field, Reason: "resource projection unverified"})
		}
		for _, other := range resources[:index] {
			for _, conflict := range resourceConflicts(r, other) {
				if !conflict.Possible {
					continue
				}
				warnings[worker] = append(warnings[worker], localapi.ResourceWarning{ProfileID: r.ID, Field: conflict.FirstField, OtherProfileID: other.ID, OtherField: conflict.SecondField, Reason: "possible conflict; platform/routing behavior unverified"})
				peer := m.workers[other.ID]
				warnings[peer] = append(warnings[peer], localapi.ResourceWarning{ProfileID: other.ID, Field: conflict.SecondField, OtherProfileID: r.ID, OtherField: conflict.FirstField, Reason: "possible conflict; platform/routing behavior unverified"})
			}
		}
	}
	m.warningsMu.Lock()
	m.warnings = warnings
	m.warningsMu.Unlock()
}

func (m *networkManager) markProgress(worker *Daemon, phase string, at time.Time) {
	m.progressMu.Lock()
	defer m.progressMu.Unlock()
	p := m.progress[worker]
	p.Phase = phase
	switch phase {
	case "queued":
		p.QueuedAt = at
		p.StartedAt = time.Time{}
		p.QueueDelayMS = 0
	case "checking":
		p.StartedAt = at
		p.QueueDelayMS = at.Sub(p.QueuedAt).Milliseconds()
	case "idle":
		p.CompletedAt = at
	}
	m.progress[worker] = p
}

func (m *networkManager) check(ctx context.Context, worker *Daemon) {
	m.markProgress(worker, "queued", time.Now())
	select {
	case m.checks <- struct{}{}:
		defer func() { <-m.checks }()
	case <-ctx.Done():
		m.markProgress(worker, "stopped", time.Now())
		return
	}
	if ctx.Err() != nil {
		m.markProgress(worker, "stopped", time.Now())
		return
	}
	m.markProgress(worker, "checking", time.Now())
	worker.runRound(ctx)
	m.markProgress(worker, "idle", time.Now())
	if ctx.Err() == nil {
		m.reportNetworkProgress(worker)
	}
}

// A watchdog epoch requires a completed round from every registered worker,
// including manually stopped ones. A fast peer cannot feed a stuck peer's epoch.
func (m *networkManager) reportNetworkProgress(worker *Daemon) {
	if m.notifier == nil {
		return
	}
	m.heartbeatMu.Lock()
	defer m.heartbeatMu.Unlock()
	if m.heartbeat == nil {
		m.heartbeat = make(map[*Daemon]bool)
	}
	m.heartbeat[worker] = true
	if len(m.heartbeat) != len(m.workers) {
		return
	}
	clear(m.heartbeat)
	if err := m.notifier.Watchdog(); err != nil {
		m.notifyFailOnce.Do(func() { worker.opts.Logf("network manager watchdog notification failed: %v", err) })
	}
}

// Run starts only the workers' lifecycle/check loops, never nested API sockets.
// The socket owner must cancel this context and wait before returning.
func (m *networkManager) Run(ctx context.Context) error {
	// Publish exit guards immediately, before waiting for a query or operation
	// lock, so late background recoveries cannot start a core during teardown.
	guardsDone := make(chan struct{})
	go func() {
		defer close(guardsDone)
		<-ctx.Done()
		for _, worker := range m.workers {
			worker.shutdownRequested.Store(true)
		}
	}()
	var wg sync.WaitGroup
	ids := make([]string, 0, len(m.workers))
	for id := range m.workers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for index, id := range ids {
		worker := m.workers[id]
		wg.Add(1)
		go func() { defer wg.Done(); worker.runProbes(ctx) }()
		wg.Add(1)
		go func() { defer wg.Done(); worker.runHistoryPersistence(ctx) }()
		wg.Add(1)
		go func(index int, worker *Daemon) {
			defer wg.Done()
			defer func() {
				m.markProgress(worker, "stopped", time.Now())
				worker.shutdownRequested.Store(true)
				worker.operationMu.Lock()
				defer worker.operationMu.Unlock()
				worker.autoBlocked = true
				if worker.managed() {
					_ = worker.proc.Stop(10 * time.Second)
				}
				_ = worker.persistBudget()
			}()
			timer := time.NewTimer(time.Duration(index) * worker.opts.CheckInterval / time.Duration(len(ids)))
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
			}
			if ctx.Err() != nil {
				return
			}
			if worker.managed() {
				if err := worker.initialStart(); err != nil {
					worker.opts.Logf("network initial start failed: %v", err)
				}
			}
			m.check(ctx, worker)
			ticker := time.NewTicker(worker.opts.CheckInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					m.check(ctx, worker)
				}
			}
		}(index, worker)
	}
	wg.Wait()
	for _, worker := range m.workers {
		worker.waitRecoveries()
	}
	<-guardsDone
	if ctx.Err() != nil {
		return nil
	}
	return fmt.Errorf("network loops ended unexpectedly")
}

// Package health turns observations about the core, its network instances
// and their connectivity into durable health states and bounded recovery
// decisions.
//
// The engine is deliberately free of side effects: it decides whether a
// component is healthy, degraded, failed or unknown, and when a bounded
// recovery action should run. The daemon owns the actions themselves (CLI
// queries, restarting a process) and reports their outcome back, so this
// package stays testable with a fake clock.
//
// Vocabulary (report 7.6):
//
//   - Desired: what the user wants (run or stop). A user's stop is never
//     overridden by the engine.
//   - Lifecycle: starting/running/stopping/stopped, owned by the daemon.
//   - Level: the health verdict: unknown, healthy, degraded, failed.
//   - Recovery: idle, scheduled, running, exhausted.
//
// Only failures attributed to this machine (process exited, core
// unresponsive, missing resources) can trigger recovery. External causes
// (network outage, unreachable remote, relay-only routing) degrade the
// verdict but never restart a working core; collector problems (missing
// CLI, parse errors) degrade to unknown instead of blaming the core.
package health

import (
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"
)

// Level is the health verdict of one component.
type Level uint8

const (
	// LevelUnknown holds until evidence arrives, while a component is
	// manually stopped, or while the collector cannot observe it.
	LevelUnknown Level = iota
	// LevelHealthy is the expected, confirmed-good state.
	LevelHealthy
	// LevelDegraded is working but not in the target condition: no peers,
	// external outage, awaiting an address, or accumulating evidence.
	LevelDegraded
	// LevelFailed is a confirmed local fault that may warrant recovery.
	LevelFailed
)

func (l Level) String() string {
	switch l {
	case LevelHealthy:
		return "healthy"
	case LevelDegraded:
		return "degraded"
	case LevelFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// Cause classifies who is at fault for a non-healthy observation. It
// decides whether recovery may run.
type Cause uint8

const (
	// CauseNone accompanies healthy observations.
	CauseNone Cause = iota
	// CauseLocalFault is attributable to this machine: the process exited,
	// the listener is gone, a required device or route is missing.
	CauseLocalFault
	// CauseUnresponsive is a hung or timing-out core: the process exists
	// but management queries do not answer.
	CauseUnresponsive
	// CauseExternal is outside this machine: no network, all peers offline,
	// a configured probe target unreachable. Never triggers recovery.
	CauseExternal
	// CauseCollector is an application-side failure: binary missing, output
	// not parseable, version mismatch. Blames the adapter, not the core.
	CauseCollector
	// CauseAwaiting is a normal transient: starting up, waiting for the
	// peer to assign an address, reconnecting after a wake.
	CauseAwaiting
)

// attributable reports whether a cause may count toward a local fault.
func (c Cause) attributable() bool {
	return c == CauseLocalFault || c == CauseUnresponsive
}

func (c Cause) String() string {
	switch c {
	case CauseLocalFault:
		return "local-fault"
	case CauseUnresponsive:
		return "unresponsive"
	case CauseExternal:
		return "external"
	case CauseCollector:
		return "collector"
	case CauseAwaiting:
		return "awaiting"
	default:
		return "none"
	}
}

// Kind identifies the sort of component a state describes.
type Kind uint8

const (
	// KindService is the OS-level service registration (SCM, systemd,
	// launchd).
	KindService Kind = iota
	// KindCore is the EasyTier core process and its management portal.
	KindCore
	// KindInstance is one network instance inside a core.
	KindInstance
	// KindProbe is a user-configured reachability target.
	KindProbe
)

func (k Kind) String() string {
	switch k {
	case KindService:
		return "service"
	case KindCore:
		return "core"
	case KindInstance:
		return "instance"
	case KindProbe:
		return "probe"
	default:
		return "unknown"
	}
}

// Component identifies one monitored thing.
type Component struct {
	Kind Kind   `json:"kind"`
	ID   string `json:"id"`
}

func (c Component) String() string { return c.Kind.String() + "/" + c.ID }

// Desired is the user's intent for a component.
type Desired uint8

const (
	DesiredRun Desired = iota
	DesiredStop
)

func (d Desired) String() string {
	if d == DesiredStop {
		return "stop"
	}
	return "run"
}

// Lifecycle is the daemon-owned process state.
type Lifecycle uint8

const (
	LifecycleUnknown Lifecycle = iota
	LifecycleStarting
	LifecycleRunning
	LifecycleStopping
	LifecycleStopped
)

func (l Lifecycle) String() string {
	switch l {
	case LifecycleStarting:
		return "starting"
	case LifecycleRunning:
		return "running"
	case LifecycleStopping:
		return "stopping"
	case LifecycleStopped:
		return "stopped"
	default:
		return "unknown"
	}
}

// Observation is one check round's result for one component. Detail must
// already be scrubbed of secrets; it is stored in events.
type Observation struct {
	Component Component
	// Lifecycle is the process state the daemon observed, when it knows
	// it. It does not depend on the health level.
	Lifecycle Lifecycle
	// Level is the raw verdict of this single observation.
	Level Level
	// Cause classifies a non-healthy observation.
	Cause Cause
	// Reason is a short machine-readable explanation (stable identifiers
	// over sentences where possible), e.g. "rpc-unreachable".
	Reason string
	// Detail is optional human context, already scrubbed.
	Detail string
}

// RecoveryState is the recovery pipeline state of a component.
type RecoveryState uint8

const (
	RecoveryIdle RecoveryState = iota
	RecoveryScheduled
	RecoveryRunning
	RecoveryExhausted
)

func (r RecoveryState) String() string {
	switch r {
	case RecoveryScheduled:
		return "scheduled"
	case RecoveryRunning:
		return "running"
	case RecoveryExhausted:
		return "exhausted"
	default:
		return "idle"
	}
}

// State is a component's health picture for the UI and the API.
type State struct {
	Component Component `json:"component"`

	Desired   Desired   `json:"desired"`
	Lifecycle Lifecycle `json:"lifecycle"`
	Level     Level     `json:"level"`
	Cause     Cause     `json:"cause"`
	Reason    string    `json:"reason"`
	Detail    string    `json:"detail,omitempty"`

	// Since is when Level last changed. LastCheck is the last completed
	// observation; LastOK the last healthy one.
	Since     time.Time `json:"since"`
	LastCheck time.Time `json:"last_check"`
	LastOK    time.Time `json:"last_ok"`

	ConsecutiveOKs      int `json:"consecutive_oks"`
	ConsecutiveFailures int `json:"consecutive_failures"`

	Recovery         RecoveryState `json:"recovery"`
	NextRetry        time.Time     `json:"next_retry"`
	AttemptsInWindow int           `json:"attempts_in_window"`
	// RecoveryExhaustedAt is when the budget last ran out, so the UI can
	// explain why nothing is being retried automatically.
	RecoveryExhaustedAt time.Time `json:"recovery_exhausted_at"`
}

// StaleAfter reports whether the state's evidence is older than d at now,
// which callers render as "unknown: monitor unavailable" rather than a
// confidently green value.
func (s State) StaleAfter(now time.Time, d time.Duration) bool {
	return s.LastCheck.IsZero() || now.Sub(s.LastCheck) > d
}

// ActionKind is what the daemon should do about a failed component.
type ActionKind uint8

const (
	// ActionNone carries no work.
	ActionNone ActionKind = iota
	// ActionRestartInstance restarts one network instance.
	ActionRestartInstance
	// ActionRestartCore restarts the core process (all its instances).
	ActionRestartCore
)

func (a ActionKind) String() string {
	switch a {
	case ActionRestartInstance:
		return "restart-instance"
	case ActionRestartCore:
		return "restart-core"
	default:
		return "none"
	}
}

// Action is one recovery step the engine decided on. After is the delay
// from now at which it becomes due; the daemon schedules it and reports
// progress with BeginRecovery and EndRecovery.
type Action struct {
	Kind      ActionKind
	Component Component
	Reason    string
	After     time.Duration
}

// EventKind enumerates durable health events.
type EventKind uint8

const (
	// EventLevelChanged reports a component's verdict change.
	EventLevelChanged EventKind = iota
	// EventRecoveryScheduled reports a scheduled recovery action.
	EventRecoveryScheduled
	// EventRecoveryStarted reports an action that began running.
	EventRecoveryStarted
	// EventRecoveryFinished reports an action's outcome.
	EventRecoveryFinished
	// EventRecoveryExhausted reports the end of the attempt budget.
	EventRecoveryExhausted
	// EventConfigApplied reports that a different network config was
	// activated (the core restart itself is reported by level changes).
	EventConfigApplied
	EventConfigStarted
	EventConfigFailed
	EventConfigRollback
)

func (k EventKind) String() string {
	switch k {
	case EventLevelChanged:
		return "level-changed"
	case EventRecoveryScheduled:
		return "recovery-scheduled"
	case EventRecoveryStarted:
		return "recovery-started"
	case EventRecoveryFinished:
		return "recovery-finished"
	case EventRecoveryExhausted:
		return "recovery-exhausted"
	case EventConfigApplied:
		return "config-applied"
	case EventConfigStarted:
		return "config-started"
	case EventConfigFailed:
		return "config-failed"
	case EventConfigRollback:
		return "config-rollback"
	default:
		return "unknown"
	}
}

// Event is one entry of the durable health timeline.
type Event struct {
	OperationID      string    `json:"operation_id,omitempty"`
	Revision         string    `json:"revision,omitempty"`
	PreviousRevision string    `json:"previous_revision,omitempty"`
	Generation       uint64    `json:"generation,omitempty"`
	Time             time.Time `json:"time"`
	Component        Component `json:"component"`
	Kind             EventKind `json:"kind"`
	From             Level     `json:"from"`
	To               Level     `json:"to"`
	Cause            Cause     `json:"cause"`
	Reason           string    `json:"reason"`
	Detail           string    `json:"detail,omitempty"`
	Attempt          int       `json:"attempt"`
}

// Policy is the recovery policy. Zero fields take DefaultPolicy values;
// tests set Jitter to zero for deterministic timing.
type Policy struct {
	// FailThreshold is how many consecutive attributable failures confirm
	// a fault.
	FailThreshold int
	// OKThreshold is how many consecutive healthy observations clear one.
	OKThreshold int
	// GracePeriod suppresses recovery right after deliberate lifecycle
	// changes (start, config apply, wake from sleep).
	GracePeriod time.Duration
	// BackoffBase is the first retry delay; it doubles per attempt.
	BackoffBase time.Duration
	// BackoffMax caps the retry delay.
	BackoffMax time.Duration
	// Jitter is the ± fraction of random jitter on delays (0.2 = ±20%).
	Jitter float64
	// MaxAttempts bounds recovery attempts per component per Window.
	MaxAttempts int
	// Window is the sliding window the attempt budget counts over.
	Window time.Duration
	// RecoveryEnabled allows automatic recovery at all ("monitor only"
	// mode sets it false).
	RecoveryEnabled bool
}

// DefaultPolicy returns the reviewable starting policy. The numbers are
// product decisions pending field measurement (report 7.6.3); they are
// not upstream defaults.
func DefaultPolicy() Policy {
	return Policy{
		FailThreshold:   3,
		OKThreshold:     2,
		GracePeriod:     45 * time.Second,
		BackoffBase:     5 * time.Second,
		BackoffMax:      60 * time.Second,
		Jitter:          0.2,
		MaxAttempts:     3,
		Window:          10 * time.Minute,
		RecoveryEnabled: true,
	}
}

func (p Policy) withDefaults() Policy {
	d := DefaultPolicy()
	if p.FailThreshold <= 0 {
		p.FailThreshold = d.FailThreshold
	}
	if p.OKThreshold <= 0 {
		p.OKThreshold = d.OKThreshold
	}
	if p.GracePeriod < 0 {
		p.GracePeriod = d.GracePeriod
	}
	if p.BackoffBase <= 0 {
		p.BackoffBase = d.BackoffBase
	}
	if p.BackoffMax <= 0 {
		p.BackoffMax = d.BackoffMax
	}
	if p.Jitter < 0 {
		p.Jitter = d.Jitter
	}
	if p.MaxAttempts <= 0 {
		p.MaxAttempts = d.MaxAttempts
	}
	if p.Window <= 0 {
		p.Window = d.Window
	}
	return p
}

type componentState struct {
	State
	graceUntil        time.Time
	recoveryAttempts  []time.Time
	exhaustedNotified bool
}

// Engine evaluates observations against a policy. It is safe for
// concurrent use.
type Engine struct {
	policy Policy
	now    func() time.Time
	jitter func() float64

	mu     sync.Mutex
	states map[Component]*componentState
	sink   func(Event)
}

// Option configures an Engine (tests use the clock one).
type Option func(*Engine)

// WithClock replaces the time source.
func WithClock(now func() time.Time) Option {
	return func(e *Engine) { e.now = now }
}

// WithJitter replaces the jitter source with a value in [-1, 1).
func WithJitter(j func() float64) Option {
	return func(e *Engine) { e.jitter = j }
}

// New creates an engine with the policy (zero fields take defaults).
func New(policy Policy, opts ...Option) *Engine {
	e := &Engine{
		policy: policy.withDefaults(),
		now:    time.Now,
		jitter: rand.Float64,
		states: make(map[Component]*componentState),
	}
	for _, o := range opts {
		o(e)
	}
	return e
}

// Policy returns the effective policy.
func (e *Engine) Policy() Policy { return e.policy }

// SetEventSink installs the durable event callback. Events are delivered
// synchronously while holding the engine lock; sinks must not call back
// into the engine. A nil sink discards events.
func (e *Engine) SetEventSink(sink func(Event)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.sink = sink
}

func (e *Engine) emit(ev Event) {
	if e.sink != nil {
		e.sink(ev)
	}
}

func (e *Engine) component(c Component) *componentState {
	st, ok := e.states[c]
	if !ok {
		st = &componentState{}
		st.Component = c
		st.Desired = DesiredRun
		st.Level = LevelUnknown
		st.Since = e.now()
		e.states[c] = st
	}
	return st
}

// SetDesired records the user's intent. Stopping clears any recovery; the
// engine never restarts what the user stopped.
func (e *Engine) SetDesired(c Component, d Desired) {
	e.mu.Lock()
	defer e.mu.Unlock()
	st := e.component(c)
	if st.Desired == d {
		return
	}
	st.Desired = d
	if d == DesiredStop {
		st.Recovery = RecoveryIdle
		st.NextRetry = time.Time{}
	}
}

// Grace postpones recovery for a component after a deliberate lifecycle
// change: a start, a configuration apply, an upgrade, a wake from sleep.
// Observations during the grace period still update the verdict, but no
// recovery is scheduled until it ends.
func (e *Engine) Grace(c Component) {
	e.mu.Lock()
	defer e.mu.Unlock()
	st := e.component(c)
	st.graceUntil = e.now().Add(e.policy.GracePeriod)
}

// GrantRetry clears the attempt budget for a component after a manual
// retry or a configuration change, re-authorizing automatic recovery.
// GrantRetry clears the attempt budget for a component after a manual
// decision. The caller runs them and then calls BeginRecovery/EndRecovery.
func (e *Engine) GrantRetry(c Component) {
	e.mu.Lock()
	defer e.mu.Unlock()
	st := e.component(c)
	st.recoveryAttempts = nil
	st.RecoveryExhaustedAt = time.Time{}
	st.exhaustedNotified = false
	if st.Recovery == RecoveryExhausted {
		st.Recovery = RecoveryIdle
	}
	st.NextRetry = time.Time{}
}

// AttemptSnapshot is one component's recovery attempts, for persistence
// across a daemon restart. Without it the OS could bypass the bounded
// recovery promise: every restart would hand out a fresh budget.
type AttemptSnapshot struct {
	Component Component   `json:"component"`
	Times     []time.Time `json:"times"`
}

// Attempts returns the current recovery attempts per component, oldest
// first, already pruned to the policy window.
func (e *Engine) Attempts() []AttemptSnapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := e.now()
	out := make([]AttemptSnapshot, 0, len(e.states))
	for c, st := range e.states {
		e.pruneAttempts(st, now)
		if len(st.recoveryAttempts) == 0 {
			continue
		}
		out = append(out, AttemptSnapshot{
			Component: c,
			Times:     append([]time.Time(nil), st.recoveryAttempts...),
		})
	}
	return out
}

// SeedAttempts restores persisted recovery attempts for a component,
// pruning them to the window as of now. It reports how many were kept. A
// restored component that already used its budget starts out exhausted,
// so a restart cannot silently re-authorize automatic recovery.
func (e *Engine) SeedAttempts(c Component, times []time.Time) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	st := e.component(c)
	now := e.now()
	for _, t := range times {
		if !t.IsZero() {
			st.recoveryAttempts = append(st.recoveryAttempts, t)
		}
	}
	e.pruneAttempts(st, now)
	if len(st.recoveryAttempts) == 0 {
		return 0
	}
	if st.recoveryAttempts[0].After(now) {
		// Times in the future mean a clock problem: drop them rather than
		// trusting a budget that may never expire.
		st.recoveryAttempts = nil
		return 0
	}
	if len(st.recoveryAttempts) >= e.policy.MaxAttempts {
		st.Recovery = RecoveryExhausted
		st.RecoveryExhaustedAt = now
	}
	return len(st.recoveryAttempts)
}

// Observe records one check round and returns due recovery actions. The
// caller runs them and then calls BeginRecovery/EndRecovery.
func (e *Engine) Observe(obs Observation) []Action {
	e.mu.Lock()
	defer e.mu.Unlock()

	now := e.now()
	st := e.component(obs.Component)
	prevLevel := st.Level

	if obs.Lifecycle != LifecycleUnknown {
		st.Lifecycle = obs.Lifecycle
	}
	st.LastCheck = now
	st.Cause = obs.Cause
	st.Reason = obs.Reason
	st.Detail = obs.Detail

	switch {
	case obs.Level == LevelHealthy && obs.Cause == CauseNone:
		st.ConsecutiveOKs++
		st.ConsecutiveFailures = 0
		st.LastOK = now
	case obs.Cause.attributable():
		st.ConsecutiveFailures++
		st.ConsecutiveOKs = 0
	default:
		// External, collector and awaiting observations are not local
		// fault evidence; they do not move the failure counters.
		st.ConsecutiveOKs = 0
	}

	next := e.nextLevel(st, obs)
	e.setLevel(st, next, obs, now)

	var actions []Action
	if st.Level == LevelFailed && st.Desired == DesiredRun && e.policy.RecoveryEnabled {
		actions = e.planRecovery(st, obs, now)
	}
	if st.Level == LevelHealthy {
		st.Recovery = RecoveryIdle
		st.NextRetry = time.Time{}
		// Attempts are deliberately kept: the budget bounds a component
		// per window, not per failure episode. Clearing it here would let
		// a crash → recover → crash loop restart forever, and would also
		// make the persisted budget meaningless across a daemon restart.
		// The window (Policy.Window) expires old attempts instead, and a
		// manual start or an explicit renewal is what re-authorizes.
		st.RecoveryExhaustedAt = time.Time{}
		st.exhaustedNotified = false
	}
	if prevLevel != st.Level && st.Level == LevelHealthy {
		st.exhaustedNotified = false
	}
	return actions
}

// nextLevel computes the settled verdict for the current observation,
// applying thresholds, grace and the user's intent.
func (e *Engine) nextLevel(st *componentState, obs Observation) Level {
	if st.Desired == DesiredStop {
		return LevelUnknown
	}
	// A healthy observation only clears the verdict after OKThreshold
	// consecutive ones, matching how failures confirm.
	if obs.Level == LevelHealthy && obs.Cause == CauseNone {
		if st.ConsecutiveOKs >= e.policy.OKThreshold || st.Level == LevelUnknown {
			return LevelHealthy
		}
		return LevelDegraded
	}
	switch obs.Cause {
	case CauseCollector:
		// The collector is broken; the core may be fine. Say unknown.
		return LevelUnknown
	case CauseExternal:
		return LevelDegraded
	case CauseAwaiting:
		return LevelDegraded
	}
	if obs.Cause.attributable() {
		if st.ConsecutiveFailures >= e.policy.FailThreshold {
			return LevelFailed
		}
		return LevelDegraded
	}
	// Degraded verdicts without a cause keep the level moving downward;
	// no cause health observations come as LevelHealthy.
	return LevelDegraded
}

func (e *Engine) setLevel(st *componentState, level Level, obs Observation, now time.Time) {
	if st.Level == level {
		return
	}
	from := st.Level
	st.Level = level
	st.Since = now
	e.emit(Event{
		Time:      now,
		Component: st.Component,
		Kind:      EventLevelChanged,
		From:      from,
		To:        level,
		Cause:     obs.Cause,
		Reason:    obs.Reason,
		Detail:    obs.Detail,
	})
}

// planRecovery decides the bounded recovery step for a failed component.
func (e *Engine) planRecovery(st *componentState, obs Observation, now time.Time) []Action {
	if now.Before(st.graceUntil) {
		// Within the grace window the failure is recorded but not acted on.
		return nil
	}

	e.pruneAttempts(st, now)
	if len(st.recoveryAttempts) >= e.policy.MaxAttempts {
		if st.Recovery != RecoveryExhausted || !st.exhaustedNotified {
			st.Recovery = RecoveryExhausted
			st.NextRetry = time.Time{}
			st.RecoveryExhaustedAt = now
			st.exhaustedNotified = true
			e.emit(Event{
				Time: now, Component: st.Component, Kind: EventRecoveryExhausted,
				Cause: obs.Cause, Reason: obs.Reason, Detail: obs.Detail,
				Attempt: len(st.recoveryAttempts),
			})
		}
		return nil
	}

	attempt := len(st.recoveryAttempts)
	delay := e.backoff(attempt)
	kind := ActionRestartCore
	if st.Component.Kind == KindInstance {
		kind = ActionRestartInstance
	}
	if st.Recovery != RecoveryScheduled || st.NextRetry.IsZero() {
		st.Recovery = RecoveryScheduled
		st.NextRetry = now.Add(delay)
		e.emit(Event{
			Time: now, Component: st.Component, Kind: EventRecoveryScheduled,
			Cause: obs.Cause, Reason: obs.Reason, Detail: obs.Detail,
			Attempt: attempt + 1,
		})
		return []Action{{Kind: kind, Component: st.Component, Reason: obs.Reason, After: delay}}
	}
	// Already scheduled: report the outstanding delay when it is due, so
	// the daemon can (re)arm its timer after a restart.
	after := st.NextRetry.Sub(now)
	if after <= 0 {
		after = 0
	}
	return []Action{{Kind: kind, Component: st.Component, Reason: obs.Reason, After: after}}
}

func (e *Engine) pruneAttempts(st *componentState, now time.Time) {
	cutoff := now.Add(-e.policy.Window)
	kept := st.recoveryAttempts[:0]
	for _, t := range st.recoveryAttempts {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	st.recoveryAttempts = kept
}

func (e *Engine) backoff(attempt int) time.Duration {
	d := e.policy.BackoffBase
	for i := 0; i < attempt; i++ {
		d *= 2
		if d >= e.policy.BackoffMax {
			d = e.policy.BackoffMax
			break
		}
	}
	if e.policy.Jitter > 0 {
		d = time.Duration(float64(d) * (1 + e.policy.Jitter*(2*e.jitter()-1)))
	}
	return d
}

// BeginRecovery marks a scheduled action as running and counts the
// attempt, which consumes the recovery budget. ok is false when the
// component has no state or is no longer allowed to recover (stopped by
// the user, budget exhausted).
func (e *Engine) BeginRecovery(c Component) (attempt int, ok bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	st, found := e.states[c]
	if !found || st.Desired == DesiredStop {
		return 0, false
	}
	now := e.now()
	e.pruneAttempts(st, now)
	if len(st.recoveryAttempts) >= e.policy.MaxAttempts {
		st.Recovery = RecoveryExhausted
		return 0, false
	}
	st.recoveryAttempts = append(st.recoveryAttempts, now)
	st.Recovery = RecoveryRunning
	st.NextRetry = time.Time{}
	attempt = len(st.recoveryAttempts)
	e.emit(Event{
		Time: now, Component: c, Kind: EventRecoveryStarted,
		Cause: st.Cause, Reason: st.Reason, Detail: st.Detail, Attempt: attempt,
	})
	return attempt, true
}

// EndRecovery reports the outcome of a running action. Subsequent
// observations judge whether it actually helped.
func (e *Engine) EndRecovery(c Component, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	st, found := e.states[c]
	if !found {
		return
	}
	if st.Recovery == RecoveryRunning {
		st.Recovery = RecoveryIdle
	}
	detail := ""
	if err != nil {
		detail = err.Error()
	}
	e.emit(Event{
		Time: e.now(), Component: c, Kind: EventRecoveryFinished,
		Cause: st.Cause, Reason: st.Reason, Detail: detail,
		Attempt: len(st.recoveryAttempts),
	})
}

// Snapshot returns a sorted copy of all component states.
func (e *Engine) Snapshot() []State {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]State, 0, len(e.states))
	for _, st := range e.states {
		s := st.State
		s.AttemptsInWindow = len(st.recoveryAttempts)
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Component.Kind != out[j].Component.Kind {
			return out[i].Component.Kind < out[j].Component.Kind
		}
		return out[i].Component.ID < out[j].Component.ID
	})
	return out
}

// State returns one component's state copy.
func (e *Engine) State(c Component) (State, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	st, ok := e.states[c]
	if !ok {
		return State{}, false
	}
	s := st.State
	s.AttemptsInWindow = len(st.recoveryAttempts)
	return s, true
}

// Remove deletes a component (for example an instance that was removed by
// the user).
func (e *Engine) Remove(c Component) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.states, c)
}

// FormatDetail is a helper for building scrubbed one-line details from
// several fields; it never includes untrusted raw config text.
func FormatDetail(parts ...string) string {
	kept := parts[:0]
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, "; ")
}

// String renders a state as one line for logs.
func (s State) String() string {
	return fmt.Sprintf("%s desired=%s lifecycle=%s level=%s cause=%s reason=%q recovery=%s attempts=%d",
		s.Component, s.Desired, s.Lifecycle, s.Level, s.Cause, s.Reason, s.Recovery, s.AttemptsInWindow)
}

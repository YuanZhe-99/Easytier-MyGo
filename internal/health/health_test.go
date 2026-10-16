package health

import (
	"testing"
	"time"
)

// testClock is a settable clock for the engine.
type testClock struct{ t time.Time }

func (c *testClock) now() time.Time          { return c.t }
func (c *testClock) advance(d time.Duration) { c.t = c.t.Add(d) }

var core = Component{Kind: KindCore, ID: "core"}

func newEngine(clk *testClock) *Engine {
	p := DefaultPolicy()
	p.Jitter = 0
	p.GracePeriod = 0
	p.RecoveryEnabled = true
	return New(p, WithClock(clk.now), WithJitter(func() float64 { return 0 }))
}

func observe(e *Engine, level Level, cause Cause) []Action {
	return e.Observe(Observation{Component: core, Lifecycle: LifecycleRunning, Level: level, Cause: cause, Reason: "test"})
}

// TestOnlyLocalCausesRecover is the product contract: external, collector
// and transient causes degrade the verdict but never restart anything.
func TestOnlyLocalCausesRecover(t *testing.T) {
	cases := []struct {
		cause      Cause
		level      Level
		wantAction bool
	}{
		{CauseLocalFault, LevelFailed, true},
		{CauseUnresponsive, LevelFailed, true},
		{CauseExternal, LevelDegraded, false},
		{CauseCollector, LevelUnknown, false},
		{CauseAwaiting, LevelDegraded, false},
	}
	for _, tc := range cases {
		t.Run(tc.cause.String(), func(t *testing.T) {
			clk := &testClock{t: time.Unix(1_000_000, 0)}
			e := newEngine(clk)
			e.SetDesired(core, DesiredRun)
			var actions []Action
			for i := 0; i < 10; i++ {
				actions = append(actions, observe(e, tc.level, tc.cause)...)
				clk.advance(5 * time.Second)
			}
			if got := len(actions) > 0; got != tc.wantAction {
				t.Fatalf("cause %v: recovery scheduled = %v, want %v", tc.cause, got, tc.wantAction)
			}
		})
	}
}

// TestFailureNeedsConfirmation: one bad round is not a fault.
func TestFailureNeedsConfirmation(t *testing.T) {
	clk := &testClock{t: time.Unix(1_000_000, 0)}
	e := newEngine(clk)
	e.SetDesired(core, DesiredRun)
	n := e.Policy().FailThreshold
	for i := 1; i < n; i++ {
		if a := observe(e, LevelFailed, CauseLocalFault); len(a) > 0 {
			t.Fatalf("recovery after %d of %d failures", i, n)
		}
	}
	if a := observe(e, LevelFailed, CauseLocalFault); len(a) == 0 {
		t.Fatalf("no recovery after %d failures", n)
	}
	if s, _ := e.State(core); s.Level != LevelFailed {
		t.Fatalf("level %v, want failed", s.Level)
	}
}

// TestManualStopIsNeverOverridden: a user's stop clears recovery and no
// later observation brings it back.
func TestManualStopIsNeverOverridden(t *testing.T) {
	clk := &testClock{t: time.Unix(1_000_000, 0)}
	e := newEngine(clk)
	e.SetDesired(core, DesiredStop)
	for i := 0; i < 10; i++ {
		if a := observe(e, LevelFailed, CauseLocalFault); len(a) > 0 {
			t.Fatal("recovery scheduled for a stopped component")
		}
		clk.advance(5 * time.Second)
	}
	if _, ok := e.BeginRecovery(core); ok {
		t.Fatal("BeginRecovery allowed on a stopped component")
	}
}

// TestRecoveryIsBounded: attempts back off, stop at the budget, and the
// budget refills only after the window or a manual grant.
func TestRecoveryIsBounded(t *testing.T) {
	clk := &testClock{t: time.Unix(1_000_000, 0)}
	e := newEngine(clk)
	e.SetDesired(core, DesiredRun)
	p := e.Policy()

	var delays []time.Duration
	for len(delays) < p.MaxAttempts+2 {
		actions := observe(e, LevelFailed, CauseLocalFault)
		if len(actions) == 0 {
			clk.advance(time.Second)
			if s, _ := e.State(core); s.Recovery == RecoveryExhausted {
				break
			}
			continue
		}
		delays = append(delays, actions[0].After)
		clk.advance(actions[0].After)
		if _, ok := e.BeginRecovery(core); !ok {
			break
		}
		e.EndRecovery(core, nil)
	}
	if len(delays) != p.MaxAttempts {
		t.Fatalf("%d recovery attempts, want %d", len(delays), p.MaxAttempts)
	}
	for i := 1; i < len(delays); i++ {
		if delays[i] < delays[i-1] || delays[i] > p.BackoffMax {
			t.Fatalf("delays %v: not a bounded backoff", delays)
		}
	}
	if s, _ := e.State(core); s.Recovery != RecoveryExhausted {
		t.Fatalf("recovery %v after the budget, want exhausted", s.Recovery)
	}

	e.GrantRetry(core)
	got := false
	for i := 0; i < p.FailThreshold+1 && !got; i++ {
		got = len(observe(e, LevelFailed, CauseLocalFault)) > 0
	}
	if !got {
		t.Fatal("no recovery after GrantRetry")
	}
}

// TestRestartCannotResetBudget: persisted attempts restore an exhausted
// budget, so a daemon restart does not hand out new attempts.
func TestRestartCannotResetBudget(t *testing.T) {
	clk := &testClock{t: time.Unix(1_000_000, 0)}
	e := newEngine(clk)
	p := e.Policy()
	times := make([]time.Time, p.MaxAttempts)
	for i := range times {
		times[i] = clk.t.Add(-time.Duration(i+1) * time.Second)
	}
	e.SetDesired(core, DesiredRun)
	if kept := e.SeedAttempts(core, times); kept != p.MaxAttempts {
		t.Fatalf("kept %d attempts, want %d", kept, p.MaxAttempts)
	}
	for i := 0; i < 10; i++ {
		if a := observe(e, LevelFailed, CauseLocalFault); len(a) > 0 {
			t.Fatal("recovery after restoring an exhausted budget")
		}
	}
}

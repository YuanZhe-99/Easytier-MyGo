package daemon

// Recovery budget persistence: the attempts the health engine spent are
// written to the state directory and restored when the daemon starts
// again. Without this the OS could hand out a fresh budget on every
// restart (a watchdog restart, an upgrade, a crash), which would quietly
// break the bounded-recovery promise: "attempts per window" must count
// across daemon lifetimes, not per process.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/health"
)

// budgetFile is the file name inside the state directory.
const budgetFile = "recovery-budget.json"

// budgetState is the on-disk shape.
type budgetState struct {
	// SavedAt is when the file was written (diagnostics only).
	SavedAt time.Time `json:"saved_at"`
	// Components are the per-component attempts, oldest first.
	Components []health.AttemptSnapshot `json:"components"`
}

func decodeBudget(data []byte) (budgetState, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return budgetState{}, err
	}
	if _, ok := fields["components"]; !ok {
		return budgetState{}, errors.New("incomplete recovery budget")
	}
	var state budgetState
	if err := json.Unmarshal(data, &state); err != nil {
		return state, err
	}
	if state.SavedAt.IsZero() {
		return state, errors.New("missing recovery budget timestamp")
	}
	seen := make(map[health.Component]bool)
	for _, component := range state.Components {
		if component.Component.ID == "" || seen[component.Component] {
			return state, errors.New("invalid or duplicate budget component")
		}
		seen[component.Component] = true
		for _, at := range component.Times {
			if at.IsZero() || at.After(time.Now().Add(time.Minute)) {
				return state, errors.New("invalid recovery attempt timestamp")
			}
		}
	}
	return state, nil
}

func budgetPath(stateDir string) string {
	return filepath.Join(stateDir, budgetFile)
}

// loadBudget reads persisted attempts. A missing file is not an error (a
// fresh install). restoreBudget inhibits automatic operation on damaged data.
func loadBudget(stateDir string, logf func(string, ...any)) []health.AttemptSnapshot {
	data, err := os.ReadFile(budgetPath(stateDir))
	if err != nil {
		if !os.IsNotExist(err) {
			logf("daemon: reading recovery budget: %v", err)
		}
		return nil
	}
	st, err := decodeBudget(data)
	if err != nil {
		logf("daemon: recovery budget file is damaged: %v", err)
		return nil
	}
	return st.Components
}

// saveBudget writes the attempts atomically (temp file + rename), so a
// crash mid-write never leaves a partial budget.
func saveBudget(stateDir string, attempts []health.AttemptSnapshot) error {
	st := budgetState{SavedAt: time.Now(), Components: attempts}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return atomicState(budgetPath(stateDir), data)
}

// persistBudget snapshots the engine's attempts and stores them. It is
// called whenever the budget changes and when the daemon stops.
func (d *Daemon) persistBudget() error {
	if d.engine == nil {
		return nil
	}
	if err := saveBudget(d.opts.StateDir, d.engine.Attempts()); err != nil {
		d.autoBlocked = true
		d.opts.Logf("daemon: %v", err)
		return err
	}
	return nil
}

// restoreBudget seeds the engine with the persisted attempts, so a
// restart does not re-authorize recovery that was already spent.
func (d *Daemon) restoreBudget() {
	// A damaged budget is not authorization for a fresh recovery window.
	data, err := os.ReadFile(budgetPath(d.opts.StateDir))
	if os.IsNotExist(err) {
		return
	}
	state, decodeErr := decodeBudget(data)
	if err != nil || decodeErr != nil {
		d.autoBlocked = true
		d.opts.Logf("daemon: automatic recovery inhibited by unreadable budget")
		return
	}
	for _, a := range state.Components {
		if n := d.engine.SeedAttempts(a.Component, a.Times); n > 0 {
			d.opts.Logf("daemon: restored %d recovery attempt(s) for %s", n, a.Component)
		}
	}
}

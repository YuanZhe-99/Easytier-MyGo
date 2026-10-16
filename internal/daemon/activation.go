package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/configdoc"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/coreadapter"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/health"
)

var errCandidateUnconfirmed = errors.New("collector cannot confirm candidate; automatic rollback withheld")

// appliedConfig separates mutable drafts from exact bytes used for recovery.
type appliedConfig struct {
	Version  int    `json:"version"`
	Source   string `json:"source"`
	Revision string `json:"revision"`
	Runtime  string `json:"runtime"`
}

type activationRecord struct {
	Trace          activationTrace `json:"trace,omitempty"`
	Previous       appliedConfig   `json:"previous"`
	Candidate      appliedConfig   `json:"candidate"`
	Phase          string          `json:"phase"`
	PreviousIntent *runIntent      `json:"previous_intent,omitempty"`
}

type activationTrace struct {
	OperationID      string `json:"operation_id,omitempty"`
	Revision         string `json:"revision,omitempty"`
	PreviousRevision string `json:"previous_revision,omitempty"`
	Generation       uint64 `json:"generation,omitempty"`
}

func (d *Daemon) activationEvent(event health.Event) {
	event.OperationID = d.activationTrace.OperationID
	event.Revision = d.activationTrace.Revision
	event.PreviousRevision = d.activationTrace.PreviousRevision
	event.Generation = d.activationTrace.Generation
	d.events.appendEvent(event)
}

func (d *Daemon) previousActivationIntent() (*runIntent, error) {
	b, err := os.ReadFile(filepath.Join(d.opts.StateDir, "activation.json"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var record activationRecord
	if err := json.Unmarshal(b, &record); err != nil {
		return nil, err
	}
	if record.PreviousIntent != nil && (record.PreviousIntent.Version != 1 ||
		(record.PreviousIntent.Desired != health.DesiredRun && record.PreviousIntent.Desired != health.DesiredStop)) {
		return nil, errors.New("invalid activation intent")
	}
	return record.PreviousIntent, nil
}

// blockRollback persists the failed outcome so OS restarts cannot retry it.
func (d *Daemon) blockRollback(previous, candidate appliedConfig, cause error) error {
	d.autoBlocked = true
	return errors.Join(cause, d.recordActivation(previous, candidate, "rollback-failed"))
}

func (d *Daemon) validateApplied(a appliedConfig) error {
	if a.Version != 1 || a.Revision == "" {
		return errors.New("invalid applied revision")
	}
	root, err := filepath.Abs(filepath.Join(d.opts.StateDir, "applied"))
	if err != nil {
		return err
	}
	path, err := filepath.Abs(a.Runtime)
	if err != nil {
		return err
	}
	if filepath.Dir(path) != root || filepath.Base(path) != a.Revision+".toml" || strings.ContainsAny(a.Revision, "/\\") {
		return errors.New("applied path escapes runtime directory")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("applied file is not regular")
	}
	text, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if configdoc.Revision(string(text)) != a.Revision {
		return errors.New("applied revision content mismatch")
	}
	return nil
}

// rollbackCandidate makes rollback durable before touching another process.
func (d *Daemon) rollbackCandidate(previous, candidate appliedConfig, cause error) error {
	d.activationEvent(health.Event{Time: time.Now(), Component: d.coreComponent(), Kind: health.EventConfigRollback, Reason: "config-rollback", Detail: previous.Revision})
	d.applied = previous
	priorIntent, err := d.previousActivationIntent()
	if err != nil {
		return d.blockRollback(previous, candidate, errors.Join(cause, err))
	}
	if err := d.recordActivation(previous, candidate, "rollback"); err != nil {
		d.autoBlocked = true
		return errors.Join(cause, err)
	}
	previousDesired := d.intent.Desired
	if priorIntent != nil {
		previousDesired = priorIntent.Desired
	}
	if d.stopRequested.Load() || previousDesired == health.DesiredStop {
		// Even when stop wins, restore the document selection before returning.
		// The caller's stop operation will persist DesiredStop under the same lock.
		if err := d.restorePreviousSelection(previous); err != nil {
			return d.blockRollback(previous, candidate, errors.Join(cause, err))
		}
		if err := d.proc.Stop(10 * time.Second); err != nil {
			return d.blockRollback(previous, candidate, errors.Join(cause, err))
		}
		d.activationState.Store("cancelled")
		return cause
	}
	if previous.Runtime == "" {
		return d.blockRollback(previous, candidate, errors.Join(cause, d.proc.Stop(10*time.Second)))
	}
	if err := d.restartCore(); err != nil {
		return d.blockRollback(previous, candidate, errors.Join(cause, err))
	}
	if err := d.confirmCandidate(context.Background()); err != nil {
		return d.blockRollback(previous, candidate, errors.Join(cause, err))
	}
	if err := d.restorePreviousSelection(previous); err != nil {
		return d.blockRollback(previous, candidate, errors.Join(cause, err))
	}
	d.activationState.Store("rolled-back")
	return cause
}

func (d *Daemon) restorePreviousSelection(previous appliedConfig) error {
	priorIntent, err := d.previousActivationIntent()
	if err != nil {
		return err
	}
	if err := d.saveApplied(previous); err != nil {
		return err
	}
	if err := persistActive(d.opts.StateDir, previous.Source); err != nil {
		return err
	}
	d.mu.Lock()
	d.activePath = previous.Source
	d.mu.Unlock()
	d.refreshCoreUserMode()
	d.appliedRevision.Store(previous.Revision)
	if priorIntent != nil {
		desired := priorIntent.Desired
		if d.stopRequested.Load() {
			desired = health.DesiredStop
		}
		if err := d.writeIntent(desired); err != nil {
			return err
		}
		d.engine.SetDesired(d.coreComponent(), desired)
		d.engine.SetDesired(d.instanceComponent(), desired)
	}
	if err := os.Remove(filepath.Join(d.opts.StateDir, "activation.json")); err != nil {
		return err
	}
	return nil
}

func (d *Daemon) recordActivation(previous, candidate appliedConfig, phase string) error {
	d.activationState.Store(phase)
	intent := d.intent
	priorIntent := &intent
	if phase != "starting" {
		stored, err := d.previousActivationIntent()
		if err != nil {
			d.autoBlocked = true
			return err
		}
		if stored != nil {
			priorIntent = stored
		}
	}
	b, err := json.Marshal(activationRecord{Trace: d.activationTrace, Previous: previous, Candidate: candidate, Phase: phase, PreviousIntent: priorIntent})
	if err != nil {
		return err
	}
	if err := atomicState(filepath.Join(d.opts.StateDir, "activation.json"), b); err != nil {
		d.autoBlocked = true
		return err
	}
	return nil
}

func (d *Daemon) freezeConfig(source, text string) (appliedConfig, error) {
	revision := configdoc.Revision(text)
	// Runtime documents deliberately retain the daemon working directory as
	// their relative-path base; moving the file does not change that base.
	dir := filepath.Join(d.opts.StateDir, "applied")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return appliedConfig{}, err
	}
	path := filepath.Join(dir, revision+".toml")
	if err := atomicState(path, []byte(text)); err != nil {
		return appliedConfig{}, err
	}
	return appliedConfig{Version: 1, Source: source, Revision: revision, Runtime: path}, nil
}

func (d *Daemon) saveApplied(a appliedConfig) error {
	b, err := json.Marshal(a)
	if err != nil {
		return err
	}
	return atomicState(filepath.Join(d.opts.StateDir, "applied-config.json"), b)
}

func (d *Daemon) initApplied() error {
	if d.activeConfigPath() == "" {
		return nil
	}
	if b, err := os.ReadFile(filepath.Join(d.opts.StateDir, "activation.json")); err == nil {
		var record activationRecord
		if err := json.Unmarshal(b, &record); err != nil {
			return err
		}
		if _, err := d.previousActivationIntent(); err != nil {
			return err
		}
		chosen := record.Previous
		if record.Phase == "committed" {
			chosen = record.Candidate
		}
		if record.Phase == "unconfirmed" {
			chosen = record.Candidate
			d.autoBlocked = true
			d.activationState.Store("unconfirmed")
			d.activationError.Store("candidate confirmation unavailable; manual authorization required")
		}
		if record.Phase == "rollback-failed" {
			d.autoBlocked = true
			d.activationState.Store("rollback-failed")
			d.activationError.Store("previous revision could not be restored; manual authorization required")
		}
		if record.Phase != "committed" && record.Phase != "starting" && record.Phase != "rollback" && record.Phase != "unconfirmed" && record.Phase != "rollback-failed" {
			return errors.New("unknown activation phase")
		}
		if chosen.Runtime != "" {
			if err := d.validateApplied(chosen); err != nil {
				return err
			}
			if err := d.saveApplied(chosen); err != nil {
				return err
			}
			if err := persistActive(d.opts.StateDir, chosen.Source); err != nil {
				return err
			}
			d.mu.Lock()
			d.activePath = chosen.Source
			d.mu.Unlock()
			if record.Phase != "committed" && record.PreviousIntent != nil {
				desired := record.PreviousIntent.Desired
				if d.intent.Desired == health.DesiredStop {
					desired = health.DesiredStop
				}
				if err := d.writeIntent(desired); err != nil {
					return err
				}
				d.engine.SetDesired(d.coreComponent(), desired)
				d.engine.SetDesired(d.instanceComponent(), desired)
			}
		}
		if record.Phase != "unconfirmed" && record.Phase != "rollback-failed" {
			if err := os.Remove(filepath.Join(d.opts.StateDir, "activation.json")); err != nil {
				return err
			}
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	b, err := os.ReadFile(filepath.Join(d.opts.StateDir, "applied-config.json"))
	if err == nil {
		if err := json.Unmarshal(b, &d.applied); err != nil {
			return err
		}
		if err := d.validateApplied(d.applied); err != nil {
			return err
		}
		d.appliedRevision.Store(d.applied.Revision)
		d.mu.Lock()
		d.activePath = d.applied.Source
		d.mu.Unlock()
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	text, err := os.ReadFile(d.activeConfigPath())
	if err != nil {
		return err
	}
	a, err := d.freezeConfig(d.activeConfigPath(), string(text))
	if err != nil {
		return err
	}
	if err := d.saveApplied(a); err != nil {
		return err
	}
	d.applied = a
	d.appliedRevision.Store(a.Revision)
	return nil
}

func (d *Daemon) runtimePath() string {
	if d.applied.Runtime != "" {
		return d.applied.Runtime
	}
	return d.activeConfigPath()
}

// checkRuntime prevents later edits to frozen bytes from becoming an implicit apply.
// Call before stopping a live process or renewing its recovery authorization.
func (d *Daemon) checkRuntime() error {
	if d.applied.Runtime == "" {
		return nil
	}
	if err := d.validateApplied(d.applied); err != nil {
		d.autoBlocked = true
		d.activationState.Store("failed")
		d.activationError.Store("applied configuration is damaged; apply a valid configuration to repair it")
		return errors.New("applied configuration is damaged; apply a valid configuration to repair it")
	}
	return nil
}

// confirmCandidate checks local operation only; no peer/address requirement.
func (d *Daemon) confirmCandidate(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for {
		if d.stopRequested.Load() || d.shutdownRequested.Load() {
			return errors.New("operation cancelled by manual stop")
		}
		if alive, _ := d.proc.Alive(); !alive {
			return errors.New("candidate process exited")
		}
		if node, err := d.cli.Node(ctx); err == nil {
			if node == nil {
				return errCandidateUnconfirmed
			}
			if d.stopRequested.Load() || d.shutdownRequested.Load() {
				return errors.New("candidate confirmation cancelled")
			}
			if ctx.Err() != nil {
				return errors.New("candidate confirmation deadline elapsed")
			}
			if alive, _ := d.proc.Alive(); !alive {
				return errors.New("candidate exited during response confirmation")
			}
			return nil
		} else {
			switch coreadapter.KindOf(err) {
			case coreadapter.ErrUnavailable, coreadapter.ErrParse, coreadapter.ErrMultiInstance, coreadapter.ErrCommand:
				return errCandidateUnconfirmed
			case coreadapter.ErrUnknown:
				return errCandidateUnconfirmed
			}
		}
		select {
		case <-ctx.Done():
			return errors.New("candidate local response confirmation timed out")
		case <-time.After(100 * time.Millisecond):
		}
	}
}

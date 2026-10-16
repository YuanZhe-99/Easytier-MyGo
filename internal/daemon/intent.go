package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/health"
)

// runIntent survives daemon restarts independently of process lifetime.
type runIntent struct {
	Version    int            `json:"version"`
	Desired    health.Desired `json:"desired"`
	Target     string         `json:"target"`
	Generation uint64         `json:"generation"`
}

func (d *Daemon) loadIntent() error {
	b, err := os.ReadFile(filepath.Join(d.opts.StateDir, "run-intent.json"))
	if errors.Is(err, os.ErrNotExist) {
		d.intent = runIntent{Version: 1, Desired: health.DesiredRun, Target: d.activeConfigPath()}
		return nil
	}
	if err != nil {
		return err
	}
	var decoded struct {
		Version    *int            `json:"version"`
		Desired    *health.Desired `json:"desired"`
		Target     *string         `json:"target"`
		Generation *uint64         `json:"generation"`
	}
	if err := json.Unmarshal(b, &decoded); err != nil {
		return fmt.Errorf("read run intent: %w", err)
	}
	if decoded.Version == nil || decoded.Desired == nil || decoded.Target == nil || decoded.Generation == nil {
		return errors.New("incomplete run intent; manual authorization required")
	}
	d.intent = runIntent{Version: *decoded.Version, Desired: *decoded.Desired, Target: *decoded.Target, Generation: *decoded.Generation}
	if d.intent.Version != 1 || (d.intent.Desired != health.DesiredRun && d.intent.Desired != health.DesiredStop) {
		return errors.New("invalid run intent; manual authorization required")
	}
	d.engine.SetDesired(d.coreComponent(), d.intent.Desired)
	d.engine.SetDesired(d.instanceComponent(), d.intent.Desired)
	return nil
}

// writeIntent is called under operationMu. Failure inhibits automatic starts.
func (d *Daemon) writeIntent(desired health.Desired) error {
	d.intent.Version = 1
	d.intent.Desired = desired
	d.intent.Target = d.activeConfigPath()
	d.intent.Generation++
	b, err := json.Marshal(d.intent)
	if err == nil {
		err = atomicState(filepath.Join(d.opts.StateDir, "run-intent.json"), b)
	}
	if err != nil {
		d.autoBlocked = true
	}
	return err
}

func atomicState(path string, data []byte) error {
	return atomicStateSteps(path, data, nil)
}

// atomicStateSteps exposes per-call boundaries to subprocess crash/failure tests.
// Production never installs a checkpoint and no environment variable changes IO.
func atomicStateSteps(path string, data []byte, checkpoint func(string) error) error {
	step := func(name string) error {
		if checkpoint != nil {
			return checkpoint(name)
		}
		return nil
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".state-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err := step("created"); err != nil {
		return err
	}
	if err = f.Chmod(0o600); err == nil {
		err = step("chmod")
	}
	if err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = step("written")
	}
	if err == nil {
		err = f.Sync()
	}
	if err == nil {
		err = step("file-synced")
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := step("closed"); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return err
	}
	if err := step("renamed"); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	if err := step("directory-opened"); err != nil {
		return err
	}
	if err := dir.Sync(); err != nil {
		return err
	}
	return step("directory-synced")
}

func (d *Daemon) initialStart() error {
	d.operationMu.Lock()
	defer d.operationMu.Unlock()
	if d.autoBlocked || d.intent.Desired == health.DesiredStop || d.stopRequested.Load() || d.shutdownRequested.Load() {
		return nil
	}
	for _, a := range d.engine.Attempts() {
		if (a.Component == d.coreComponent() || a.Component == d.instanceComponent()) && len(a.Times) >= d.opts.Policy.MaxAttempts {
			return nil
		}
	}
	return d.startCoreLocked()
}

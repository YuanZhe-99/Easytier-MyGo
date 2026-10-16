package supervisor

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"
)

// coreProcess runs one easytier-core child. Its stdout/stderr go to a log
// file in the network's state directory (they can contain configuration
// fragments; the file stays local, mode 0600).
type coreProcess struct {
	bin     string
	logPath string

	mu        sync.Mutex
	cmd       *exec.Cmd
	exited    bool
	exitErr   error
	stopping  bool
	waitCh    chan struct{}
	startedAt time.Time
	// exitNotify receives one value whenever a started process exits.
	exitNotify chan struct{}
}

func newCoreProcess(bin, logPath string) *coreProcess {
	return &coreProcess{bin: bin, logPath: logPath, exitNotify: make(chan struct{}, 1)}
}

// Start launches the core with args. It fails if a process is running.
func (p *coreProcess) Start(args []string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd != nil && !p.exited {
		return errors.New("core already running")
	}
	logFile, err := os.OpenFile(p.logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open core log: %w", err)
	}
	cmd := exec.Command(p.bin, args...)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	cmd.SysProcAttr = managedProcessAttrs()
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("start core: %w", err)
	}
	p.cmd, p.exited, p.exitErr, p.stopping = cmd, false, nil, false
	p.startedAt = time.Now()
	waitCh := make(chan struct{})
	p.waitCh = waitCh
	go func() {
		err := cmd.Wait()
		logFile.Close()
		p.mu.Lock()
		p.exited, p.exitErr = true, err
		p.mu.Unlock()
		close(waitCh)
		select {
		case p.exitNotify <- struct{}{}:
		default:
		}
	}()
	return nil
}

// Stop asks the core's process group to terminate, waits up to timeout,
// then kills it.
func (p *coreProcess) Stop(timeout time.Duration) error {
	p.mu.Lock()
	if p.cmd == nil || p.exited {
		p.mu.Unlock()
		return nil
	}
	p.stopping = true
	pid, waitCh := p.cmd.Process.Pid, p.waitCh
	p.mu.Unlock()

	terminateGroup(pid)
	select {
	case <-waitCh:
		return nil
	case <-time.After(timeout):
	}
	killGroup(pid)
	select {
	case <-waitCh:
		return nil
	case <-time.After(2 * time.Second):
		return errors.New("core did not exit after being killed")
	}
}

// Alive reports whether the process runs; otherwise detail says why not.
func (p *coreProcess) Alive() (alive bool, detail string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	switch {
	case p.cmd != nil && !p.exited:
		return true, ""
	case p.cmd == nil:
		return false, "not started"
	case p.stopping:
		return false, "stopped"
	case p.exitErr != nil:
		return false, "exited: " + p.exitErr.Error()
	default:
		return false, "exited"
	}
}

// StartedAt returns when the running process started (zero when none).
func (p *coreProcess) StartedAt() time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd == nil || p.exited {
		return time.Time{}
	}
	return p.startedAt
}

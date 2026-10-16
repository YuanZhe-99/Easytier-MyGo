package daemon

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"
)

// coreProcess runs the managed easytier-core child. The daemon starts it
// with the active config file and the management portal address; the
// core's stdout/stderr go to a log file in the state directory (they can
// contain configuration fragments; the file stays local).
type coreProcess struct {
	bin       string
	rpcPortal string
	logPath   string
	logf      func(string, ...any)
	logging   LoggingOptions

	mu       sync.Mutex
	cmd      *exec.Cmd
	stopping bool
	exited   bool
	exitErr  error
	waitCh   chan struct{}
	// configPath is what the running process was started with, for logs.
	configPath string
}

func newCoreProcess(bin, rpc, logPath string, logf func(string, ...any)) *coreProcess {
	if bin == "" {
		bin = "easytier-core"
	}
	return &coreProcess{
		bin:       bin,
		rpcPortal: rpc,
		logPath:   logPath,
		logf:      logf,
	}
}

// Start launches the core with a config file. It fails if a process is
// already running.
func (p *coreProcess) Start(configPath string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd != nil && !p.exited {
		return errors.New("core already running")
	}

	loggingArgs, err := p.logging.args(p.logPath)
	if err != nil {
		return fmt.Errorf("core logging options: %w", err)
	}
	logFile, err := os.OpenFile(p.logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open core log: %w", err)
	}

	args := append([]string{"-c", configPath, "--rpc-portal", p.rpcPortal}, loggingArgs...)
	cmd := exec.Command(p.bin, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = managedProcessAttrs()
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("start core: %w", err)
	}

	p.cmd = cmd
	p.stopping = false
	p.exited = false
	p.exitErr = nil
	p.configPath = configPath
	waitCh := make(chan struct{})
	p.waitCh = waitCh
	pid := cmd.Process.Pid

	go func() {
		err := cmd.Wait()
		p.mu.Lock()
		p.exited = true
		p.exitErr = err
		p.mu.Unlock()
		close(waitCh)
		logFile.Close()
		p.logf("daemon: core exited pid=%d err=%v", pid, err)
	}()

	// In its own process group so a Ctrl+C aimed at the daemon's
	// terminal does not race the daemon's own ordered shutdown.
	p.logf("daemon: core started pid=%d config=%s", pid, configPath)
	return nil
}

// Stop sends SIGTERM, waits for the grace period, then SIGKILLs. The
// process group is signalled so nothing under the core outlives it.
func (p *coreProcess) Stop(timeout time.Duration) error {
	p.mu.Lock()
	cmd := p.cmd
	waitCh := p.waitCh
	if cmd == nil || p.exited {
		p.mu.Unlock()
		return nil
	}
	p.stopping = true
	pid := cmd.Process.Pid
	p.mu.Unlock()

	terminateGroup(pid)

	select {
	case <-waitCh:
		return nil
	case <-time.After(timeout):
		p.logf("daemon: core did not stop in %v; killing", timeout)
		killGroup(pid)
		select {
		case <-waitCh:
		case <-time.After(2 * time.Second):
			return errors.New("core did not exit after SIGKILL")
		}
		return nil
	}
}

// Alive reports whether the process is running; when it is not, the
// detail describes the exit.
func (p *coreProcess) Alive() (bool, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd != nil && !p.exited {
		return true, ""
	}
	if p.cmd == nil {
		return false, "core not started"
	}
	if p.stopping {
		return false, "stopped by request"
	}
	if p.exitErr != nil {
		return false, "exited: " + p.exitErr.Error()
	}
	return false, "exited"
}

// Pid returns the running process id, or 0.
func (p *coreProcess) Pid() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd == nil || p.exited {
		return 0
	}
	return p.cmd.Process.Pid
}

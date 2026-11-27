package supervisor

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const coreLogMax = 10 << 20 // 10 MiB hard cap on core.log

// coreProcess runs one easytier-core child. Its stdout/stderr go to a
// bounded error-only log file in the network's state directory (they can
// contain configuration fragments; the file stays local, mode 0600).
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
	logWriter, err := newCoreLog(p.logPath, coreLogMax)
	if err != nil {
		return fmt.Errorf("open core log: %w", err)
	}
	cmd := exec.Command(p.bin, args...)
	cmd.Stdout, cmd.Stderr = logWriter, logWriter
	cmd.SysProcAttr = managedProcessAttrs()
	if err := cmd.Start(); err != nil {
		_ = logWriter.Close()
		return fmt.Errorf("start core: %w", err)
	}
	adoptProcess(cmd.Process)
	p.cmd, p.exited, p.exitErr, p.stopping = cmd, false, nil, false
	p.startedAt = time.Now()
	waitCh := make(chan struct{})
	p.waitCh = waitCh
	go func() {
		err := cmd.Wait()
		_ = logWriter.Close()
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

// coreLog filters core output to errors only (dropping INFO/WARN/DEBUG/TRACE
// records and their continuation lines) and keeps the single log file strictly
// within max bytes by trimming old content in-place.
type coreLog struct {
	mu       sync.Mutex
	path     string
	max      int64
	f        *os.File
	size     int64
	buf      []byte
	dropping bool
}

// newCoreLog opens path (creating it mode 0600), removes any legacy ".1"
// backup, and immediately shrinks an oversized existing log to <= max/2
// (keeping only error lines from its tail).
func newCoreLog(path string, max int64) (*coreLog, error) {
	_ = os.Remove(path + ".1")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	w := &coreLog{path: path, max: max, f: f, size: info.Size()}
	if max > 0 && w.size > max {
		if err := w.shrinkOversized(); err != nil {
			_ = f.Close()
			return nil, err
		}
	}
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		_ = f.Close()
		return nil, err
	}
	return w, nil
}

// shrinkOversized trims an already-oversized core.log down to the filtered
// error lines from its last max/2 bytes so multi-gigabyte logs are reclaimed
// immediately without scanning the whole file.
func (w *coreLog) shrinkOversized() error {
	keep := max(w.max/2, 1)
	start := max(w.size-keep, 0)
	if _, err := w.f.Seek(start, io.SeekStart); err != nil {
		return err
	}
	tail, err := io.ReadAll(w.f)
	if err != nil {
		return err
	}
	if start > 0 {
		if i := bytes.IndexByte(tail, '\n'); i >= 0 {
			tail = tail[i+1:]
		} else {
			tail = nil
		}
	}
	filtered := filterErrorLines(tail)
	return w.rewrite(filtered)
}

func filterErrorLines(data []byte) []byte {
	var out []byte
	dropping := false
	for len(data) > 0 {
		var line []byte
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			line = data[:i+1]
			data = data[i+1:]
		} else {
			line = data
			data = nil
		}
		if keepLogLine(line, &dropping) {
			out = append(out, line...)
		}
	}
	return out
}

// keepLogLine drops timestamped INFO/WARN/DEBUG/TRACE records and their
// indented multi-line continuation blocks while retaining ERROR and panic text.
func keepLogLine(line []byte, dropping *bool) bool {
	if level, ok := tracingLevel(line); ok {
		*dropping = level != "ERROR"
		return !*dropping
	}
	if *dropping && isContinuationLine(line) {
		return false
	}
	*dropping = false
	return true
}

func tracingLevel(line []byte) (string, bool) {
	s := string(bytes.TrimRight(line, "\r\n"))
	// EasyTier tracing format: "2026-11-27T18:17:10.905Z  INFO ..."
	if len(s) < 20 || s[4] != '-' || s[7] != '-' || s[10] != 'T' {
		return "", false
	}
	sp := strings.IndexByte(s, ' ')
	if sp < 0 {
		return "", false
	}
	rest := strings.TrimLeft(s[sp:], " ")
	for _, lvl := range []string{"ERROR", "WARN", "INFO", "DEBUG", "TRACE"} {
		if strings.HasPrefix(rest, lvl+" ") || rest == lvl {
			return lvl, true
		}
	}
	return "", false
}

func isContinuationLine(line []byte) bool {
	trimmed := bytes.TrimRight(line, "\r\n")
	if len(trimmed) == 0 {
		return true
	}
	switch trimmed[0] {
	case ' ', '\t', ')', '}', ']':
		return true
	}
	return false
}

func (w *coreLog) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return 0, os.ErrClosed
	}
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		line := w.buf[:i+1]
		if keepLogLine(line, &w.dropping) {
			if err := w.writeBounded(line); err != nil {
				return 0, err
			}
		}
		w.buf = w.buf[i+1:]
	}
	// Bound buffered partial line if a huge single line arrives without '\n'.
	if w.max > 0 && int64(len(w.buf)) > w.max {
		if keepLogLine(w.buf, &w.dropping) {
			if err := w.writeBounded(w.buf); err != nil {
				return 0, err
			}
		}
		w.buf = nil
	}
	return len(p), nil
}

func (w *coreLog) writeBounded(line []byte) error {
	if w.max > 0 && int64(len(line)) >= w.max {
		return w.rewrite(line[int64(len(line))-w.max:])
	}
	if w.max > 0 && w.size+int64(len(line)) > w.max {
		if err := w.trimFor(int64(len(line))); err != nil {
			return err
		}
	}
	n, err := w.f.Write(line)
	w.size += int64(n)
	return err
}

// trimFor discards the oldest lines so that adding incoming bytes stays <= max.
func (w *coreLog) trimFor(incoming int64) error {
	target := min(w.max/2, w.max-incoming)
	if target <= 0 {
		return w.rewrite(nil)
	}
	start := max(w.size-target, 0)
	if _, err := w.f.Seek(start, io.SeekStart); err != nil {
		return err
	}
	tail, err := io.ReadAll(w.f)
	if err != nil {
		return err
	}
	if start > 0 {
		if i := bytes.IndexByte(tail, '\n'); i >= 0 {
			tail = tail[i+1:]
		} else {
			tail = nil
		}
	}
	return w.rewrite(tail)
}

func (w *coreLog) rewrite(data []byte) error {
	if _, err := w.f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := w.f.Truncate(0); err != nil {
		return err
	}
	n, err := w.f.Write(data)
	w.size = int64(n)
	return err
}

func (w *coreLog) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return nil
	}
	if len(w.buf) > 0 {
		if keepLogLine(w.buf, &w.dropping) {
			_ = w.writeBounded(w.buf)
		}
		w.buf = nil
	}
	err := w.f.Close()
	w.f = nil
	return err
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

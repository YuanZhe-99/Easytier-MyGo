package certmanager

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Hook limits.
const (
	DefaultHookTimeout = 30 * time.Second
	MaxHookTimeout     = 10 * time.Minute
	maxHookOutput      = 16 << 10
)

// Shell names how a hook's Command runs.
type Shell string

const (
	ShellDefault    Shell = ""           // sh -c on Unix, cmd /C on Windows
	ShellSh         Shell = "sh"         // sh -c
	ShellCmd        Shell = "cmd"        // cmd /C (Windows)
	ShellPowerShell Shell = "powershell" // powershell -NoProfile -NonInteractive -Command (Windows)
)

// Hook is the command a job runs after it wrote a new certificate.
type Hook struct {
	// Enabled switches the hook on; a configured but disabled hook is kept.
	Enabled bool `json:"enabled"`
	// Command is a shell command line; Args, when set, is run directly
	// instead (Args[0] is the program) and Command is ignored.
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
	Shell   Shell    `json:"shell,omitempty"`
	// TimeoutSeconds bounds the run (0: 30 s; at most 600).
	TimeoutSeconds int `json:"timeout_seconds,omitempty"`
}

// HookResult is the outcome of one hook run. Reported in Status.
type HookResult struct {
	Time       time.Time `json:"time"`
	DurationMS int64     `json:"duration_ms"`
	ExitCode   int       `json:"exit_code"` // -1 when it did not exit by itself
	OK         bool      `json:"ok"`
	TimedOut   bool      `json:"timed_out,omitempty"`
	// PermissionDenied marks a failure that looks like missing rights
	// (exit code 126, "permission denied", the Docker socket, …).
	PermissionDenied bool   `json:"permission_denied,omitempty"`
	Error            string `json:"error,omitempty"`
	// Output is stdout and stderr combined, cut to its last 16 KiB.
	Output string `json:"output,omitempty"`
}

// HookVars are the values of the placeholders a hook may use:
// {{CERT_PATH}}, {{KEY_PATH}}, {{FULLCHAIN_PATH}}, {{OUT_DIR}}.
type HookVars struct {
	CertPath, KeyPath, FullchainPath, OutDir string
}

func (v HookVars) replacer() *strings.Replacer {
	return strings.NewReplacer(
		"{{CERT_PATH}}", v.CertPath, "{{KEY_PATH}}", v.KeyPath,
		"{{FULLCHAIN_PATH}}", v.FullchainPath, "{{OUT_DIR}}", v.OutDir)
}

func (v HookVars) env() []string {
	return []string{
		"EASYTIER_MYGO_CERT_PATH=" + v.CertPath, "EASYTIER_MYGO_KEY_PATH=" + v.KeyPath,
		"EASYTIER_MYGO_FULLCHAIN_PATH=" + v.FullchainPath, "EASYTIER_MYGO_OUT_DIR=" + v.OutDir,
	}
}

// Validate checks a hook's shape (not whether its program exists).
func (h Hook) Validate() error {
	if !h.Enabled && h.Command == "" && len(h.Args) == 0 {
		return nil
	}
	if strings.TrimSpace(h.Command) == "" && len(h.Args) == 0 {
		return errors.New("the hook has no command")
	}
	if len(h.Args) > 0 && strings.TrimSpace(h.Args[0]) == "" {
		return errors.New("the hook's program is empty")
	}
	if h.TimeoutSeconds < 0 || time.Duration(h.TimeoutSeconds)*time.Second > MaxHookTimeout {
		return fmt.Errorf("the hook timeout must be 0 to %d seconds", int(MaxHookTimeout/time.Second))
	}
	switch h.Shell {
	case ShellDefault, ShellSh, ShellCmd, ShellPowerShell:
	default:
		return fmt.Errorf("unknown shell %q (use sh, cmd or powershell)", h.Shell)
	}
	return nil
}

// command builds the process for a hook with its placeholders replaced.
func (h Hook) command(ctx context.Context, vars HookVars) (*exec.Cmd, error) {
	r := vars.replacer()
	if len(h.Args) > 0 {
		args := make([]string, len(h.Args))
		for i, a := range h.Args {
			args[i] = r.Replace(a)
		}
		return exec.CommandContext(ctx, args[0], args[1:]...), nil
	}
	line := r.Replace(h.Command)
	shell := h.Shell
	if shell == ShellDefault {
		shell = ShellSh
		if runtime.GOOS == "windows" {
			shell = ShellCmd
		}
	}
	switch shell {
	case ShellSh:
		return exec.CommandContext(ctx, "sh", "-c", line), nil
	case ShellCmd:
		if runtime.GOOS != "windows" {
			return nil, errors.New("the cmd shell exists only on Windows")
		}
		cmd := exec.CommandContext(ctx, "cmd")
		rawCmdLine(cmd, line)
		return cmd, nil
	case ShellPowerShell:
		ps := "powershell"
		if runtime.GOOS != "windows" {
			ps = "pwsh"
		}
		return exec.CommandContext(ctx, ps, "-NoProfile", "-NonInteractive", "-Command", line), nil
	}
	return nil, fmt.Errorf("unknown shell %q", shell)
}

// RunHook runs a hook to completion or until its timeout.
// Inputs: vars fill the placeholders and EASYTIER_MYGO_* environment
// variables; ctx cancellation stops the hook like a timeout.
// Returns: the result; it never panics or blocks past timeout + 3 s.
// Side effects: starts a process (on Unix in its own process group, which
// is killed as a whole when the hook runs too long).
func RunHook(ctx context.Context, h Hook, vars HookVars) HookResult {
	start := time.Now()
	res := HookResult{Time: start, ExitCode: -1}
	timeout := time.Duration(h.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = DefaultHookTimeout
	}
	timeout = min(timeout, MaxHookTimeout)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd, err := h.command(ctx, vars)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	out := &tailBuffer{max: maxHookOutput}
	cmd.Stdout, cmd.Stderr = out, out
	cmd.Stdin = nil
	cmd.Env = append(os.Environ(), vars.env()...)
	cmd.Dir = vars.OutDir
	if cmd.Dir != "" {
		if _, err := os.Stat(cmd.Dir); err != nil {
			cmd.Dir = ""
		}
	}
	cmd.WaitDelay = 2 * time.Second
	setProcessGroup(cmd)
	err = cmd.Run()
	res.DurationMS = time.Since(start).Milliseconds()
	res.Output = out.String()
	if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
		res.ExitCode = cmd.ProcessState.ExitCode()
	}
	switch {
	case err == nil:
		res.OK = true
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		res.TimedOut = true
		res.Error = fmt.Sprintf("the hook did not finish within %s and was stopped", timeout)
	case ctx.Err() != nil:
		res.Error = "the hook was stopped"
	default:
		res.Error = err.Error()
	}
	if !res.OK {
		res.PermissionDenied = looksLikePermission(err, res.ExitCode, res.Output)
	}
	return res
}

// looksLikePermission recognizes the usual signs of missing rights.
func looksLikePermission(err error, code int, output string) bool {
	if errors.Is(err, os.ErrPermission) || code == 126 {
		return true
	}
	o := strings.ToLower(output)
	for _, s := range []string{"permission denied", "access is denied", "operation not permitted",
		"docker.sock", "interactive authentication required", "must be run as root", "are you root"} {
		if strings.Contains(o, s) {
			return true
		}
	}
	return false
}

// tailBuffer keeps the last max bytes written to it. Safe for the two
// writers exec uses for stdout and stderr.
type tailBuffer struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	max       int
	truncated bool
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf.Write(p)
	if over := t.buf.Len() - t.max; over > 0 {
		t.buf.Next(over)
		t.truncated = true
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := strings.ToValidUTF8(t.buf.String(), "�")
	if t.truncated {
		s = "…" + s
	}
	return s
}

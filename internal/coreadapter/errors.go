package coreadapter

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ErrorKind classifies why a CLI query failed. Health decisions depend on
// this classification: a timeout or refused RPC is evidence about the
// core, a missing binary or parse error is evidence about the adapter.
type ErrorKind uint8

const (
	// ErrUnknown is an unclassified failure.
	ErrUnknown ErrorKind = iota
	// ErrUnavailable means the CLI binary could not be run at all
	// (missing, not executable). This is an application-side problem,
	// not core health evidence.
	ErrUnavailable
	// ErrTimeout means the command did not finish within the deadline.
	// Combined with repeated failures it is core-unresponsive evidence.
	ErrTimeout
	// ErrRPCUnreachable means the CLI ran but could not reach the core's
	// RPC portal (usually connection refused).
	ErrRPCUnreachable
	// ErrNoInstances means the core is reachable but has no running
	// network instances ("no running instances found").
	ErrNoInstances
	// ErrCommand means the command exited non-zero for another reason.
	ErrCommand
	// ErrParse means the command output could not be decoded.
	ErrParse
	// ErrMultiInstance means the output described more than one instance,
	// which this adapter's callers do not expect (one instance per core).
	ErrMultiInstance
)

func (k ErrorKind) String() string {
	switch k {
	case ErrUnavailable:
		return "unavailable"
	case ErrTimeout:
		return "timeout"
	case ErrRPCUnreachable:
		return "rpc-unreachable"
	case ErrNoInstances:
		return "no-instances"
	case ErrCommand:
		return "command-failed"
	case ErrParse:
		return "parse"
	case ErrMultiInstance:
		return "multi-instance"
	default:
		return "unknown"
	}
}

// CommandError is returned for every failed CLI query.
type CommandError struct {
	Kind     ErrorKind
	Args     []string
	ExitCode int
	Stderr   string
	// Instances lists instance labels for ErrMultiInstance.
	Instances []string
	Err       error
}

func (e *CommandError) Error() string {
	var b strings.Builder
	b.WriteString("easytier-cli ")
	b.WriteString(strings.Join(e.Args, " "))
	b.WriteString(": ")
	b.WriteString(e.Kind.String())
	if e.ExitCode != 0 {
		fmt.Fprintf(&b, " (exit %d)", e.ExitCode)
	}
	if msg := firstStderrLine(e.Stderr); msg != "" {
		b.WriteString(": ")
		b.WriteString(msg)
	}
	if len(e.Instances) > 0 {
		b.WriteString(": instances " + strings.Join(e.Instances, ", "))
	}
	return b.String()
}

func (e *CommandError) Unwrap() error { return e.Err }

// KindOf extracts the classification of err, or ErrUnknown.
func KindOf(err error) ErrorKind {
	var ce *CommandError
	if errors.As(err, &ce) {
		return ce.Kind
	}
	return ErrUnknown
}

// firstStderrLine picks the most useful first line of the CLI's error
// output, so diagnostics and events keep a short attributable cause
// instead of the binary's full anyhow chain.
func firstStderrLine(stderr string) string {
	for _, line := range strings.Split(strings.TrimSpace(stderr), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = strings.TrimPrefix(line, "Error: ")
		if line == "" {
			continue
		}
		// Skip anyhow's numbered chain frames ("0: ...").
		if len(line) > 1 && line[1] == ':' && line[0] >= '0' && line[0] <= '9' {
			continue
		}
		return line
	}
	return ""
}

// classifyStderr maps the CLI's error text onto an ErrorKind.
func classifyStderr(stderr string) ErrorKind {
	s := strings.ToLower(stderr)
	switch {
	case strings.Contains(s, "connection refused"),
		strings.Contains(s, "failed to connect to server"):
		return ErrRPCUnreachable
	case strings.Contains(s, "no running instances found"):
		return ErrNoInstances
	case strings.Contains(s, "connect timeout"),
		strings.Contains(s, "timed out"),
		strings.Contains(s, "timeout"):
		return ErrTimeout
	default:
		return ErrCommand
	}
}

// classifyExecErr maps process-start failures.
func classifyExecErr(err error) ErrorKind {
	if err == nil {
		return ErrUnknown
	}
	if errors.Is(err, exec.ErrNotFound) || strings.Contains(err.Error(), "executable file not found") {
		return ErrUnavailable
	}
	return ErrCommand
}

func multiInstanceError(args []string, labels []string) *CommandError {
	return &CommandError{
		Kind:      ErrMultiInstance,
		Args:      args,
		Instances: labels,
	}
}

func parseError(args []string, err error) *CommandError {
	return &CommandError{Kind: ErrParse, Args: args, Err: err}
}

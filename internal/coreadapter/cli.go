package coreadapter

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"
)

// DefaultRPC is the core's default RPC portal address.
const DefaultRPC = "127.0.0.1:15888"

// DefaultTimeout bounds one CLI query. The health checks rely on every
// query finishing or failing within it.
const DefaultTimeout = 3 * time.Second

const maxCLIOutput = 8 << 20

var errCLIOutputLimit = errors.New("CLI output exceeds eight MiB limit")

type limitedOutput struct {
	buffer   bytes.Buffer
	exceeded bool
}

func (b *limitedOutput) Len() int       { return b.buffer.Len() }
func (b *limitedOutput) Bytes() []byte  { return b.buffer.Bytes() }
func (b *limitedOutput) String() string { return b.buffer.String() }

func (b *limitedOutput) Write(p []byte) (int, error) {
	remaining := maxCLIOutput - b.Len()
	if len(p) > remaining {
		b.exceeded = true
		n, _ := b.buffer.Write(p[:remaining])
		return n, errCLIOutputLimit
	}
	return b.buffer.Write(p)
}

// CLI runs the easytier-cli companion binary against one core RPC portal.
// A CLI value is safe for concurrent use.
type CLI struct {
	// Bin is the path of the easytier-cli binary. When empty, "easytier-cli"
	// is looked up in PATH.
	Bin string
	// RPC is the core's RPC portal, "host:port".
	RPC string
	// Timeout bounds a single query. Zero means DefaultTimeout.
	Timeout time.Duration
}

func (c CLI) bin() string {
	if c.Bin != "" {
		return c.Bin
	}
	return "easytier-cli"
}

func (c CLI) rpc() string {
	if c.RPC != "" {
		return c.RPC
	}
	return DefaultRPC
}

func (c CLI) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return DefaultTimeout
}

// run executes one query and returns its stdout. rpcSelect adds the
// global -p flag; service management subcommands omit it because they do
// not talk to the core.
func (c CLI) run(ctx context.Context, rpcSelect bool, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout())
	defer cancel()

	full := make([]string, 0, len(args)+2)
	if rpcSelect {
		full = append(full, "-p", c.rpc())
	}
	full = append(full, args...)

	cmd := exec.CommandContext(ctx, c.bin(), full...)
	var stdout, stderr limitedOutput
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.WaitDelay = time.Second

	err := cmd.Run()
	if stdout.exceeded || stderr.exceeded {
		return nil, &CommandError{Args: full, Kind: ErrParse, Err: errCLIOutputLimit}
	}
	if err == nil {
		return stdout.Bytes(), nil
	}

	ce := &CommandError{Args: full, Stderr: stderr.String(), Err: err}
	if ctx.Err() == context.DeadlineExceeded {
		ce.Kind = ErrTimeout
		return nil, ce
	}
	var exitErr *exec.ExitError
	if ok := asExitError(err, &exitErr); ok {
		ce.ExitCode = exitErr.ExitCode()
		ce.Kind = classifyStderr(stderr.String())
		return nil, ce
	}
	ce.Kind = classifyExecErr(err)
	return nil, ce
}

// asExitError is errors.As specialized to avoid importing errors here.
func asExitError(err error, target **exec.ExitError) bool {
	e, ok := err.(*exec.ExitError)
	if ok {
		*target = e
	}
	return ok
}

// CLIVersion reports the CLI binary's version ("2.6.4-8428a89d").
func (c CLI) CLIVersion(ctx context.Context) (string, error) {
	out, err := c.run(ctx, false, "--version")
	if err != nil {
		return "", err
	}
	fields := strings.Fields(strings.TrimSpace(string(out)))
	if len(fields) == 0 {
		return "", parseError([]string{"--version"}, errNoVersion)
	}
	return fields[len(fields)-1], nil
}

// Node returns the core's own status.
func (c CLI) Node(ctx context.Context) (*NodeInfo, error) {
	out, err := c.run(ctx, true, "-o", "json", "node")
	if err != nil {
		return nil, err
	}
	return decodeNode(out)
}

// Peers returns the peer list in the CLI's table shape.
func (c CLI) Peers(ctx context.Context) ([]Peer, error) {
	out, err := c.run(ctx, true, "-o", "json", "peer")
	if err != nil {
		return nil, err
	}
	return decodePeers(out)
}

// PeersVerbose returns the peer list with raw connection counters.
func (c CLI) PeersVerbose(ctx context.Context) ([]PeerRoutePair, error) {
	out, err := c.run(ctx, true, "-v", "-o", "json", "peer")
	if err != nil {
		return nil, err
	}
	return decodePeerVerbose(out)
}

// Routes returns the routing table.
func (c CLI) Routes(ctx context.Context) ([]Route, error) {
	out, err := c.run(ctx, true, "-o", "json", "route")
	if err != nil {
		return nil, err
	}
	return decodeRoutes(out)
}

// Connectors returns the configured connectors and their states.
func (c CLI) Connectors(ctx context.Context) ([]Connector, error) {
	out, err := c.run(ctx, true, "-o", "json", "connector")
	if err != nil {
		return nil, err
	}
	return decodeConnectors(out)
}

// ServiceStatus returns the raw text of `easytier-cli service status`,
// which does not follow the JSON output switch. The daemon interprets it.
func (c CLI) ServiceStatus(ctx context.Context) (string, error) {
	out, err := c.run(ctx, false, "service", "status")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

package coreadapter

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestLiveCore queries a running dev core through the real easytier-cli. It
// runs only when EASYTIER_CLI (the CLI's path) and EASYTIER_RPC (an isolated
// core's portal, "host:port") are set, and guards the decoders against the
// bundled version's actual output.
func TestLiveCore(t *testing.T) {
	bin, rpc := os.Getenv("EASYTIER_CLI"), os.Getenv("EASYTIER_RPC")
	if bin == "" || rpc == "" {
		t.Skip("set EASYTIER_CLI and EASYTIER_RPC to query a live core")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := CLI{Bin: bin, RPC: rpc}

	if v, err := c.CLIVersion(ctx); err != nil || v == "" {
		t.Fatalf("CLIVersion = %q, %v", v, err)
	}
	node, err := c.Node(ctx)
	if err != nil {
		t.Fatalf("Node: %v", err)
	}
	if node.Hostname == "" || node.Version == "" {
		t.Fatalf("node missing fields: %+v", node)
	}
	for name, query := range map[string]func() error{
		"Peers":        func() error { _, err := c.Peers(ctx); return err },
		"PeersVerbose": func() error { _, err := c.PeersVerbose(ctx); return err },
		"Routes":       func() error { _, err := c.Routes(ctx); return err },
		"Connectors":   func() error { _, err := c.Connectors(ctx); return err },
	} {
		if err := query(); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

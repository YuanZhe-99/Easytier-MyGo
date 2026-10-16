package coreadapter

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// fixture reads real easytier-cli output captured from the bundled core
// (testdata; re-capture when the bundled EasyTier version moves).
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestDecodeRealOutput(t *testing.T) {
	node, err := decodeNode(fixture(t, "node.json"))
	if err != nil {
		t.Fatal(err)
	}
	if node.IPv4Addr == "" || node.Hostname == "" || node.Version == "" {
		t.Fatalf("node missing fields: %+v", node)
	}

	pairs, err := decodePeerVerbose(fixture(t, "peer-verbose.json"))
	if err != nil {
		t.Fatal(err)
	}
	var counted bool
	for _, p := range pairs {
		for _, c := range p.Peer.Conns {
			if c.Stats.RxBytes > 0 || c.Stats.TxBytes > 0 {
				counted = true
			}
		}
	}
	if !counted {
		t.Fatal("verbose peers carry no traffic counters; charts depend on them")
	}

	for name, decode := range map[string]func([]byte) error{
		"peer.json":      func(b []byte) error { _, err := decodePeers(b); return err },
		"route.json":     func(b []byte) error { _, err := decodeRoutes(b); return err },
		"connector.json": func(b []byte) error { _, err := decodeConnectors(b); return err },
	} {
		if err := decode(fixture(t, name)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// TestMultiInstance: one wrapped instance is unwrapped, several are an
// error rather than a silent misparse.
func TestMultiInstance(t *testing.T) {
	one := `[{"instance_id":"a","instance_name":"n1","result":{"hostname":"h"}}]`
	node, err := decodeNode([]byte(one))
	if err != nil || node.Hostname != "h" {
		t.Fatalf("single wrapped instance: %+v, %v", node, err)
	}
	two := `[{"instance_id":"a","result":{}},{"instance_id":"b","result":{}}]`
	if _, err := decodeNode([]byte(two)); KindOf(err) != ErrMultiInstance {
		t.Fatalf("two instances: %v, want multi-instance", err)
	}
	if _, err := decodePeers([]byte(`[]`)); err != nil {
		t.Fatalf("empty peer list: %v", err)
	}
}

// TestErrorClassification decides between core evidence (refused,
// timeout) and adapter problems (missing binary, bad output).
func TestErrorClassification(t *testing.T) {
	cases := []struct {
		stderr string
		want   ErrorKind
	}{
		{string(fixture(t, "error-conn-refused.txt")), ErrRPCUnreachable},
		{"Error: no running instances found", ErrNoInstances},
		{"Error: connect timeout", ErrTimeout},
		{"Error: something else", ErrCommand},
	}
	for _, tc := range cases {
		if got := classifyStderr(tc.stderr); got != tc.want {
			t.Errorf("classifyStderr(%.40q) = %v, want %v", tc.stderr, got, tc.want)
		}
	}
	if got := classifyExecErr(&os.PathError{Op: "exec", Err: errors.New("executable file not found in $PATH")}); got != ErrUnavailable {
		t.Errorf("missing binary = %v, want unavailable", got)
	}
	if _, err := decodeNode([]byte("not json")); KindOf(err) != ErrParse {
		t.Errorf("garbage output = %v, want parse", KindOf(err))
	}
}

func TestServiceStatus(t *testing.T) {
	installed, running, ok := InterpretServiceStatus(string(fixture(t, "service-status-not-installed.txt")))
	if !ok || installed || running {
		t.Fatalf("not installed: installed=%v running=%v ok=%v", installed, running, ok)
	}
}

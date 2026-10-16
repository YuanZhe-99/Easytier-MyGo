package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/health"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/localapi"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/supervisor"
)

// The test binary doubles as a fake easytier-core and easytier-cli: the
// tests run it through symlinks with those names.
//
// Fake core: `--check-config -c FILE` fails when FILE contains "invalid";
// a run exits at once when its config contains "exit-at-start", else it
// sleeps until killed.
// Fake cli: prints a node (one peer) for `node` and a peer pair for
// `-v peer`; with a "cli-refuse" file next to the binary it fails like an
// unreachable portal.
func TestMain(m *testing.M) {
	switch filepath.Base(os.Args[0]) {
	case "easytier-core":
		fakeCore(os.Args[1:])
	case "easytier-cli":
		fakeCLI(os.Args[1:])
	}
	// Race-instrumented children otherwise sleep one second at exit.
	os.Setenv("GORACE", "atexit_sleep_ms=0")
	os.Exit(m.Run())
}

func fakeCore(args []string) {
	var cfg string
	check := false
	for i, a := range args {
		switch a {
		case "-c":
			cfg = args[i+1]
		case "--check-config":
			check = true
		}
	}
	text, _ := os.ReadFile(cfg)
	if check {
		if strings.Contains(string(text), "invalid") {
			fmt.Println("Error: invalid config")
			os.Exit(1)
		}
		os.Exit(0)
	}
	if strings.Contains(string(text), "exit-at-start") {
		os.Exit(3)
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, os.Interrupt)
	<-sig
	os.Exit(0)
}

func fakeCLI(args []string) {
	dir := filepath.Dir(os.Args[0])
	if _, err := os.Stat(filepath.Join(dir, "cli-refuse")); err == nil {
		fmt.Fprintln(os.Stderr, "Error: failed to get manage client\nConnection refused (os error 111)")
		os.Exit(1)
	}
	if args[len(args)-1] == "node" {
		fmt.Print(`{"peer_id":1,"ipv4_addr":"10.9.0.1/24","hostname":"me","version":"2.6.4","stun_info":{"udp_nat_type":6},"config":"network_secret = \"x\""}`)
	} else {
		fmt.Print(`[{"route":{"peer_id":2,"hostname":"peer","cost":1,"ipv4_addr":{"address":{"addr":168361986},"network_length":24}},"peer":{"peer_id":2,"conns":[{"conn_id":"c","peer_id":2,"tunnel":{"tunnel_type":"tcp"},"stats":{"rx_bytes":100,"tx_bytes":50,"latency_us":1500}}]}}]`)
	}
	os.Exit(0)
}

const noTUN = "instance_name = \"t\"\n[network_identity]\nnetwork_name = \"n\"\nnetwork_secret = \"s\"\n[flags]\nno_tun = true\n"

type harness struct {
	t      *testing.T
	dir    string
	client *localapi.Client
	cancel context.CancelFunc
	done   chan error
}

// start runs a daemon with fast cadences and fake binaries.
func start(t *testing.T, mutate func(*Options)) *harness {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	self, _ := os.Executable()
	for _, n := range []string{"easytier-core", "easytier-cli"} {
		if err := os.Symlink(self, filepath.Join(bin, n)); err != nil {
			t.Fatal(err)
		}
	}
	policy := health.DefaultPolicy()
	policy.FailThreshold, policy.OKThreshold = 1, 1
	policy.GracePeriod, policy.BackoffBase, policy.BackoffMax, policy.Jitter = time.Millisecond, 20*time.Millisecond, 50*time.Millisecond, 0
	policy.RecoveryEnabled = true
	opts := Options{
		Host: localapi.HostUser, Version: "test", StateDir: filepath.Join(dir, "state"),
		SocketPath: filepath.Join(dir, "ctl.sock"),
		Authorizer: localapi.Authorizer{OwnerUID: uint32(os.Getuid()), Private: true},
		CoreBin:    filepath.Join(bin, "easytier-core"), CLIBin: filepath.Join(bin, "easytier-cli"),
		RPCPortBase: 35888, Policy: policy, Logf: t.Logf,
		Worker: supervisor.Config{FastInterval: 30 * time.Millisecond, SlowInterval: 60 * time.Millisecond,
			StartupWindow: 200 * time.Millisecond, ConfirmWindow: 400 * time.Millisecond},
	}
	if mutate != nil {
		mutate(&opts)
	}
	ctx, cancel := context.WithCancel(context.Background())
	h := &harness{t: t, dir: dir, client: localapi.NewClient(opts.SocketPath), cancel: cancel, done: make(chan error, 1)}
	go func() { h.done <- Run(ctx, opts) }()
	t.Cleanup(func() { cancel(); <-h.done })
	h.eventually("daemon answers", func() bool {
		_, err := h.client.Do(context.Background(), localapi.Request{Type: localapi.RequestInfo})
		return err == nil
	})
	return h
}

func (h *harness) do(req localapi.Request) *localapi.Response {
	h.t.Helper()
	r, err := h.client.Do(context.Background(), req)
	if err != nil {
		h.t.Fatalf("%s: %v", req.Type, err)
	}
	return r
}

func (h *harness) create(text string) string {
	h.t.Helper()
	r := h.do(localapi.Request{Type: localapi.RequestNetworkCreate, Name: "net", Text: text})
	return r.Networks[len(r.Networks)-1].ID
}

func (h *harness) status(id string) localapi.NetworkStatus {
	for _, n := range h.do(localapi.Request{Type: localapi.RequestNetworks}).Networks {
		if n.ID == id {
			return n
		}
	}
	h.t.Fatalf("network %s missing", id)
	return localapi.NetworkStatus{}
}

func (h *harness) eventually(what string, ok func() bool) {
	h.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	h.t.Fatalf("timed out waiting for: %s", what)
}

// attach opens a session until the returned func is called.
func (h *harness) attach(watch ...string) func() {
	ctx, cancel := context.WithCancel(context.Background())
	ended := make(chan struct{})
	go func() { _ = h.client.Attach(ctx, watch, func(*localapi.Push) error { return nil }); close(ended) }()
	return func() { cancel(); <-ended }
}

// TestNetworksFollowSessions is the Follow GUI contract: a connected
// network runs only while a session is attached, and its intent survives.
func TestNetworksFollowSessions(t *testing.T) {
	h := start(t, nil)
	id := h.create(noTUN)
	h.do(localapi.Request{Type: localapi.RequestConnect, Network: id})
	if s := h.status(id); s.State != localapi.StateStopped || !s.Run {
		t.Fatalf("without a session: %+v", s)
	}
	detach := h.attach(id)
	h.eventually("connected", func() bool { return h.status(id).State == localapi.StateConnected })
	s := h.status(id)
	if s.VirtualIPv4 != "10.9.0.1/24" || s.PeerCount != 1 {
		t.Fatalf("status from the fake cli: %+v", s)
	}
	detail := h.do(localapi.Request{Type: localapi.RequestDetail, Network: id}).Detail
	if len(detail.Peers) != 2 || !detail.Peers[1].Direct || detail.Peers[1].LatencyMS != 1.5 {
		t.Fatalf("detail peers: %+v", detail.Peers)
	}
	detach()
	h.eventually("stopped after the session", func() bool { return h.status(id).State == localapi.StateStopped })
	if !h.status(id).Run {
		t.Fatal("the user's intent must survive the session")
	}
}

// TestCrashIsRecovered: a core that dies is restarted (bounded recovery).
func TestCrashIsRecovered(t *testing.T) {
	h := start(t, nil)
	id := h.create(noTUN)
	h.do(localapi.Request{Type: localapi.RequestConnect, Network: id})
	defer h.attach(id)()
	h.eventually("connected", func() bool { return h.status(id).State == localapi.StateConnected })
	started := h.status(id).StartedAt
	killCore(t, h.dir)
	h.eventually("restarted", func() bool {
		s := h.status(id)
		return s.State == localapi.StateConnected && s.StartedAt.After(started)
	})
	var recovered bool
	for _, e := range h.do(localapi.Request{Type: localapi.RequestEvents}).Events {
		recovered = recovered || e.Kind == health.EventRecoveryFinished
	}
	if !recovered {
		t.Fatal("no recovery event")
	}
}

// TestFailedApplyRollsBack: a revision whose core exits returns the
// network to the previous revision.
func TestFailedApplyRollsBack(t *testing.T) {
	h := start(t, nil)
	id := h.create(noTUN)
	h.do(localapi.Request{Type: localapi.RequestConnect, Network: id})
	defer h.attach(id)()
	h.eventually("connected", func() bool { return h.status(id).State == localapi.StateConnected })
	good := h.status(id).Applied

	doc := h.do(localapi.Request{Type: localapi.RequestConfigGet, Network: id}).Config
	h.do(localapi.Request{Type: localapi.RequestConfigSave, Network: id, Text: doc.Text + "# exit-at-start\n", Expected: doc.Revision})
	h.do(localapi.Request{Type: localapi.RequestApply, Network: id})
	h.eventually("rolled back", func() bool {
		s := h.status(id)
		return s.Applied == good && s.State == localapi.StateConnected && s.Error != ""
	})
}

// TestRejectedConfigNeverReplacesTheRunningOne: --check-config failure
// refuses the apply without touching the running core.
func TestRejectedConfigNeverReplacesTheRunningOne(t *testing.T) {
	h := start(t, nil)
	id := h.create(noTUN)
	h.do(localapi.Request{Type: localapi.RequestConnect, Network: id})
	defer h.attach(id)()
	h.eventually("connected", func() bool { return h.status(id).State == localapi.StateConnected })
	before := h.status(id)
	doc := h.do(localapi.Request{Type: localapi.RequestConfigGet, Network: id}).Config
	h.do(localapi.Request{Type: localapi.RequestConfigSave, Network: id, Text: doc.Text + "# invalid\n", Expected: doc.Revision})
	_, err := h.client.Do(context.Background(), localapi.Request{Type: localapi.RequestApply, Network: id})
	if localapi.CodeOf(err) != localapi.CodeInvalidConfig {
		t.Fatalf("apply of a rejected config: %v", err)
	}
	if after := h.status(id); after.StartedAt != before.StartedAt || after.Applied != before.Applied {
		t.Fatalf("running core was replaced: %+v", after)
	}
}

// TestTUNNeedsHelper: without privileges a TUN network is refused with a
// code the GUI can act on.
func TestTUNNeedsHelper(t *testing.T) {
	h := start(t, nil)
	id := h.create(strings.Replace(noTUN, "no_tun = true", "no_tun = false", 1))
	_, err := h.client.Do(context.Background(), localapi.Request{Type: localapi.RequestConnect, Network: id})
	if localapi.CodeOf(err) != localapi.CodeNeedsHelper {
		t.Fatalf("connect TUN network: %v", err)
	}
}

// TestSaveConflict: a save based on an old revision is refused.
func TestSaveConflict(t *testing.T) {
	h := start(t, nil)
	id := h.create(noTUN)
	doc := h.do(localapi.Request{Type: localapi.RequestConfigGet, Network: id}).Config
	h.do(localapi.Request{Type: localapi.RequestConfigSave, Network: id, Text: doc.Text + "# a\n", Expected: doc.Revision})
	_, err := h.client.Do(context.Background(), localapi.Request{Type: localapi.RequestConfigSave, Network: id, Text: doc.Text + "# b\n", Expected: doc.Revision})
	if localapi.CodeOf(err) != localapi.CodeConflict {
		t.Fatalf("stale save: %v", err)
	}
}

// TestLifelineEndsDaemon: when the GUI's pipe closes the daemon stops its
// cores and returns.
func TestLifelineEndsDaemon(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	h := start(t, func(o *Options) { o.Lifeline = r })
	id := h.create(noTUN)
	h.do(localapi.Request{Type: localapi.RequestConnect, Network: id})
	detach := h.attach(id)
	defer detach()
	h.eventually("connected", func() bool { return h.status(id).State == localapi.StateConnected })
	w.Close()
	select {
	case err := <-h.done:
		h.done <- err // for the cleanup
	case <-time.After(5 * time.Second):
		t.Fatal("daemon kept running after the lifeline closed")
	}
	if _, err := h.client.Do(context.Background(), localapi.Request{Type: localapi.RequestInfo}); !errors.Is(err, localapi.ErrNotRunning) {
		t.Fatalf("daemon still answers: %v", err)
	}
	if n := coreCount(t, h.dir); n != 0 {
		t.Fatalf("%d fake cores still running", n)
	}
}

// coreProcesses finds the fake cores started from this test's directory.
func coreProcesses(t *testing.T, dir string) []int {
	entries, _ := os.ReadDir("/proc")
	var out []int
	for _, e := range entries {
		var pid int
		if _, err := fmt.Sscan(e.Name(), &pid); err != nil {
			continue
		}
		cmd, err := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
		if err == nil && strings.HasPrefix(string(cmd), filepath.Join(dir, "bin", "easytier-core")) {
			out = append(out, pid)
		}
	}
	return out
}

func coreCount(t *testing.T, dir string) int { return len(coreProcesses(t, dir)) }

func killCore(t *testing.T, dir string) {
	pids := coreProcesses(t, dir)
	if len(pids) == 0 {
		t.Fatal("no fake core to kill")
	}
	for _, p := range pids {
		_ = syscall.Kill(p, syscall.SIGKILL)
	}
}

// TestHelperRefusesFileOptions: a root-run core never takes file paths
// from a user's document.
func TestHelperRefusesFileOptions(t *testing.T) {
	h := start(t, func(o *Options) { o.Host = localapi.HostHelper; o.Privileged = true })
	id := h.create("credential_file = \"/etc/shadow\"\n" + noTUN)
	_, err := h.client.Do(context.Background(), localapi.Request{Type: localapi.RequestConnect, Network: id})
	if localapi.CodeOf(err) != localapi.CodeInvalidConfig {
		t.Fatalf("connect with credential_file under the helper: %v", err)
	}
}

// TestIdleHelperExits: an on-demand helper with no session and nothing
// running exits; a session keeps it alive; background mode keeps it
// running without one.
func TestIdleHelperExits(t *testing.T) {
	h := start(t, func(o *Options) {
		o.Host, o.Privileged, o.IdleExit = localapi.HostHelper, true, 300*time.Millisecond
		o.OnBackground = func(bool) error { return nil }
	})
	detach := h.attach()
	time.Sleep(600 * time.Millisecond)
	if _, err := h.client.Do(context.Background(), localapi.Request{Type: localapi.RequestInfo}); err != nil {
		t.Fatalf("helper exited during a session: %v", err)
	}
	h.do(localapi.Request{Type: localapi.RequestSettingsSet, Settings: &localapi.Settings{Background: true}})
	detach()
	time.Sleep(600 * time.Millisecond)
	if _, err := h.client.Do(context.Background(), localapi.Request{Type: localapi.RequestInfo}); err != nil {
		t.Fatalf("background helper exited: %v", err)
	}
	h.do(localapi.Request{Type: localapi.RequestSettingsSet, Settings: &localapi.Settings{Background: false}})
	select {
	case err := <-h.done:
		h.done <- err
	case <-time.After(3 * time.Second):
		t.Fatal("idle helper kept running")
	}
}

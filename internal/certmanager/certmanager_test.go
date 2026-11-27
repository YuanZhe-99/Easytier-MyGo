package certmanager

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)

func mustCA(t *testing.T, opts CAOptions, now time.Time) *CA {
	t.Helper()
	ca, err := GenerateCA(opts, now)
	if err != nil {
		t.Fatal(err)
	}
	return ca
}

func mustSANs(t *testing.T, dns, ips []string) SANs {
	t.Helper()
	s, err := NormalizeSANs(dns, ips)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestGenerateCA(t *testing.T) {
	for _, kt := range []KeyType{"", KeyRSA2048} {
		ca := mustCA(t, CAOptions{CommonName: "Home CA", Organization: "Home", KeyType: kt}, t0)
		c := ca.Cert
		if !c.IsCA || !c.BasicConstraintsValid || !c.MaxPathLenZero {
			t.Errorf("%q: not a leaf-only CA: IsCA=%v BC=%v", kt, c.IsCA, c.BasicConstraintsValid)
		}
		if c.KeyUsage&(x509.KeyUsageCertSign|x509.KeyUsageCRLSign) != x509.KeyUsageCertSign|x509.KeyUsageCRLSign {
			t.Errorf("%q: key usage %v", kt, c.KeyUsage)
		}
		if len(c.SubjectKeyId) == 0 || c.Subject.CommonName != "Home CA" || c.Subject.Organization[0] != "Home" {
			t.Errorf("%q: subject %v, SKI %x", kt, c.Subject, c.SubjectKeyId)
		}
		if got := c.NotAfter.Sub(t0); got < 3650*24*time.Hour || got > 3654*24*time.Hour {
			t.Errorf("%q: default validity %v, want 10 years", kt, got)
		}
		if _, err := ParseCA(ca.CertPEM, ca.KeyPEM, t0); err != nil {
			t.Errorf("%q: own CA does not parse back: %v", kt, err)
		}
	}
	if _, err := GenerateCA(CAOptions{Years: 31}, t0); err == nil {
		t.Error("31-year CA accepted")
	}
}

func TestParseCA(t *testing.T) {
	ca := mustCA(t, CAOptions{}, t0)
	other := mustCA(t, CAOptions{}, t0)
	old := mustCA(t, CAOptions{Years: 1}, t0.AddDate(-2, 0, 0))
	leaf, err := Issue(ca, LeafRequest{SANs: mustSANs(t, []string{"a.et.net"}, nil)}, t0)
	if err != nil {
		t.Fatal(err)
	}
	// The same key in SEC 1 form must be accepted too.
	sec1, _ := x509.MarshalECPrivateKey(ca.Key.(*ecdsa.PrivateKey))
	sec1PEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: sec1})

	cases := []struct {
		name      string
		cert, key []byte
		now       time.Time
		wantErr   string
	}{
		{"valid", ca.CertPEM, ca.KeyPEM, t0, ""},
		{"sec1 key", ca.CertPEM, sec1PEM, t0, ""},
		{"key from another CA", ca.CertPEM, other.KeyPEM, t0, "does not belong"},
		{"leaf is not a CA", leaf.CertPEM, leaf.KeyPEM, t0, "not a certificate authority"},
		{"expired", old.CertPEM, old.KeyPEM, t0, "expired"},
		{"not yet valid", ca.CertPEM, ca.KeyPEM, t0.Add(-time.Hour), "not valid before"},
		{"no certificate", []byte("hello"), ca.KeyPEM, t0, "no PEM certificate"},
		{"no key", ca.CertPEM, ca.CertPEM, t0, "no PEM private key"},
	}
	for _, c := range cases {
		_, err := ParseCA(c.cert, c.key, c.now)
		switch {
		case c.wantErr == "" && err != nil:
			t.Errorf("%s: %v", c.name, err)
		case c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr) || !errors.Is(err, ErrInvalidCA)):
			t.Errorf("%s: error %v, want %q", c.name, err, c.wantErr)
		}
	}
}

func TestNormalizeSANs(t *testing.T) {
	cases := []struct {
		dns, ips []string
		want     SANs
		bad      bool
	}{
		{dns: []string{"Host.ET.net.", "host.et.net", "*.et.net"}, ips: []string{"10.144.144.2/24", "::ffff:10.0.0.1", "10.0.0.1"},
			want: SANs{DNS: []string{"*.et.net", "host.et.net"}, IPs: []string{"10.0.0.1", "10.144.144.2"}}},
		{dns: []string{"my_host.et.net", "localhost"}, want: SANs{DNS: []string{"localhost", "my_host.et.net"}}},
		{ips: []string{"fd00::1", "10.0.0.2"}, want: SANs{IPs: []string{"10.0.0.2", "fd00::1"}}},
		{dns: []string{"a.*.et.net"}, bad: true},
		{dns: []string{"*.net"}, bad: true},
		{dns: []string{"-bad.et.net"}, bad: true},
		{dns: []string{"bücher.et.net"}, bad: true},
		{dns: []string{"10.0.0.1"}, bad: true},
		{ips: []string{"host"}, bad: true},
		{ips: []string{"fe80::1%eth0"}, bad: true},
	}
	for _, c := range cases {
		got, err := NormalizeSANs(c.dns, c.ips)
		if c.bad {
			if err == nil {
				t.Errorf("%v %v accepted as %v", c.dns, c.ips, got)
			}
			continue
		}
		if err != nil || !got.Equal(c.want) {
			t.Errorf("%v %v = %v, %v; want %v", c.dns, c.ips, got, err, c.want)
		}
	}
}

func TestIssuedCertificateVerifies(t *testing.T) {
	ca := mustCA(t, CAOptions{}, t0)
	sans := mustSANs(t, []string{"laptop.et.net", "*.svc.et.net", "laptop"}, []string{"10.144.144.5", "fd00::5"})
	leaf, err := Issue(ca, LeafRequest{SANs: sans}, t0)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca.Cert)
	for _, name := range []string{"laptop.et.net", "grafana.svc.et.net", "laptop", "10.144.144.5", "fd00::5"} {
		if _, err := leaf.Cert.Verify(x509.VerifyOptions{DNSName: name, Roots: pool, CurrentTime: t0.Add(time.Hour)}); err != nil {
			t.Errorf("%s does not verify: %v", name, err)
		}
	}
	if _, err := leaf.Cert.Verify(x509.VerifyOptions{DNSName: "other.et.net", Roots: pool, CurrentTime: t0}); err == nil {
		t.Error("a name outside the SANs verifies")
	}
	if !slices.Equal(leaf.Cert.ExtKeyUsage, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}) || leaf.Cert.IsCA {
		t.Errorf("leaf usage %v, IsCA %v", leaf.Cert.ExtKeyUsage, leaf.Cert.IsCA)
	}
	if d := leaf.Cert.NotAfter.Sub(t0); d != 90*24*time.Hour {
		t.Errorf("default lifetime %v, want 90 days", d)
	}
	// The full chain is the leaf, then the CA.
	chain, err := parseCerts(leaf.FullchainPEM(ca))
	if err != nil || len(chain) != 2 || !chain[0].Equal(leaf.Cert) || !chain[1].Equal(ca.Cert) {
		t.Errorf("full chain: %d certs, %v", len(chain), err)
	}
	// A leaf never outlives its CA.
	short := mustCA(t, CAOptions{Years: 1}, t0.AddDate(0, -11, 0))
	l2, err := Issue(short, LeafRequest{SANs: sans, Days: 200}, t0)
	if err != nil || !l2.Cert.NotAfter.Equal(short.Cert.NotAfter) {
		t.Errorf("leaf of an ending CA: %v, %v (CA ends %v)", l2, err, short.Cert.NotAfter)
	}
	for _, days := range []int{-1, MaxLeafDays + 1} {
		if _, err := Issue(ca, LeafRequest{SANs: sans, Days: days}, t0); err == nil {
			t.Errorf("%d days accepted", days)
		}
	}
	if _, err := Issue(ca, LeafRequest{}, t0); err == nil {
		t.Error("certificate without names issued")
	}
}

// writeLeaf issues and writes a certificate into dir.
func writeLeaf(t *testing.T, ca *CA, sans SANs, days int, now time.Time, dir string) Files {
	t.Helper()
	f := Files{Cert: filepath.Join(dir, "cert.pem"), Fullchain: filepath.Join(dir, "fullchain.pem"), Key: filepath.Join(dir, "key.pem")}
	leaf, err := Issue(ca, LeafRequest{SANs: sans, Days: days}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteOutputs(f, leaf, ca, 0o644, 0o600); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestDecide(t *testing.T) {
	ca := mustCA(t, CAOptions{}, t0)
	other := mustCA(t, CAOptions{}, t0)
	sans := mustSANs(t, []string{"a.et.net"}, []string{"10.0.0.1"})
	window := 30 * 24 * time.Hour

	dir := t.TempDir()
	good := writeLeaf(t, ca, sans, 90, t0, filepath.Join(dir, "good"))
	foreign := writeLeaf(t, other, sans, 90, t0, filepath.Join(dir, "foreign"))
	mismatch := writeLeaf(t, ca, sans, 90, t0, filepath.Join(dir, "mismatch"))
	otherKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, _ := x509.MarshalPKCS8PrivateKey(otherKey)
	os.WriteFile(mismatch.Key, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600)
	corrupt := writeLeaf(t, ca, sans, 90, t0, filepath.Join(dir, "corrupt"))
	os.WriteFile(corrupt.Fullchain, []byte("-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n"), 0o644)
	missing := writeLeaf(t, ca, sans, 90, t0, filepath.Join(dir, "missing"))
	os.Remove(missing.Key)

	cases := []struct {
		name  string
		files Files
		want  SANs
		now   time.Time
		force bool
		out   Reason
	}{
		{"current", good, sans, t0.Add(24 * time.Hour), false, ReasonNone},
		{"forced", good, sans, t0, true, ReasonForced},
		{"missing key", missing, sans, t0, false, ReasonMissing},
		{"no files", Files{Cert: filepath.Join(dir, "x", "c"), Key: filepath.Join(dir, "x", "k")}, sans, t0, false, ReasonMissing},
		{"corrupt full chain", corrupt, sans, t0, false, ReasonCorrupt},
		{"key does not match", mismatch, sans, t0, false, ReasonCorrupt},
		{"other CA", foreign, sans, t0, false, ReasonCAChanged},
		{"names changed", good, mustSANs(t, []string{"a.et.net", "b.et.net"}, []string{"10.0.0.1"}), t0, false, ReasonSANChanged},
		{"address changed", good, mustSANs(t, []string{"a.et.net"}, []string{"10.0.0.2"}), t0, false, ReasonSANChanged},
		{"inside the window", good, sans, t0.Add(61 * 24 * time.Hour), false, ReasonExpiring},
		{"just before the window", good, sans, t0.Add(59 * 24 * time.Hour), false, ReasonNone},
		{"expired", good, sans, t0.Add(100 * 24 * time.Hour), false, ReasonExpiring},
	}
	for _, c := range cases {
		if got := Decide(Inspect(c.files), ca, c.want, window, c.force, c.now); got != c.out {
			t.Errorf("%s: %s, want %s", c.name, got, c.out)
		}
	}
	// A third of a long lifetime counts when it is longer than the window.
	long := writeLeaf(t, ca, sans, 390, t0, filepath.Join(dir, "long"))
	if got := Decide(Inspect(long), ca, sans, window, false, t0.Add(300*24*time.Hour)); got != ReasonExpiring {
		t.Errorf("390-day certificate with 90 days left: %s, want expiring", got)
	}
}

func TestWriteOutputsIsAtomic(t *testing.T) {
	ca := mustCA(t, CAOptions{}, t0)
	sans := mustSANs(t, []string{"a.et.net"}, nil)
	dir := filepath.Join(t.TempDir(), "new", "dir")
	f := Files{Cert: filepath.Join(dir, "cert.pem"), Fullchain: filepath.Join(dir, "fullchain.pem"), Key: filepath.Join(dir, "key.pem")}
	leaf, _ := Issue(ca, LeafRequest{SANs: sans}, t0)
	if err := WriteOutputs(f, leaf, ca, 0o640, 0o600); err != nil {
		t.Fatal(err)
	}
	d := Inspect(f)
	if d.Leaf == nil || !d.Leaf.Equal(leaf.Cert) {
		t.Fatalf("written files do not read back: %v", d.Problem)
	}
	if runtime.GOOS != "windows" {
		for path, want := range map[string]os.FileMode{f.Cert: 0o640, f.Fullchain: 0o640, f.Key: 0o600} {
			if fi, _ := os.Stat(path); fi.Mode().Perm() != want {
				t.Errorf("%s mode %v, want %v", filepath.Base(path), fi.Mode().Perm(), want)
			}
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 3 {
		t.Errorf("directory holds %d entries, want 3 (no temporary files)", len(entries))
	}
	// A write that cannot complete leaves the old files untouched.
	blocked := f
	blocked.Fullchain = filepath.Join(dir, "cert.pem", "sub") // a file used as a directory
	leaf2, _ := Issue(ca, LeafRequest{SANs: sans}, t0)
	if err := WriteOutputs(blocked, leaf2, ca, 0o644, 0o600); err == nil {
		t.Fatal("write into a file succeeded")
	}
	if d := Inspect(f); d.Leaf == nil || !d.Leaf.Equal(leaf.Cert) {
		t.Error("failed write changed the old certificate")
	}
	entries, _ = os.ReadDir(dir)
	if len(entries) != 3 {
		t.Errorf("failed write left %d entries", len(entries))
	}
}

// TestWrittenFilesServeTLS: the files load as a web server's key pair
// (Caddy and Go servers use crypto/tls) and a client that trusts only the
// CA accepts the handshake for a wildcard name and an IP.
func TestWrittenFilesServeTLS(t *testing.T) {
	ca := mustCA(t, CAOptions{}, t0)
	f := writeLeaf(t, ca, mustSANs(t, []string{"*.svc.et.net"}, []string{"10.144.144.5"}), 90, time.Now(), t.TempDir())
	pair, err := tls.LoadX509KeyPair(f.Fullchain, f.Key)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca.Cert)
	for _, name := range []string{"grafana.svc.et.net", "10.144.144.5"} {
		c1, c2 := net.Pipe()
		srv := tls.Server(c1, &tls.Config{Certificates: []tls.Certificate{pair}})
		go func() { _ = srv.Handshake(); c1.Close() }()
		cli := tls.Client(c2, &tls.Config{RootCAs: pool, ServerName: name})
		if err := cli.Handshake(); err != nil {
			t.Errorf("%s: %v", name, err)
		}
		c2.Close() // no close_notify: the synchronous pipe would block on it
	}
}

// sh returns a shell command line per platform.
func sh(unix, windows string) string {
	if runtime.GOOS == "windows" {
		return windows
	}
	return unix
}

func TestRunHook(t *testing.T) {
	dir := t.TempDir()
	vars := HookVars{CertPath: filepath.Join(dir, "cert.pem"), KeyPath: "k", FullchainPath: "f", OutDir: dir}

	ok := RunHook(context.Background(), Hook{Enabled: true, Command: sh("echo cert={{CERT_PATH}} env=$EASYTIER_MYGO_KEY_PATH", "echo cert={{CERT_PATH}} env=%EASYTIER_MYGO_KEY_PATH%")}, vars)
	if !ok.OK || ok.ExitCode != 0 || !strings.Contains(ok.Output, "cert="+vars.CertPath) || !strings.Contains(ok.Output, "env=k") {
		t.Errorf("echo hook: %+v", ok)
	}

	failed := RunHook(context.Background(), Hook{Enabled: true, Command: sh("echo nope >&2; exit 3", "echo nope 1>&2 & exit /b 3")}, vars)
	if failed.OK || failed.ExitCode != 3 || !strings.Contains(failed.Output, "nope") || failed.PermissionDenied {
		t.Errorf("failing hook: %+v", failed)
	}

	denied := RunHook(context.Background(), Hook{Enabled: true, Command: sh("echo 'permission denied while trying to connect to the Docker daemon socket at unix:///var/run/docker.sock' >&2; exit 1",
		"echo Access is denied. 1>&2 & exit /b 5")}, vars)
	if denied.OK || !denied.PermissionDenied {
		t.Errorf("permission failure not recognized: %+v", denied)
	}

	start := time.Now()
	slow := RunHook(context.Background(), Hook{Enabled: true, TimeoutSeconds: 1, Command: sh("sleep 30", "ping -n 30 127.0.0.1 >nul")}, vars)
	if took := time.Since(start); took > 10*time.Second {
		t.Errorf("timed-out hook returned after %v", took)
	}
	if slow.OK || !slow.TimedOut {
		t.Errorf("slow hook: %+v", slow)
	}

	args := RunHook(context.Background(), Hook{Enabled: true, Args: []string{"no-such-program-easytier-mygo"}}, vars)
	if args.OK || args.Error == "" {
		t.Errorf("missing program: %+v", args)
	}
}

func TestHookOutputIsBounded(t *testing.T) {
	b := &tailBuffer{max: 10}
	b.Write([]byte("0123456789abcdef"))
	if got := b.String(); got != "…6789abcdef" {
		t.Errorf("tail %q", got)
	}
}

func TestDetectLinuxFlavor(t *testing.T) {
	none := func(string) bool { return false }
	cases := []struct {
		osRelease string
		has       func(string) bool
		want      string
	}{
		{"ID=debian\nVERSION_ID=\"13\"", none, "debian"},
		{"ID=ubuntu\nID_LIKE=debian", none, "debian"},
		{"ID=alpine", none, "debian"},
		{"ID=\"rocky\"\nID_LIKE=\"rhel centos fedora\"", none, "redhat"},
		{"ID=fedora", none, "redhat"},
		{"ID=opensuse-tumbleweed\nID_LIKE=\"opensuse suse\"", none, "suse"},
		{"ID=arch", none, "arch"},
		{"ID=cachyos\nID_LIKE=arch", none, "arch"},
		{"ID=mystery", func(p string) bool { return p == "update-ca-trust" }, "redhat"},
		{"", func(p string) bool { return p == "trust" }, "arch"},
		{"ID=mystery", none, ""},
	}
	for _, c := range cases {
		f, ok := detectLinuxFlavor(c.osRelease, c.has)
		if f.id != c.want || ok != (c.want != "") {
			t.Errorf("%q: %q (%v), want %q", c.osRelease, f.id, ok, c.want)
		}
	}
	ca := mustCA(t, CAOptions{}, t0)
	if cmd := flavorDebian.installCommand(ca.Cert, "/var/lib/my ca.crt"); !strings.Contains(cmd, "'/var/lib/my ca.crt'") ||
		!strings.Contains(cmd, "/usr/local/share/ca-certificates/easytier-mygo-ca-") || !strings.HasSuffix(cmd, "update-ca-certificates") {
		t.Errorf("debian command %q", cmd)
	}
	bundle := append(mustCA(t, CAOptions{}, t0).CertPEM, ca.CertPEM...)
	if !bundleHas(bundle, ca.Cert) || bundleHas(bundle[:len(bundle)-len(ca.CertPEM)], ca.Cert) {
		t.Error("bundle lookup")
	}
}

func TestTrustNeedsPrivilege(t *testing.T) {
	if Elevated() || backend() == nil {
		t.Skip("running elevated, or no trust store backend")
	}
	ca := mustCA(t, CAOptions{}, t0)
	var pe *PrivilegeError
	if err := InstallTrust(ca.Cert, "/tmp/ca.crt"); !errors.As(err, &pe) || pe.Command == "" {
		t.Errorf("unprivileged install: %v", err)
	}
	if st := Trust(ca.Cert, "/tmp/ca.crt"); st.Privileged || st.Installed || st.InstallCommand == "" {
		t.Errorf("trust status of a fresh CA: %+v", st)
	}
}

// fakeNames is a network-name source the test changes.
type fakeNames struct {
	mu    sync.Mutex
	names map[string]NetworkNames
}

func (f *fakeNames) get(id string) (NetworkNames, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n, ok := f.names[id]
	return n, ok
}

func (f *fakeNames) set(id string, n *NetworkNames) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if n == nil {
		delete(f.names, id)
	} else {
		f.names[id] = *n
	}
}

func TestManagerLifecycle(t *testing.T) {
	dir := t.TempDir()
	now := t0
	names := &fakeNames{names: map[string]NetworkNames{}}
	m, err := New(Options{Dir: dir, Names: names.get, Now: func() time.Time { return now }, Logf: t.Logf})
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "srv")
	marker := filepath.Join(out, "hook.log")
	job, err := m.SetJob(Job{Network: "net1", DNSNames: []string{"*.home.et.net"}, OutDir: out,
		Hook: Hook{Enabled: true, Command: sh(`echo run >> "{{OUT_DIR}}/hook.log"`, `echo run>> "{{OUT_DIR}}\hook.log"`)}})
	if err != nil {
		t.Fatal(err)
	}
	hookRuns := func() int {
		data, _ := os.ReadFile(marker)
		return strings.Count(string(data), "run")
	}
	ctx := context.Background()
	check := func(force bool) JobStatus {
		t.Helper()
		if err := m.Check(ctx, job.ID, force); err != nil {
			t.Fatal(err)
		}
		return m.Status().Jobs[0]
	}

	// No CA, names unknown: the job waits.
	if js := check(false); js.Phase != PhaseWaiting || js.Cert != nil {
		t.Fatalf("before the network runs: %s, cert %v", js.Phase, js.Cert)
	}
	names.set("net1", &NetworkNames{DNS: []string{"laptop.et.net", "laptop"}, IPs: []string{"10.144.144.5/24"}})
	if js := check(false); js.Phase != PhaseFailed || !strings.Contains(js.LastError, "no certificate authority") {
		t.Fatalf("without a CA: %s %q", js.Phase, js.LastError)
	}
	if err := m.GenerateCA(CAOptions{}, false); err != nil {
		t.Fatal(err)
	}
	if err := m.GenerateCA(CAOptions{}, false); !errors.Is(err, ErrCAExists) {
		t.Fatalf("second CA without replace: %v", err)
	}

	js := check(false)
	if js.Phase != PhaseOK || js.LastReason != ReasonMissing || hookRuns() != 1 || js.Hook == nil || !js.Hook.OK {
		t.Fatalf("first issue: %s %s hook runs %d %+v", js.Phase, js.LastReason, hookRuns(), js.Hook)
	}
	want := SANs{DNS: []string{"*.home.et.net", "laptop", "laptop.et.net"}, IPs: []string{"10.144.144.5"}}
	if !js.Cert.SANs.Equal(want) {
		t.Fatalf("certificate names %v, want %v", js.Cert.SANs, want)
	}

	// Nothing changed: no write, no hook.
	issued := js.LastIssued
	now = now.Add(time.Hour)
	if js := check(false); js.LastIssued != issued || hookRuns() != 1 {
		t.Fatalf("unchanged job re-issued (%v) or hook ran (%d)", js.LastIssued, hookRuns())
	}

	// The network stops: its last names are kept, nothing is re-issued.
	names.set("net1", nil)
	if js := check(false); js.LastIssued != issued || !js.Cert.SANs.Equal(want) {
		t.Fatal("a stopped network shrank the certificate")
	}

	// The virtual address changes: the watch notices, the job re-issues.
	names.set("net1", &NetworkNames{DNS: []string{"laptop.et.net", "laptop"}, IPs: []string{"10.144.144.9"}})
	if ids := m.namesChanged(); !slices.Equal(ids, []string{job.ID}) {
		t.Fatalf("names watch: %v", ids)
	}
	if js := check(false); js.LastReason != ReasonSANChanged || hookRuns() != 2 {
		t.Fatalf("address change: %s, hook runs %d", js.LastReason, hookRuns())
	}
	if ids := m.namesChanged(); len(ids) != 0 {
		t.Fatalf("names watch after re-issue: %v", ids)
	}

	// A new CA re-issues.
	if err := m.GenerateCA(CAOptions{CommonName: "Second"}, true); err != nil {
		t.Fatal(err)
	}
	if js := check(false); js.LastReason != ReasonCAChanged || hookRuns() != 3 {
		t.Fatalf("CA change: %s, hook runs %d", js.LastReason, hookRuns())
	}
	if _, err := os.Stat(filepath.Join(dir, "ca.crt.old")); err != nil {
		t.Errorf("replaced CA not kept: %v", err)
	}

	// Close to expiry it renews.
	now = now.Add(70 * 24 * time.Hour)
	if js := check(false); js.LastReason != ReasonExpiring || hookRuns() != 4 {
		t.Fatalf("expiry: %s, hook runs %d", js.LastReason, hookRuns())
	}

	// A failing hook is reported but the certificate is still written.
	job.Hook.Command = sh("echo broken >&2; exit 7", "echo broken 1>&2 & exit /b 7")
	if _, err := m.SetJob(job); err != nil {
		t.Fatal(err)
	}
	before := m.Status().Jobs[0].Cert.Serial
	js = check(true)
	if js.LastReason != ReasonForced || js.LastError != "" || !js.HookWarning || js.Hook.ExitCode != 7 || js.Cert.Serial == before || js.Phase != PhaseOK {
		t.Fatalf("failing hook: %+v", js)
	}

	// The state survives a restart.
	m2, err := New(Options{Dir: dir, Names: names.get, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	st := m2.Status()
	if st.CA == nil || !st.CA.Usable || !strings.Contains(st.CA.Subject, "Second") || len(st.Jobs) != 1 || st.Jobs[0].LastReason != ReasonForced {
		t.Fatalf("after restart: %+v", st)
	}
	exp, err := m2.ExportCA(false)
	if err != nil || exp.KeyPEM != "" || !strings.Contains(exp.CertPEM, "BEGIN CERTIFICATE") {
		t.Fatalf("export without key: %v %+v", err, exp)
	}
	if exp, _ := m2.ExportCA(true); !strings.Contains(exp.KeyPEM, "PRIVATE KEY") {
		t.Fatal("export with key has no key")
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(filepath.Join(dir, "ca.key")); fi.Mode().Perm() != 0o600 {
			t.Errorf("CA key mode %v", fi.Mode().Perm())
		}
	}
	if err := m2.RemoveJob(job.ID); err != nil || len(m2.Status().Jobs) != 0 {
		t.Fatalf("remove: %v", err)
	}
}

func TestImportCA(t *testing.T) {
	m, err := New(Options{Dir: t.TempDir(), Now: func() time.Time { return t0 }})
	if err != nil {
		t.Fatal(err)
	}
	ca := mustCA(t, CAOptions{CommonName: "Imported"}, t0.Add(-time.Hour))
	other := mustCA(t, CAOptions{}, t0)
	if err := m.ImportCA(ca.CertPEM, other.KeyPEM, false); !errors.Is(err, ErrInvalidCA) {
		t.Fatalf("mismatched import: %v", err)
	}
	if m.Status().CA != nil {
		t.Fatal("a refused import was stored")
	}
	if err := m.ImportCA(ca.CertPEM, ca.KeyPEM, false); err != nil {
		t.Fatal(err)
	}
	if st := m.Status(); st.CA == nil || st.CA.Fingerprint != Fingerprint(ca.Cert) {
		t.Fatalf("imported CA: %+v", st.CA)
	}
}

func TestJobValidation(t *testing.T) {
	cases := []struct {
		name string
		job  Job
		bad  string
	}{
		{"names only", Job{DNSNames: []string{"a.et.net"}}, ""},
		{"network only", Job{Network: "n"}, ""},
		{"nothing", Job{}, "needs a network"},
		{"relative dir", Job{Network: "n", OutDir: "certs"}, "absolute"},
		{"path in file name", Job{Network: "n", KeyFile: "../key.pem"}, "plain file name"},
		{"same file twice", Job{Network: "n", CertFile: "x.pem", KeyFile: "x.pem"}, "both named"},
		{"skip key", Job{Network: "n", KeyFile: SkipFile}, "plain file name"},
		{"skip both certs", Job{Network: "n", CertFile: SkipFile, FullchainFile: SkipFile}, "at least"},
		{"mode", Job{Network: "n", KeyMode: "rw"}, "permission"},
		{"validity", Job{Network: "n", ValidityDays: 398}, "397"},
		{"renew window", Job{Network: "n", ValidityDays: 30, RenewBeforeDays: 30}, "shorter"},
		{"hook without command", Job{Network: "n", Hook: Hook{Enabled: true}}, "no command"},
	}
	for _, c := range cases {
		j := c.job
		err := j.validate()
		switch {
		case c.bad == "" && err != nil:
			t.Errorf("%s: %v", c.name, err)
		case c.bad != "" && (err == nil || !strings.Contains(err.Error(), c.bad)):
			t.Errorf("%s: %v, want %q", c.name, err, c.bad)
		}
	}
}

package certmanager

import (
	"context"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/fsutil"
)

// Cadences of the background loop.
const (
	DefaultCheckInterval = 6 * time.Hour
	DefaultWatchInterval = time.Minute
	trustCacheTTL        = 30 * time.Second
)

// NetworkNames are the names a network's running core reports for this
// computer.
type NetworkNames struct {
	DNS []string
	IPs []string
}

// Options configure a Manager.
type Options struct {
	// Dir holds the CA, jobs.json, state.json and the default output
	// directories (<Dir>/out/<job>). Created with mode 0700.
	Dir string
	// Restricted is the privileged helper's policy: a job writes to a
	// folder of its own or runs a command only while an administrator's
	// approval covers exactly those settings (approval.go).
	Restricted bool
	// Names returns a network's current names; ok=false while they are not
	// known (the network is not running). Nil: no network names.
	Names func(network string) (NetworkNames, bool)
	// CheckInterval and WatchInterval: full checks (0: 6 h) and checks of
	// network names only (0: 1 min).
	CheckInterval, WatchInterval time.Duration
	Logf                         func(string, ...any)
	// Now is the clock (tests); nil: time.Now.
	Now func() time.Time
}

// Errors the local API maps to codes.
var (
	ErrNoCA        = errors.New("no certificate authority yet; generate or import one first")
	ErrCAExists    = errors.New("a certificate authority already exists; replacing it re-issues every certificate")
	ErrJobNotFound = errors.New("certificate job not found")
)

// JobState is what the manager remembers about a job between runs.
type JobState struct {
	LastCheck  time.Time   `json:"last_check,omitzero"`
	LastIssued time.Time   `json:"last_issued,omitzero"`
	LastReason Reason      `json:"last_reason,omitempty"`
	LastError  string      `json:"last_error,omitempty"`
	Hook       *HookResult `json:"hook,omitempty"`
	// Network names last reported by the core; used while it is stopped
	// so a stopped network does not shrink the certificate.
	NetworkDNS []string `json:"network_dns,omitempty"`
	NetworkIPs []string `json:"network_ips,omitempty"`
	NamesKnown bool     `json:"names_known,omitempty"`
}

// Manager keeps the CA and the jobs' certificates. Safe for concurrent
// use; checks run one at a time.
type Manager struct {
	opts Options

	mu     sync.Mutex
	ca     *CA
	caErr  error // a stored CA that cannot be used (expired, damaged)
	caCert *CAStatus
	jobs   []Job
	state  map[string]*JobState
	trust  TrustStatus
	trustT time.Time
	trustF string

	runMu sync.Mutex
}

// New opens (and creates) the manager's directory.
// Returns: an error only when the directory or its JSON files cannot be
// read; a damaged CA is reported in Status instead.
func New(opts Options) (*Manager, error) {
	if opts.Logf == nil {
		opts.Logf = func(string, ...any) {}
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.CheckInterval <= 0 {
		opts.CheckInterval = DefaultCheckInterval
	}
	if opts.WatchInterval <= 0 {
		opts.WatchInterval = DefaultWatchInterval
	}
	if err := os.MkdirAll(opts.Dir, 0o700); err != nil {
		return nil, err
	}
	m := &Manager{opts: opts, state: map[string]*JobState{}}
	if _, err := fsutil.ReadJSON(m.path("jobs.json"), &m.jobs); err != nil {
		return nil, fmt.Errorf("certificate jobs: %w", err)
	}
	if _, err := fsutil.ReadJSON(m.path("state.json"), &m.state); err != nil {
		opts.Logf("certificates: state: %v", err) // only history; start fresh
		m.state = map[string]*JobState{}
	}
	m.loadCA()
	return m, nil
}

func (m *Manager) path(name string) string { return filepath.Join(m.opts.Dir, name) }

// CAPath is where the CA certificate is stored (for manual trust commands).
func (m *Manager) CAPath() string { return m.path("ca.crt") }

// loadCA reads the stored CA; a missing one is not an error.
func (m *Manager) loadCA() {
	m.ca, m.caErr, m.caCert = nil, nil, nil
	certPEM, err := os.ReadFile(m.path("ca.crt"))
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		m.caErr = err
		return
	}
	if c, err := firstCert(certPEM); err == nil {
		m.caCert = caStatusOf(c, m.CAPath())
	}
	keyPEM, err := os.ReadFile(m.path("ca.key"))
	if err != nil {
		m.caErr = fmt.Errorf("CA key: %w", err)
		return
	}
	ca, err := ParseCA(certPEM, keyPEM, m.opts.Now())
	if err != nil {
		m.caErr = err
		return
	}
	m.ca = ca
	m.caCert = caStatusOf(ca.Cert, m.CAPath())
}

// GenerateCA creates a new CA and stores it.
// Inputs: replace must be set when a CA exists (the old one is kept as
// ca.crt.old / ca.key.old).
// Side effects: writes ca.crt (0644) and ca.key (0600); every job is
// re-issued by the next check.
func (m *Manager) GenerateCA(opts CAOptions, replace bool) error {
	ca, err := GenerateCA(opts, m.opts.Now())
	if err != nil {
		return err
	}
	return m.storeCA(ca, replace)
}

// ImportCA stores an existing CA after ParseCA's checks.
func (m *Manager) ImportCA(certPEM, keyPEM []byte, replace bool) error {
	ca, err := ParseCA(certPEM, keyPEM, m.opts.Now())
	if err != nil {
		return err
	}
	return m.storeCA(ca, replace)
}

func (m *Manager) storeCA(ca *CA, replace bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, statErr := os.Stat(m.path("ca.crt"))
	exists := statErr == nil
	if exists && !replace {
		return ErrCAExists
	}
	if exists {
		for _, n := range []string{"ca.crt", "ca.key"} {
			if data, err := os.ReadFile(m.path(n)); err == nil {
				if err := fsutil.WriteFileAtomic(m.path(n+".old"), data, 0o600); err != nil {
					return err
				}
			}
		}
	}
	// Key first: a crash in between leaves a key without its certificate,
	// which loads as "no CA" instead of a CA paired with the wrong key.
	if err := fsutil.WriteFileAtomic(m.path("ca.key"), ca.KeyPEM, 0o600); err != nil {
		return err
	}
	if err := fsutil.WriteFileAtomic(m.path("ca.crt"), ca.CertPEM, 0o644); err != nil {
		return err
	}
	m.ca, m.caErr, m.caCert = ca, nil, caStatusOf(ca.Cert, m.CAPath())
	m.trustT = time.Time{}
	m.opts.Logf("certificates: new CA %s (%s)", ca.Cert.Subject.CommonName, Fingerprint(ca.Cert))
	return nil
}

// Export is a CA handed to a client.
type Export struct {
	CertPEM     string `json:"cert_pem"`
	KeyPEM      string `json:"key_pem,omitempty"` // only when asked for
	Fingerprint string `json:"fingerprint"`
}

// ExportCA returns the CA certificate and, with withKey, its private key
// (to run the same CA on another computer). The key is never logged.
func (m *Manager) ExportCA(withKey bool) (Export, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ca == nil {
		if m.caErr != nil {
			return Export{}, m.caErr
		}
		return Export{}, ErrNoCA
	}
	e := Export{CertPEM: string(m.ca.CertPEM), Fingerprint: Fingerprint(m.ca.Cert)}
	if withKey {
		e.KeyPEM = string(m.ca.KeyPEM)
	}
	return e, nil
}

// SetJob adds or replaces a job (matched by ID).
// Returns: the stored job (its ID filled in).
// Side effects: writes jobs.json. The certificate is checked by the next
// Check.
// Notes: in the privileged helper a job with its own folder or a command
// is refused with ErrNeedsApproval unless an administrator approved
// exactly those settings (Approve).
func (m *Manager) SetJob(j Job) (Job, error) {
	if err := j.validate(); err != nil {
		return Job{}, err
	}
	if m.opts.Restricted && NeedsApproval(j) {
		if err := m.approved(j); err != nil {
			return Job{}, err
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	jobs := slices.Clone(m.jobs)
	if i := slices.IndexFunc(jobs, func(o Job) bool { return o.ID == j.ID }); i >= 0 {
		jobs[i] = j
	} else {
		jobs = append(jobs, j)
	}
	if err := fsutil.WriteJSON(m.path("jobs.json"), jobs); err != nil {
		return Job{}, err
	}
	m.jobs = jobs
	return j, nil
}

// RemoveJob forgets a job. Its files stay where they are.
func (m *Manager) RemoveJob(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	i := slices.IndexFunc(m.jobs, func(o Job) bool { return o.ID == id })
	if i < 0 {
		return ErrJobNotFound
	}
	jobs := slices.Delete(slices.Clone(m.jobs), i, i+1)
	if err := fsutil.WriteJSON(m.path("jobs.json"), jobs); err != nil {
		return err
	}
	m.jobs = jobs
	delete(m.state, id)
	m.saveStateLocked()
	if m.opts.Restricted {
		m.dropApproval(id) // a job of the same ID needs approving again
	}
	return nil
}

// Run checks every job now, then every CheckInterval, and re-checks a job
// as soon as its network's names change (polled every WatchInterval).
// Returns when ctx ends; a running hook is stopped with it.
func (m *Manager) Run(ctx context.Context) {
	m.CheckAll(ctx, false)
	full := time.NewTicker(m.opts.CheckInterval)
	defer full.Stop()
	watch := time.NewTicker(m.opts.WatchInterval)
	defer watch.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-full.C:
			m.CheckAll(ctx, false)
		case <-watch.C:
			for _, id := range m.namesChanged() {
				_ = m.Check(ctx, id, false)
			}
		}
	}
}

// CheckAll checks every job (force: issue new certificates regardless).
func (m *Manager) CheckAll(ctx context.Context, force bool) {
	m.mu.Lock()
	jobs := slices.Clone(m.jobs)
	m.mu.Unlock()
	for _, j := range jobs {
		if ctx.Err() != nil {
			return
		}
		m.runMu.Lock()
		m.checkJob(ctx, j, force)
		m.runMu.Unlock()
	}
}

// Check checks one job now.
// Returns: ErrJobNotFound for an unknown ID; otherwise nil (the outcome
// is in Status).
func (m *Manager) Check(ctx context.Context, id string, force bool) error {
	m.mu.Lock()
	i := slices.IndexFunc(m.jobs, func(o Job) bool { return o.ID == id })
	var j Job
	if i >= 0 {
		j = m.jobs[i]
	}
	m.mu.Unlock()
	if i < 0 {
		return ErrJobNotFound
	}
	m.runMu.Lock()
	defer m.runMu.Unlock()
	m.checkJob(ctx, j, force)
	return nil
}

// namesChanged lists jobs whose network now reports other names than the
// ones last seen.
func (m *Manager) namesChanged() []string {
	if m.opts.Names == nil {
		return nil
	}
	m.mu.Lock()
	jobs := slices.Clone(m.jobs)
	m.mu.Unlock()
	var out []string
	for _, j := range jobs {
		if j.Network == "" {
			continue
		}
		nn, ok := m.opts.Names(j.Network)
		if !ok {
			continue
		}
		cur, err := NormalizeSANs(nn.DNS, nn.IPs)
		if err != nil {
			continue
		}
		m.mu.Lock()
		st := m.state[j.ID]
		changed := st == nil || !st.NamesKnown || !cur.Equal(SANs{DNS: st.NetworkDNS, IPs: st.NetworkIPs})
		m.mu.Unlock()
		if changed {
			out = append(out, j.ID)
		}
	}
	return out
}

// wantedNames combines a job's own names with its network's.
// Returns: ok=false while the network's names were never known (the job
// waits instead of issuing a certificate that lacks them).
// Inputs: nn and ok come from networkNames (called without the lock); st
// records them and must be guarded by the caller.
func wantedNames(j Job, st *JobState, nn NetworkNames, ok bool) (SANs, bool, error) {
	dns, ips := slices.Clone(j.DNSNames), slices.Clone(j.IPs)
	if j.Network != "" {
		if ok {
			norm, err := NormalizeSANs(nn.DNS, nn.IPs)
			if err != nil {
				return SANs{}, false, fmt.Errorf("network names: %v", err)
			}
			st.NetworkDNS, st.NetworkIPs, st.NamesKnown = norm.DNS, norm.IPs, true
		}
		if !st.NamesKnown {
			return SANs{}, false, nil
		}
		dns = append(dns, st.NetworkDNS...)
		ips = append(ips, st.NetworkIPs...)
	}
	s, err := NormalizeSANs(dns, ips)
	if err == nil && s.Empty() {
		err = errors.New("the certificate has no names: the network reports no hostname or address, and none are configured")
	}
	return s, true, err
}

// networkNames asks the daemon for a job's network names.
func (m *Manager) networkNames(j Job) (NetworkNames, bool) {
	if j.Network == "" || m.opts.Names == nil {
		return NetworkNames{}, false
	}
	return m.opts.Names(j.Network)
}

// checkJob decides, issues, writes and runs the hook for one job; the
// outcome is recorded in its state. Callers hold runMu.
func (m *Manager) checkJob(ctx context.Context, j Job, force bool) {
	now := m.opts.Now()
	m.mu.Lock()
	st := m.state[j.ID]
	if st == nil {
		st = &JobState{}
		m.state[j.ID] = st
	}
	ca, caErr := m.ca, m.caErr
	m.mu.Unlock()

	record := func(fn func()) {
		m.mu.Lock()
		fn()
		st.LastCheck = now
		m.saveStateLocked()
		m.mu.Unlock()
	}
	fail := func(err error) {
		m.opts.Logf("certificates: job %s: %v", j.ID, err)
		record(func() { st.LastError = err.Error() })
	}

	nn, nnOK := m.networkNames(j)
	m.mu.Lock()
	want, known, err := wantedNames(j, st, nn, nnOK)
	m.mu.Unlock()
	if err != nil {
		fail(err)
		return
	}
	if !known && !force {
		record(func() { st.LastError = "" })
		return // waiting for the network's names; Status says so
	}
	if !known {
		if want, err = NormalizeSANs(j.DNSNames, j.IPs); err == nil && want.Empty() {
			err = errors.New("the network's names are not known yet and none are configured")
		}
		if err != nil {
			fail(err)
			return
		}
	}
	if ca == nil {
		if caErr == nil {
			caErr = ErrNoCA
		}
		fail(caErr)
		return
	}
	// The helper writes to a chosen folder and runs a command only while
	// the administrator's approval still matches (it may have been
	// replaced, or the folder redirected, since the job was saved).
	if m.opts.Restricted && NeedsApproval(j) {
		if err := m.approved(j); err != nil {
			fail(err)
			return
		}
	}
	files, dir := j.files(m.path("out"))
	reason := Decide(Inspect(files), ca, want, j.renewBefore(), force, now)
	if reason == ReasonNone {
		record(func() { st.LastError = "" })
		return
	}
	leaf, err := Issue(ca, LeafRequest{SANs: want, Days: j.days(), KeyType: j.KeyType}, now)
	if err != nil {
		fail(err)
		return
	}
	certMode, keyMode := j.modes()
	if err := WriteOutputs(files, leaf, ca, certMode, keyMode); err != nil {
		fail(fmt.Errorf("write certificate: %w", err))
		return
	}
	m.opts.Logf("certificates: job %s: issued (%s) for %s, valid until %s", j.ID, reason,
		strings.Join(want.All(), ", "), leaf.Cert.NotAfter.UTC().Format(time.DateOnly))
	record(func() {
		st.LastIssued, st.LastReason, st.LastError, st.Hook = now, reason, "", nil
	})
	if !j.Hook.Enabled {
		return
	}
	res := RunHook(ctx, j.Hook, HookVars{CertPath: files.Cert, KeyPath: files.Key, FullchainPath: files.Fullchain, OutDir: dir})
	if res.OK {
		m.opts.Logf("certificates: job %s: hook finished in %d ms", j.ID, res.DurationMS)
	} else {
		m.opts.Logf("certificates: job %s: hook failed: %s (permission problem: %v)\n%s", j.ID, res.Error, res.PermissionDenied, lastLines(res.Output, 20))
	}
	record(func() { st.Hook = &res })
}

func (m *Manager) saveStateLocked() {
	if err := fsutil.WriteJSON(m.path("state.json"), m.state); err != nil {
		m.opts.Logf("certificates: save state: %v", err)
	}
}

// CAStatus describes the stored CA.
type CAStatus struct {
	Name        string    `json:"name"` // the common name, as devices list it
	Subject     string    `json:"subject"`
	Fingerprint string    `json:"fingerprint"` // SHA-256
	NotBefore   time.Time `json:"not_before"`
	NotAfter    time.Time `json:"not_after"`
	KeyType     string    `json:"key_type"`
	Path        string    `json:"path"`
	// Usable is false when the CA cannot sign (expired, key missing);
	// Problem says why.
	Usable  bool   `json:"usable"`
	Problem string `json:"problem,omitempty"`
}

// LeafStatus describes a job's certificate on disk.
type LeafStatus struct {
	SANs        SANs      `json:"sans"`
	NotBefore   time.Time `json:"not_before"`
	NotAfter    time.Time `json:"not_after"`
	Serial      string    `json:"serial"`
	Fingerprint string    `json:"fingerprint"`
}

// JobPhase summarizes a job for display.
type JobPhase string

const (
	PhaseOK      JobPhase = "ok"      // current certificate on disk
	PhaseWaiting JobPhase = "waiting" // the network's names are not known yet
	PhaseFailed  JobPhase = "failed"  // the last check failed (LastError)
	PhasePending JobPhase = "pending" // not checked yet
)

// JobStatus is one job with its files and history.
type JobStatus struct {
	Job   Job      `json:"job"`
	Phase JobPhase `json:"phase"`
	Files Files    `json:"files"`
	Dir   string   `json:"dir"`
	// Names are the names the certificate should carry now.
	Names SANs        `json:"names"`
	Cert  *LeafStatus `json:"cert,omitempty"`
	// HookWarning is set when the last hook failed; the certificate files
	// are written regardless.
	HookWarning bool `json:"hook_warning,omitempty"`
	JobState
}

// Status is everything a client shows about certificates.
type Status struct {
	CA *CAStatus `json:"ca,omitempty"`
	// Trust is the system trust store as the daemon's process sees it;
	// its Privileged is the daemon's own.
	Trust TrustStatus `json:"trust"`
	// Restricted: the privileged helper's policy applies (a folder of the
	// job's own and a command need an administrator's approval).
	Restricted bool        `json:"restricted,omitempty"`
	Jobs       []JobStatus `json:"jobs"`
}

// Status reports the CA, the trust store and every job.
func (m *Manager) Status() Status {
	m.mu.Lock()
	var ca *CAStatus
	if m.caCert != nil {
		c := *m.caCert
		c.Usable = m.ca != nil
		if m.caErr != nil {
			c.Problem = m.caErr.Error()
		}
		ca = &c
	} else if m.caErr != nil {
		ca = &CAStatus{Path: m.CAPath(), Problem: m.caErr.Error()}
	}
	jobs := slices.Clone(m.jobs)
	states := map[string]JobState{}
	for id, st := range m.state {
		states[id] = *st
	}
	m.mu.Unlock()
	trust := m.trustStatus()

	out := Status{CA: ca, Trust: trust, Restricted: m.opts.Restricted, Jobs: []JobStatus{}}
	for _, j := range jobs {
		st := states[j.ID]
		files, dir := j.files(m.path("out"))
		js := JobStatus{Job: j, Files: files, Dir: dir, JobState: st}
		cp := st
		nn, ok := m.networkNames(j)
		if want, known, err := wantedNames(j, &cp, nn, ok); err == nil && known {
			js.Names = want
		}
		if d := Inspect(files); d.Leaf != nil {
			js.Cert = &LeafStatus{SANs: certSANs(d.Leaf), NotBefore: d.Leaf.NotBefore, NotAfter: d.Leaf.NotAfter,
				Serial: d.Leaf.SerialNumber.Text(16), Fingerprint: Fingerprint(d.Leaf)}
		}
		switch {
		case st.LastCheck.IsZero():
			js.Phase = PhasePending
		case st.LastError != "":
			js.Phase = PhaseFailed
		case j.Network != "" && !st.NamesKnown:
			js.Phase = PhaseWaiting
		case js.Cert != nil:
			js.Phase = PhaseOK
		default:
			js.Phase = PhasePending
		}
		js.HookWarning = st.Hook != nil && !st.Hook.OK
		out.Jobs = append(out.Jobs, js)
	}
	return out
}

// trustStatus returns the trust store state, re-read at most every 30 s
// (on Windows and macOS it runs a tool, so it runs outside the lock).
func (m *Manager) trustStatus() TrustStatus {
	m.mu.Lock()
	var cert *x509.Certificate
	if m.ca != nil {
		cert = m.ca.Cert
	} else if certPEM, err := os.ReadFile(m.CAPath()); err == nil {
		cert, _ = firstCert(certPEM)
	}
	if cert == nil {
		m.mu.Unlock()
		return TrustStatus{Privileged: Elevated(), Supported: backend() != nil}
	}
	fp := Fingerprint(cert)
	if fp == m.trustF && time.Since(m.trustT) < trustCacheTTL {
		t := m.trust
		m.mu.Unlock()
		return t
	}
	m.mu.Unlock()
	t := Trust(cert, m.CAPath())
	m.mu.Lock()
	m.trust, m.trustT, m.trustF = t, time.Now(), fp
	m.mu.Unlock()
	return t
}

// caStatusOf describes a CA certificate stored at path.
func caStatusOf(c *x509.Certificate, path string) *CAStatus {
	kt := "unknown"
	switch k := c.PublicKey.(type) {
	case *ecdsa.PublicKey:
		kt = "ecdsa-" + strings.ToLower(strings.ReplaceAll(k.Curve.Params().Name, "-", ""))
	case *rsa.PublicKey:
		kt = fmt.Sprintf("rsa-%d", k.N.BitLen())
	}
	return &CAStatus{Name: c.Subject.CommonName, Subject: c.Subject.String(), Fingerprint: Fingerprint(c), NotBefore: c.NotBefore,
		NotAfter: c.NotAfter, KeyType: kt, Path: path}
}

// lastLines keeps the last n lines of a hook's output for the log.
func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

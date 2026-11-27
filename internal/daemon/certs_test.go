//go:build linux || darwin

package daemon

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/certmanager"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/localapi"
)

// TestCertificateFollowsNetwork: a job for a network waits until the
// network's core reports its names, then issues a certificate for
// <hostname>, <hostname>.<zone> and the virtual address, and keeps it
// when the network stops.
func TestCertificateFollowsNetwork(t *testing.T) {
	h := start(t, nil)
	id := h.create(noTUN + "accept_dns = true\n")
	cert := func(req localapi.CertRequest, typ localapi.RequestType) *certmanager.Status {
		t.Helper()
		return h.do(localapi.Request{Type: typ, Cert: &req}).Cert
	}
	out := filepath.Join(h.dir, "out")
	st := cert(localapi.CertRequest{Job: &certmanager.Job{ID: "web", Network: id, OutDir: out}}, localapi.RequestCertJobSet)
	if st.Jobs[0].Phase != certmanager.PhaseWaiting {
		t.Fatalf("before the network runs: %s %q", st.Jobs[0].Phase, st.Jobs[0].LastError)
	}
	cert(localapi.CertRequest{}, localapi.RequestCertCAGenerate)

	h.do(localapi.Request{Type: localapi.RequestConnect, Network: id})
	detach := h.attach(id)
	h.eventually("connected", func() bool { return h.status(id).State == localapi.StateConnected })
	st = cert(localapi.CertRequest{JobID: "web"}, localapi.RequestCertRenew)
	js := st.Jobs[0]
	want := certmanager.SANs{DNS: []string{"me", "me.et.net"}, IPs: []string{"10.9.0.1"}}
	if js.Phase != certmanager.PhaseOK || js.Cert == nil || !js.Cert.SANs.Equal(want) {
		t.Fatalf("after connecting: %s %q %+v", js.Phase, js.LastError, js.Cert)
	}
	if _, err := os.Stat(filepath.Join(out, "fullchain.pem")); err != nil {
		t.Fatal(err)
	}
	serial := js.Cert.Serial

	detach()
	h.eventually("stopped", func() bool { return h.status(id).State == localapi.StateStopped })
	st = cert(localapi.CertRequest{}, localapi.RequestCertRenew)
	if js := st.Jobs[0]; js.Cert.Serial != serial || !js.Cert.SANs.Equal(want) {
		t.Fatalf("a stopped network changed the certificate: %+v", js.Cert)
	}

	exp := h.do(localapi.Request{Type: localapi.RequestCertCAExport}).CertExport
	if exp.KeyPEM != "" || exp.Fingerprint != st.CA.Fingerprint {
		t.Fatalf("export: %+v", exp)
	}
	_, err := h.client.Do(context.Background(), localapi.Request{Type: localapi.RequestCertCAGenerate, Cert: &localapi.CertRequest{}})
	if localapi.CodeOf(err) != localapi.CodeConflict {
		t.Fatalf("second CA without replace: %v", err)
	}
}

// TestHelperNeedsApprovalForCertificates: the privileged helper takes a
// command or a folder of its callers' choosing only once an administrator
// approved it (no API request can), and keeps other jobs in its own folder.
func TestHelperNeedsApprovalForCertificates(t *testing.T) {
	h := start(t, func(o *Options) { o.Host = localapi.HostHelper })
	certs := filepath.Join(h.dir, "state", "certs")
	out := filepath.Join(h.dir, "srv")
	for _, job := range []certmanager.Job{
		{ID: "hook", DNSNames: []string{"a.et.net"}, Hook: certmanager.Hook{Enabled: true, Command: "touch /tmp/x"}},
		{ID: "dir", DNSNames: []string{"a.et.net"}, OutDir: "/etc"},
	} {
		_, err := h.client.Do(context.Background(), localapi.Request{Type: localapi.RequestCertJobSet, Cert: &localapi.CertRequest{Job: &job}})
		if localapi.CodeOf(err) != localapi.CodeNeedsAdmin {
			t.Errorf("%+v: %v, want needs-admin", job, err)
		}
	}
	st := h.do(localapi.Request{Type: localapi.RequestCertJobSet, Cert: &localapi.CertRequest{Job: &certmanager.Job{ID: "ok", DNSNames: []string{"a.et.net"}}}}).Cert
	if !st.Restricted || !slices.ContainsFunc(st.Jobs, func(j certmanager.JobStatus) bool {
		return j.Dir == filepath.Join(certs, "out", "ok")
	}) {
		t.Fatalf("helper job: %+v", st)
	}
	// What `easytier-mygo-svc helper approve-cert` does with administrator rights.
	job := certmanager.Job{ID: "web", DNSNames: []string{"web.et.net"}, OutDir: out,
		Hook: certmanager.Hook{Enabled: true, Command: `echo reloaded > "{{OUT_DIR}}/hook.log"`}}
	if _, err := certmanager.Approve(certs, job, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	h.do(localapi.Request{Type: localapi.RequestCertCAGenerate, Cert: &localapi.CertRequest{}})
	st = h.do(localapi.Request{Type: localapi.RequestCertJobSet, Cert: &localapi.CertRequest{Job: &job}}).Cert
	if data, err := os.ReadFile(filepath.Join(out, "hook.log")); err != nil || !strings.Contains(string(data), "reloaded") {
		t.Fatalf("approved job: %v %+v", err, st.Jobs)
	}
}

// TestCertRequestsNeedWriteAccess: certificate state carries paths, hook
// output and (on export) the CA key.
func TestCertRequestsNeedWriteAccess(t *testing.T) {
	for _, typ := range []localapi.RequestType{localapi.RequestCertStatus, localapi.RequestCertCAExport, localapi.RequestCertRenew} {
		if localapi.AccessRead.Allows(typ) {
			t.Errorf("read-only peers may run %s", typ)
		}
	}
}

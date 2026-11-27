package daemon

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/certmanager"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/configdoc"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/localapi"
)

// openCerts creates the certificate manager in <state>/certs. A manager
// that cannot open leaves the rest of the daemon running; cert requests
// then report why.
// Notes: in the privileged helper the manager is restricted: a folder of a
// job's own or a command needs an administrator's approval
// (`easytier-mygo-svc helper approve-cert`), so a member of the helper's
// group cannot make root write arbitrary files or run commands.
func (d *Daemon) openCerts() {
	m, err := certmanager.New(certmanager.Options{
		Dir:        filepath.Join(d.opts.StateDir, "certs"),
		Restricted: d.opts.Host == localapi.HostHelper,
		Names:      d.certNames,
		Logf:       d.opts.Logf,
	})
	if err != nil {
		d.opts.Logf("certificates: %v", err)
		d.certErr = err
		return
	}
	d.certs = m
}

// certNames reports the names a network's running core uses for this
// computer: its hostname, <hostname>.<MagicDNS zone> and its virtual
// IPv4 address.
// Returns: ok=false unless the core is running and reports a hostname,
// so a stopped network never changes a certificate.
func (d *Daemon) certNames(id string) (certmanager.NetworkNames, bool) {
	d.mu.Lock()
	e := d.workers[id]
	d.mu.Unlock()
	if e == nil {
		return certmanager.NetworkNames{}, false
	}
	s := e.w.Status()
	switch s.State {
	case localapi.StateConnected, localapi.StateConnecting, localapi.StateDegraded:
	default:
		return certmanager.NetworkNames{}, false
	}
	if s.Hostname == "" {
		return certmanager.NetworkNames{}, false
	}
	var nn certmanager.NetworkNames
	if host := dnsLabel(s.Hostname); host != "" {
		nn.DNS = append(nn.DNS, host)
		if text, _, err := d.store.Config(id); err == nil {
			if sum, err := configdoc.Parse(text); err == nil && sum.MagicDNSZone != "" {
				nn.DNS = append(nn.DNS, host+"."+sum.MagicDNSZone)
			}
		}
	}
	if s.VirtualIPv4 != "" {
		nn.IPs = append(nn.IPs, s.VirtualIPv4)
	}
	return nn, true
}

// dnsLabel is a hostname as a DNS label (lower case), or "" when it is
// not one (EasyTier allows any hostname; MagicDNS publishes only these).
func dnsLabel(host string) string {
	host = strings.ToLower(host)
	if host == "" || len(host) > 63 || host[0] == '-' || host[len(host)-1] == '-' {
		return ""
	}
	for _, r := range host {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return ""
		}
	}
	return host
}

// handleCert answers the cert.* requests.
func (d *Daemon) handleCert(ctx context.Context, req localapi.Request) localapi.Response {
	m := d.certs
	if m == nil {
		msg := "certificates are not available"
		if d.certErr != nil {
			msg += ": " + d.certErr.Error()
		}
		return localapi.Fail("", "%s", msg)
	}
	args := req.Cert
	if args == nil {
		args = &localapi.CertRequest{}
	}
	status := func() localapi.Response {
		s := m.Status()
		return localapi.Response{OK: true, Cert: &s}
	}
	switch req.Type {
	case localapi.RequestCertStatus:
		return status()
	case localapi.RequestCertCAGenerate:
		opts := certmanager.CAOptions{}
		if args.CA != nil {
			opts = *args.CA
		}
		if err := m.GenerateCA(opts, args.Replace); err != nil {
			return certFail(err)
		}
		m.CheckAll(ctx, false)
		return status()
	case localapi.RequestCertCAImport:
		if err := m.ImportCA([]byte(args.CertPEM), []byte(args.KeyPEM), args.Replace); err != nil {
			return certFail(err)
		}
		m.CheckAll(ctx, false)
		return status()
	case localapi.RequestCertCAExport:
		e, err := m.ExportCA(args.WithKey)
		if err != nil {
			return certFail(err)
		}
		return localapi.Response{OK: true, CertExport: &e}
	case localapi.RequestCertJobSet:
		if args.Job == nil {
			return localapi.Fail("", "no certificate job")
		}
		if j := args.Job; j.Network != "" {
			if _, err := d.store.Get(j.Network); err != nil {
				return failOf(err)
			}
		}
		j, err := m.SetJob(*args.Job)
		if err != nil {
			return certFail(err)
		}
		_ = m.Check(ctx, j.ID, false)
		return status()
	case localapi.RequestCertJobRemove:
		if err := m.RemoveJob(args.JobID); err != nil {
			return certFail(err)
		}
		return status()
	case localapi.RequestCertRenew:
		if args.JobID == "" {
			m.CheckAll(ctx, args.Force)
		} else if err := m.Check(ctx, args.JobID, args.Force); err != nil {
			return certFail(err)
		}
		return status()
	}
	return localapi.Fail("", "unknown request %q", req.Type)
}

// certFail maps certificate errors to coded responses.
func certFail(err error) localapi.Response {
	switch {
	case errors.Is(err, certmanager.ErrCAExists):
		return localapi.Fail(localapi.CodeConflict, "%s", err)
	case errors.Is(err, certmanager.ErrJobNotFound), errors.Is(err, certmanager.ErrNoCA):
		return localapi.Fail(localapi.CodeNotFound, "%s", err)
	case errors.Is(err, certmanager.ErrNeedsApproval):
		return localapi.Fail(localapi.CodeNeedsAdmin, "%s", err)
	}
	return localapi.Fail("", "%s", err)
}

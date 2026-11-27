//go:build linux

package certmanager

import (
	"crypto/x509"
	"errors"
	"os"
	"os/exec"
)

// systemBundles are the extracted trust bundles of the supported families;
// the CA counts as installed when one of them contains it.
var systemBundles = []string{
	"/etc/ssl/certs/ca-certificates.crt",                // Debian, Ubuntu, Alpine, Arch
	"/etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem", // Fedora, RHEL
	"/etc/pki/tls/certs/ca-bundle.crt",                  // RHEL (compatibility link)
	"/var/lib/ca-certificates/ca-bundle.pem",            // openSUSE
}

type linuxTrust struct{ f linuxFlavor }

// backend returns the Linux trust store, or nil when the distribution's
// layout is unknown.
func backend() trustBackend {
	osRelease, _ := os.ReadFile("/etc/os-release")
	f, ok := detectLinuxFlavor(string(osRelease), func(p string) bool {
		_, err := exec.LookPath(p)
		return err == nil
	})
	if !ok {
		return nil
	}
	return linuxTrust{f}
}

// Elevated reports whether this process runs as root.
func Elevated() bool { return os.Geteuid() == 0 }

func (t linuxTrust) name() string { return "linux-" + t.f.id }

func (t linuxTrust) installed(cert *x509.Certificate) (bool, error) {
	read := false
	for _, b := range systemBundles {
		data, err := os.ReadFile(b)
		if err != nil {
			continue
		}
		read = true
		if bundleHas(data, cert) {
			return true, nil
		}
	}
	if !read {
		return false, errors.New("no system certificate bundle found")
	}
	return false, nil
}

func (t linuxTrust) install(cert *x509.Certificate, path string) error {
	if t.f.id == "arch" {
		_, err := run("trust", "anchor", "--store", path)
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(t.f.anchorDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(t.f.anchorPath(cert), data, 0o644); err != nil {
		return err
	}
	_, err = run(t.f.update[0], t.f.update[1:]...)
	return err
}

func (t linuxTrust) uninstall(cert *x509.Certificate, path string) error {
	if t.f.id == "arch" {
		_, err := run("trust", "anchor", "--remove", path)
		return err
	}
	if err := os.Remove(t.f.anchorPath(cert)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	_, err := run(t.f.update[0], t.f.update[1:]...)
	return err
}

func (t linuxTrust) installCommand(cert *x509.Certificate, path string) string {
	return t.f.installCommand(cert, path)
}

func (t linuxTrust) uninstallCommand(cert *x509.Certificate, path string) string {
	return t.f.uninstallCommand(cert, path)
}

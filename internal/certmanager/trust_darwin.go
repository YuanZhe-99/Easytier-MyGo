//go:build darwin

package certmanager

import (
	"crypto/x509"
	"os"
	"strings"
)

const systemKeychain = "/Library/Keychains/System.keychain"

type darwinTrust struct{}

// backend returns the System keychain.
func backend() trustBackend { return darwinTrust{} }

// Elevated reports whether this process runs as root.
func Elevated() bool { return os.Geteuid() == 0 }

func (darwinTrust) name() string { return "macos-system" }

// installed looks the certificate up by its SHA-1 in the System keychain
// (readable without privileges).
func (darwinTrust) installed(cert *x509.Certificate) (bool, error) {
	out, err := run("security", "find-certificate", "-a", "-Z", systemKeychain)
	if err != nil {
		return false, err
	}
	return strings.Contains(strings.ToUpper(out), thumbprint(cert)), nil
}

// install adds the certificate as a trusted root for every user. Notes:
// macOS asks for confirmation in a dialog even as root; a process without
// a login session fails with "no user interaction was possible".
func (darwinTrust) install(cert *x509.Certificate, path string) error {
	_, err := run("security", "add-trusted-cert", "-d", "-r", "trustRoot", "-k", systemKeychain, path)
	return err
}

// uninstall drops the trust setting, then the certificate itself.
func (darwinTrust) uninstall(cert *x509.Certificate, path string) error {
	_, _ = run("security", "remove-trusted-cert", "-d", path)
	_, err := run("security", "delete-certificate", "-Z", thumbprint(cert), systemKeychain)
	return err
}

func (darwinTrust) installCommand(cert *x509.Certificate, path string) string {
	return "sudo security add-trusted-cert -d -r trustRoot -k " + systemKeychain + " " + shQuote(path)
}

func (darwinTrust) uninstallCommand(cert *x509.Certificate, path string) string {
	return "sudo security remove-trusted-cert -d " + shQuote(path) +
		"; sudo security delete-certificate -Z " + thumbprint(cert) + " " + systemKeychain
}

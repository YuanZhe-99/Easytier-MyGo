//go:build windows

package certmanager

import (
	"crypto/x509"
	"strings"

	"golang.org/x/sys/windows"
)

type windowsTrust struct{}

// backend returns the local machine's Trusted Root Certification
// Authorities store.
func backend() trustBackend { return windowsTrust{} }

// Elevated reports whether this process runs with an elevated
// (administrator) token.
func Elevated() bool { return windows.GetCurrentProcessToken().IsElevated() }

func (windowsTrust) name() string { return "windows-root" }

// installed asks certutil for the certificate by thumbprint; reading the
// machine's Root store needs no privileges.
func (windowsTrust) installed(cert *x509.Certificate) (bool, error) {
	_, err := run("certutil", "-store", "Root", thumbprint(cert))
	return err == nil, nil
}

func (windowsTrust) install(cert *x509.Certificate, path string) error {
	_, err := run("certutil", "-addstore", "-f", "Root", path)
	return err
}

func (windowsTrust) uninstall(cert *x509.Certificate, path string) error {
	_, err := run("certutil", "-delstore", "Root", thumbprint(cert))
	return err
}

// installCommand is for an administrator PowerShell or Command Prompt.
func (windowsTrust) installCommand(cert *x509.Certificate, path string) string {
	return `certutil -addstore -f Root "` + strings.ReplaceAll(path, `"`, ``) + `"`
}

func (windowsTrust) uninstallCommand(cert *x509.Certificate, path string) string {
	return "certutil -delstore Root " + thumbprint(cert)
}

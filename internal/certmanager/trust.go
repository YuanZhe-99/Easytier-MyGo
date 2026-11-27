package certmanager

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// TrustStatus describes the CA in the operating system's trust store, as
// seen by the process that asks.
type TrustStatus struct {
	// Supported is false where no trust store backend exists.
	Supported bool `json:"supported"`
	// Store names the backend: "windows-root", "macos-system",
	// "linux-debian", "linux-redhat", "linux-suse", "linux-arch".
	Store string `json:"store,omitempty"`
	// Installed reports whether the system already trusts the CA.
	Installed bool `json:"installed"`
	// Privileged reports whether this process may change the store
	// (administrator or root).
	Privileged bool `json:"privileged"`
	// InstallCommand and UninstallCommand can be copied into an
	// administrator shell when the process is not privileged.
	InstallCommand   string `json:"install_command,omitempty"`
	UninstallCommand string `json:"uninstall_command,omitempty"`
	// Error is a failed installed check (the store could not be read).
	Error string `json:"error,omitempty"`
}

// PrivilegeError refuses a trust store change in a process without the
// rights for it; Command does the same in an administrator shell.
type PrivilegeError struct {
	Command string
}

func (e *PrivilegeError) Error() string {
	return "this process is not allowed to change the system trust store (administrator or root rights are needed); run instead:\n  " + e.Command
}

// ErrTrustUnsupported is returned on systems without a trust store backend.
var ErrTrustUnsupported = errors.New("installing a trusted root certificate is not supported on this system")

// trustBackend is one platform's trust store.
type trustBackend interface {
	name() string
	installed(cert *x509.Certificate) (bool, error)
	install(cert *x509.Certificate, path string) error
	uninstall(cert *x509.Certificate, path string) error
	installCommand(cert *x509.Certificate, path string) string
	uninstallCommand(cert *x509.Certificate, path string) string
}

// Trust inspects the system trust store for cert, stored at path (the
// path goes into the manual commands).
func Trust(cert *x509.Certificate, path string) TrustStatus {
	b := backend()
	st := TrustStatus{Privileged: Elevated()}
	if b == nil || cert == nil {
		return st
	}
	st.Supported, st.Store = true, b.name()
	st.InstallCommand = b.installCommand(cert, path)
	st.UninstallCommand = b.uninstallCommand(cert, path)
	ok, err := b.installed(cert)
	st.Installed = ok
	if err != nil {
		st.Error = err.Error()
	}
	return st
}

// InstallTrust adds cert (stored at path) to the system's trusted roots.
// Returns: *PrivilegeError without administrator rights (nothing is run),
// ErrTrustUnsupported where there is no backend.
// Side effects: runs the platform's tool (certutil, security,
// update-ca-certificates, update-ca-trust or trust).
func InstallTrust(cert *x509.Certificate, path string) error {
	b := backend()
	if b == nil {
		return ErrTrustUnsupported
	}
	if !Elevated() {
		return &PrivilegeError{Command: b.installCommand(cert, path)}
	}
	return b.install(cert, path)
}

// UninstallTrust removes cert from the system's trusted roots. Same rules
// as InstallTrust.
func UninstallTrust(cert *x509.Certificate, path string) error {
	b := backend()
	if b == nil {
		return ErrTrustUnsupported
	}
	if !Elevated() {
		return &PrivilegeError{Command: b.uninstallCommand(cert, path)}
	}
	return b.uninstall(cert, path)
}

// run executes one trust store tool and reports its output on failure.
func run(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	setNoWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if len(msg) > 2000 {
			msg = msg[:2000] + "…"
		}
		return string(out), fmt.Errorf("%s: %v: %s", name, err, msg)
	}
	return string(out), nil
}

// shQuote quotes one word for a POSIX shell.
func shQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\n'\"\\$`!*?[]{}()<>|&;#~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

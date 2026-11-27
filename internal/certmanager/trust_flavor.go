package certmanager

import (
	"crypto/x509"
	"path"
	"slices"
	"strings"
)

// linuxFlavor is a Linux family's trust store layout.
type linuxFlavor struct {
	id string // debian, redhat, suse, arch
	// anchorDir receives <name>.crt, then update runs (debian, redhat, suse).
	anchorDir string
	update    []string
}

var (
	flavorDebian = linuxFlavor{"debian", "/usr/local/share/ca-certificates", []string{"update-ca-certificates"}}
	flavorRedHat = linuxFlavor{"redhat", "/etc/pki/ca-trust/source/anchors", []string{"update-ca-trust", "extract"}}
	flavorSUSE   = linuxFlavor{"suse", "/etc/pki/trust/anchors", []string{"update-ca-certificates"}}
	// Arch stores anchors with p11-kit's `trust anchor`.
	flavorArch = linuxFlavor{id: "arch"}
)

// detectLinuxFlavor picks the trust store layout from /etc/os-release
// (ID, then ID_LIKE) and, when that names nothing known, from the tools
// installed (has reports whether a program is on PATH).
// Returns: ok=false when neither identifies a supported layout.
func detectLinuxFlavor(osRelease string, has func(string) bool) (linuxFlavor, bool) {
	var ids []string
	for _, line := range strings.Split(osRelease, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || (k != "ID" && k != "ID_LIKE") {
			continue
		}
		v = strings.Trim(v, `"'`)
		if k == "ID" {
			ids = append([]string{v}, ids...)
		} else {
			ids = append(ids, strings.Fields(v)...)
		}
	}
	for _, id := range ids {
		switch id {
		case "debian", "ubuntu", "alpine", "linuxmint", "raspbian", "pop":
			return flavorDebian, true
		case "rhel", "fedora", "centos", "rocky", "almalinux", "ol", "amzn":
			return flavorRedHat, true
		case "suse", "opensuse", "opensuse-leap", "opensuse-tumbleweed", "sles":
			return flavorSUSE, true
		case "arch", "manjaro", "endeavouros":
			return flavorArch, true
		}
	}
	switch {
	case has("update-ca-certificates"):
		return flavorDebian, true
	case has("update-ca-trust"):
		return flavorRedHat, true
	case has("trust"):
		return flavorArch, true
	}
	return linuxFlavor{}, false
}

// anchorPath is where the flavor keeps this CA ("" for Arch).
func (f linuxFlavor) anchorPath(cert *x509.Certificate) string {
	if f.anchorDir == "" {
		return ""
	}
	return path.Join(f.anchorDir, trustName(cert)+".crt")
}

// installCommand is the shell line that installs the CA file at path.
func (f linuxFlavor) installCommand(cert *x509.Certificate, path string) string {
	if f.id == "arch" {
		return "sudo trust anchor --store " + shQuote(path)
	}
	return "sudo install -m 0644 " + shQuote(path) + " " + shQuote(f.anchorPath(cert)) +
		" && sudo " + strings.Join(f.update, " ")
}

// uninstallCommand is the shell line that removes the CA again.
func (f linuxFlavor) uninstallCommand(cert *x509.Certificate, path string) string {
	if f.id == "arch" {
		return "sudo trust anchor --remove " + shQuote(path)
	}
	return "sudo rm -f " + shQuote(f.anchorPath(cert)) + " && sudo " + strings.Join(f.update, " ")
}

// bundleHas reports whether a PEM bundle contains cert.
func bundleHas(bundle []byte, cert *x509.Certificate) bool {
	certs, err := parseCerts(bundle)
	if err != nil {
		return false
	}
	return slices.ContainsFunc(certs, cert.Equal)
}

// Package helper installs, removes and inspects the privileged helper: the
// daemon registered once with the operating system so that networks which
// need a virtual network device can run without asking for administrator
// rights again (doc/en-us/run-modes.md).
//
// Install and Uninstall run as root (the GUI starts them through pkexec);
// Inspect runs as anyone.
package helper

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
)

// Layout of an installed helper (BinDir and StateDir are per platform, in
// paths_*.go). The binaries are administrator-owned copies, so a user
// cannot swap what the helper executes.
const (
	// Group may control the helper in addition to the operator (Linux).
	Group = "easytier-mygo"
	// Unit is the systemd service; Unit+".socket" starts it on demand.
	Unit = "easytier-mygo"
	// IdleExit ends an on-demand helper nobody uses.
	IdleExit = "60s"
)

// Binaries are the files the helper needs, copied from the app bundle;
// Optional ones are copied when present (upx unpacks downloaded cores).
var (
	Binaries = []string{"easytier-mygo-svc", "easytier-core", "easytier-cli"}
	Optional = []string{"upx", "upx-COPYING"}
)

// ErrUnsupported reports a platform without a helper backend yet.
var ErrUnsupported = errors.New("the privileged helper is not available on this platform yet")

// Status describes the helper as installed on this machine.
type Status struct {
	Installed bool `json:"installed"`
	// Active means the socket accepts connections (the helper starts on
	// the first one).
	Active bool `json:"active"`
	// Background means the service starts at boot.
	Background bool `json:"background"`
	// Version is the installed helper binary's version, when readable.
	Version string `json:"version,omitempty"`
}

// InstallOptions describes an installation.
type InstallOptions struct {
	// From is the directory holding the bundle's binaries.
	From string
	// Operator is the uid allowed to control the helper (Linux, macOS).
	Operator uint32
	// OperatorSID is the operator on Windows.
	OperatorSID string
}

// svcPath is the installed daemon binary.
func svcPath() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(BinDir, "easytier-mygo-svc.exe")
	}
	return filepath.Join(BinDir, "easytier-mygo-svc")
}

// SocketUnit is the systemd socket unit text. The socket is world
// connectable; the daemon authorizes each peer by its credentials
// (operator, root, the helper group) and refuses everyone else.
func SocketUnit() string {
	return `[Unit]
Description=EasyTier MyGo privileged helper (socket)

[Socket]
ListenStream=/run/easytier-mygo/ctl.sock
SocketMode=0666
DirectoryMode=0755
RemoveOnStop=yes

[Install]
WantedBy=sockets.target
`
}

// ServiceUnit is the systemd service unit text. It is started by the
// socket; enabling it for multi-user.target is background mode.
func ServiceUnit() string {
	return fmt.Sprintf(`[Unit]
Description=EasyTier MyGo privileged helper
Documentation=https://github.com/YuanZhe-99/Easytier-MyGo
Requires=%[1]s.socket
After=%[1]s.socket network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%[2]s run --host helper --idle-exit %[3]s
Restart=on-failure
RestartSec=3
StateDirectory=easytier-mygo
StateDirectoryMode=0700
NoNewPrivileges=yes
ProtectSystem=full
# The helper updates its own binaries from signed bundles.
ReadWritePaths=%[4]s
PrivateTmp=yes

[Install]
WantedBy=multi-user.target
`, Unit, svcPath(), IdleExit, BinDir)
}

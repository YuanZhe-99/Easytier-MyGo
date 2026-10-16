// Package platformservice installs and controls the resident management
// service with the operating system's service manager. The daemon itself
// owns core health and recovery; the service manager owns the daemon's
// own process lifetime, so the two layers do not restart each other's
// failures.
//
// Linux (systemd) is implemented with two scopes:
//
//   - ScopeUser: a systemd user service, started at login. For machines
//     that start without a login session, systemd needs lingering
//     (`loginctl enable-linger <user>`), which the GUI can offer.
//   - ScopeSystem: a systemd system service, started at boot. Running the
//     core with TUN needs elevated rights; installing this scope needs
//     root. The local API authorization story for a system service is
//     still open (report §7.4), so this scope currently targets the same
//     user's administration (sudo / root shells).
//
// Other platforms return ErrUnsupported for now; Windows (SCM) and macOS
// (launchd) are planned in the research report.
package platformservice

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Scope is who the service starts for.
type Scope string

const (
	// ScopeUser starts the service at login (systemd --user).
	ScopeUser Scope = "user"
	// ScopeSystem starts the service at boot (systemd system).
	ScopeSystem Scope = "system"
)

// ErrUnsupported means this platform or scope has no implementation yet.
var ErrUnsupported = errors.New("platform service management not implemented for this platform")

// Spec describes the service to install. Paths must be absolute: the
// service manager runs from its own environment.
type Spec struct {
	// Name is the service name, e.g. "easytier-mygo-svc".
	Name string
	// Description is the human-readable unit description.
	Description string
	// Executable is the absolute path of the management daemon.
	Executable string
	// Args are the daemon's arguments (the service runs Executable Args).
	Args []string
	// Scope selects login (user) or boot (system) startup.
	Scope Scope
	// SocketGroup is the local-API authorization group for a system
	// service. The generated unit runs the daemon with this group so the
	// socket (and the runtime directory systemd creates) is reachable by
	// its members. Empty for a user service.
	SocketGroup string
	// CheckInterval is the daemon's health check cadence, used to derive
	// the unit's WatchdogSec. Zero means the daemon's default.
	CheckInterval time.Duration
}

// Status reports what the service manager knows about the service.
type Status struct {
	Installed bool   `json:"installed"`
	Running   bool   `json:"running"`
	Enabled   bool   `json:"enabled"`
	Scope     Scope  `json:"scope"`
	UnitPath  string `json:"unit_path"`
	// Detail carries the service manager's own words for the UI, e.g.
	// "Service is not installed" or a failure reason.
	Detail string `json:"detail"`
}

// validIdentity checks what every operation needs: a name and a scope.
func (s Spec) validIdentity() error {
	if s.Name == "" || strings.ContainsAny(s.Name, " /\\") {
		return fmt.Errorf("invalid service name %q", s.Name)
	}
	switch s.Scope {
	case ScopeUser, ScopeSystem:
	default:
		return fmt.Errorf("unknown scope %q (user or system)", s.Scope)
	}
	return nil
}

// Valid checks the spec before anything touches the service manager.
// Installing additionally needs an absolute executable path and
// non-empty arguments.
func (s Spec) Valid() error {
	if err := s.validIdentity(); err != nil {
		return err
	}
	if !strings.HasPrefix(s.Executable, "/") {
		return fmt.Errorf("service executable must be an absolute path, got %q", s.Executable)
	}
	for i, a := range s.Args {
		if a == "" {
			return fmt.Errorf("argument %d is empty", i)
		}
	}
	return nil
}

// NeedsRoot reports whether installing this scope requires elevated
// rights on this platform.
func (s Spec) NeedsRoot() bool {
	return s.Scope == ScopeSystem
}

// Supported reports whether this platform has a service manager backend.
func Supported() bool { return driverSupported }

// UserModeHint describes what the active config needs of a service scope.
type UserModeHint struct {
	// UserMode is true when the core runs without a TUN device
	// (no_tun=true): pure userspace forwarding, no elevated rights.
	// False means the core needs TUN and therefore CAP_NET_ADMIN or root.
	UserMode bool
}

// RecommendScope picks the scope for a config and explains why. A
// user-mode core (no TUN) is recommended for a user service, which needs
// no root at all; a TUN core needs the system scope to create and
// configure the interface. The recommendation is advice, not a rule: the
// caller can override it.
func RecommendScope(hint UserModeHint) (scope Scope, reason string) {
	if hint.UserMode {
		return ScopeUser, "该配置以用户态运行（no_tun=true，不创建 TUN），无需提权，推荐用户级服务"
	}
	return ScopeSystem, "该配置需要 TUN 设备，必须由系统级服务运行（安装需要 sudo）"
}

// WatchdogTimeout derives the systemd WatchdogSec for a service from the
// daemon's check cadence: several rounds must fit inside the window so a
// legitimately slow round does not trip it, while a wedged daemon does.
// The floor keeps a fast cadence from producing a jittery window.
func WatchdogTimeout(s Spec) time.Duration {
	interval := s.CheckInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	window := 6 * interval
	if window < 60*time.Second {
		window = 60 * time.Second
	}
	return window
}

// ScopeWarning returns a non-empty warning when the chosen scope does not
// match what the config needs, so the caller can confirm before
// installing something that will not work, or that asks for more rights
// than necessary.
func ScopeWarning(scope Scope, hint UserModeHint) string {
	switch {
	case scope == ScopeUser && !hint.UserMode:
		return "该配置需要 TUN 设备，但用户级服务无法创建它：核心会启动失败。请改用 --scope system。"
	case scope == ScopeSystem && hint.UserMode:
		return "该配置以用户态运行，其实无需提权；系统级服务可以工作，但需要 sudo 且权限更大。用户级已足够。"
	default:
		return ""
	}
}

// Install writes the service definition, enables it and starts it.
func Install(s Spec) error { return driverInstall(s) }

// Uninstall stops, disables and removes the service definition.
func Uninstall(s Spec) error { return driverUninstall(s) }

// Inspect reports the current installation and run state.
func Inspect(s Spec) (Status, error) { return driverStatus(s) }

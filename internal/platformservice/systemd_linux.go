//go:build linux

package platformservice

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"time"
)

const driverSupported = true

func driverInstall(s Spec) error   { return install(s) }
func driverUninstall(s Spec) error { return uninstall(s) }
func driverStatus(s Spec) (Status, error) {
	return status(s)
}

// systemdUnit is the generated unit for both scopes.
func systemdUnit(s Spec) string {
	wantedBy := "default.target"
	if s.Scope == ScopeSystem {
		wantedBy = "multi-user.target"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[Unit]\n")
	fmt.Fprintf(&b, "Description=%s\n", s.Description)
	fmt.Fprintf(&b, "After=network-online.target\n")
	fmt.Fprintf(&b, "Wants=network-online.target\n")
	fmt.Fprintf(&b, "\n[Service]\n")
	// Type=notify: the daemon reports READY=1 once its API is serving and
	// WATCHDOG=1 after each completed check round. That is what makes the
	// watchdog below able to catch a process that is alive but hung.
	fmt.Fprintf(&b, "Type=notify\n")
	if wd := WatchdogTimeout(s); wd > 0 {
		fmt.Fprintf(&b, "WatchdogSec=%s\n", systemdDuration(wd))
	}
	if s.Scope == ScopeSystem {
		// systemd creates and owns these directories; the daemon then
		// only has to chown the socket itself to the authorization group.
		// The group is the one from --socket-group (see Spec.SocketGroup).
		fmt.Fprintf(&b, "StateDirectory=%s\n", s.Name)
		fmt.Fprintf(&b, "RuntimeDirectory=%s\n", s.Name)
		if s.SocketGroup != "" {
			fmt.Fprintf(&b, "RuntimeDirectoryMode=0750\n")
			fmt.Fprintf(&b, "Group=%s\n", s.SocketGroup)
		}
	}
	fmt.Fprintf(&b, "ExecStart=%s", s.Executable)
	for _, a := range s.Args {
		b.WriteString(" " + systemdQuote(a))
	}
	b.WriteString("\n")
	// The daemon reports "系统服务" in the GUI based on this marker; the
	// generic systemd INVOCATION_ID is not used because children of any
	// systemd session inherit it.
	fmt.Fprintf(&b, "Environment=EASYTIER_MYGO_SERVICE=1\n")
	// The daemon recovers the core inside its own budget; systemd only
	// recovers the daemon process. Keep the OS layer conservative so a
	// crash-looping daemon cannot fight the health engine.
	fmt.Fprintf(&b, "Restart=on-failure\n")
	fmt.Fprintf(&b, "RestartSec=5\n")
	fmt.Fprintf(&b, "TimeoutStopSec=20\n")
	fmt.Fprintf(&b, "\n[Install]\n")
	fmt.Fprintf(&b, "WantedBy=%s\n", wantedBy)
	return b.String()
}

// systemdDuration renders a duration the way systemd unit files want it:
// "90s", "2min 30s". systemd accepts plain seconds, which is what is used
// here for predictability.
func systemdDuration(d time.Duration) string {
	seconds := int(d / time.Second)
	if seconds <= 0 {
		seconds = 1
	}
	return fmt.Sprintf("%ds", seconds)
}

// systemdQuote quotes an ExecStart argument. systemd splits on
// whitespace; double quotes group, backslash escapes.
func systemdQuote(s string) string {
	if s == "" {
		return `""`
	}
	if !strings.ContainsAny(s, " \t\"'\\") {
		return s
	}
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + r.Replace(s) + `"`
}

func unitPath(s Spec) (string, error) {
	if s.Scope == ScopeSystem {
		return filepath.Join("/etc/systemd/system", s.Name+".service"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		configHome = filepath.Join(home, ".config")
	}
	return filepath.Join(configHome, "systemd", "user", s.Name+".service"), nil
}

func systemctlArgs(s Spec, args ...string) []string {
	if s.Scope == ScopeUser {
		return append([]string{"--user"}, args...)
	}
	return args
}

func runSystemctl(s Spec, args ...string) (string, error) {
	cmd := exec.Command("systemctl", systemctlArgs(s, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("systemctl %s: %s", strings.Join(args, " "), msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// Install writes the unit, reloads systemd, enables and starts it.
func install(s Spec) error {
	if err := s.Valid(); err != nil {
		return err
	}
	if s.NeedsRoot() && os.Geteuid() != 0 {
		return fmt.Errorf("installing the %s scope needs root (try sudo)", s.Scope)
	}
	path, err := unitPath(s)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(systemdUnit(s)), 0o644); err != nil {
		return err
	}
	if _, err := runSystemctl(s, "daemon-reload"); err != nil {
		return err
	}
	if _, err := runSystemctl(s, "enable", "--now", s.Name); err != nil {
		return err
	}
	return nil
}

// Uninstall stops, disables and removes the unit.
func uninstall(s Spec) error {
	if err := s.validIdentity(); err != nil {
		return err
	}
	if s.NeedsRoot() && os.Geteuid() != 0 {
		return fmt.Errorf("uninstalling the %s scope needs root (try sudo)", s.Scope)
	}
	path, err := unitPath(s)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("service %s is not installed", s.Name)
	}
	// disable --now is best effort: a stopped or failed unit still gets
	// removed.
	_, _ = runSystemctl(s, "disable", "--now", s.Name)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	_, err = runSystemctl(s, "daemon-reload")
	return err
}

// status reports the service manager's view plus the unit file itself.
func status(s Spec) (Status, error) {
	if err := s.validIdentity(); err != nil {
		return Status{}, err
	}
	st := Status{Scope: s.Scope}
	path, err := unitPath(s)
	if err != nil {
		return st, err
	}
	st.UnitPath = path
	if _, err := os.Stat(path); err == nil {
		st.Installed = true
	}

	enabled, err := runSystemctl(s, "is-enabled", s.Name)
	if err != nil {
		// is-enabled exits non-zero for disabled/not-found; the message
		// itself is the interesting part.
		if strings.Contains(err.Error(), "No such") {
			return st, nil
		}
	}
	st.Enabled = enabled == "enabled" || strings.HasPrefix(enabled, "enabled")

	active, aerr := runSystemctl(s, "is-active", s.Name)
	if aerr == nil && active == "active" {
		st.Running = true
	}
	if !st.Installed {
		st.Detail = "服务未安装"
		return st, nil
	}
	switch {
	case st.Running:
		st.Detail = "服务运行中"
	case aerr != nil:
		st.Detail = "服务已安装但未运行"
	default:
		st.Detail = "服务已安装，状态 " + active
	}
	return st, nil
}

// LingerEnabled reports whether systemd lingering is on for the current
// user, which user-scope services need to start at boot without a login.
func LingerEnabled() (bool, error) {
	u, err := user.Current()
	if err != nil {
		return false, err
	}
	out, err := exec.Command("loginctl", "show-user", u.Username, "-p", "Linger").Output()
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) == "Linger=yes", nil
}

// easytier-mygo-svc is the resident management service: it runs a
// managed EasyTier core, checks core and instance health on a fixed
// cadence, records the health timeline, executes bounded recovery and
// answers the GUI over the local socket.
//
// It runs in three ways:
//
//	easytier-mygo-svc [flags]                run in the foreground
//	easytier-mygo-svc install [flags]        install as a systemd service
//	easytier-mygo-svc uninstall [--scope]    remove the service
//	easytier-mygo-svc service-status [--scope]
//
// The service manager watches the daemon process; the daemon watches the
// core. Each layer recovers only its own failures.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/configdoc"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/daemon"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/health"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/platformservice"
	productversion "github.com/YuanZhe-99/Easytier-MyGo/internal/version"
)

// version is the product version (internal/version).
var version = productversion.Version

const serviceName = "easytier-mygo-svc"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "delete-profile", "restore-profile":
			fs := flag.NewFlagSet(serviceName+" "+os.Args[1], flag.ExitOnError)
			state := fs.String("state-dir", "", "absolute offline state root")
			manifest := fs.String("network-manifest", "", "absolute network manifest")
			id := fs.String("profile-id", "", "stable profile ID")
			_ = fs.Parse(os.Args[2:])
			if err := daemon.SetProfileDeleted(*state, *manifest, *id, os.Args[1] == "delete-profile"); err != nil {
				log.Fatal(err)
			}
			return
		case "copy-profile":
			fs := flag.NewFlagSet(serviceName+" copy-profile", flag.ExitOnError)
			state := fs.String("state-dir", "", "absolute offline state root")
			manifest := fs.String("network-manifest", "", "absolute source manifest")
			id := fs.String("profile-id", "", "source stable profile ID")
			name := fs.String("name", "", "new profile name")
			rpc := fs.String("rpc", "", "new explicit loopback RPC")
			_ = fs.Parse(os.Args[2:])
			path, err := daemon.CopyProfile(*state, *manifest, *id, *name, *rpc)
			if err != nil {
				log.Fatal(err)
			}
			fmt.Println(path)
			return
		case "migrate-drafts":
			fs := flag.NewFlagSet(serviceName+" migrate-drafts", flag.ExitOnError)
			state := fs.String("state-dir", "", "absolute offline state root")
			manifest := fs.String("network-manifest", "", "absolute network manifest")
			configs := fs.String("config-dir", "", "absolute legacy config library; also the multi-network config root")
			id := fs.String("profile-id", "", "stable profile ID")
			_ = fs.Parse(os.Args[2:])
			if err := daemon.MigrateProfileDrafts(*state, *manifest, *configs, *id); err != nil {
				log.Fatal(err)
			}
			return
		case "rename-profile":
			fs := flag.NewFlagSet(serviceName+" rename-profile", flag.ExitOnError)
			state := fs.String("state-dir", "", "absolute offline state directory")
			manifest := fs.String("network-manifest", "", "absolute network manifest")
			id := fs.String("profile-id", "", "stable profile ID")
			name := fs.String("name", "", "new display name")
			_ = fs.Parse(os.Args[2:])
			if err := daemon.RenameProfile(*state, *manifest, *id, *name); err != nil {
				log.Fatal(err)
			}
			return
		case "migrate-profile":
			fs := flag.NewFlagSet(serviceName+" migrate-profile", flag.ExitOnError)
			state := fs.String("state-dir", "", "absolute offline legacy state directory")
			name := fs.String("name", "", "profile display name")
			rpc := fs.String("rpc", "", "explicit loopback RPC for migrated network")
			_ = fs.Parse(os.Args[2:])
			manifest, err := daemon.MigrateLegacyProfile(*state, *name, *rpc)
			if err != nil {
				log.Fatal(err)
			}
			fmt.Println(manifest)
			return
		case "install":
			runInstall(os.Args[2:])
			return
		case "uninstall":
			runUninstall(os.Args[2:])
			return
		case "service-status":
			runServiceStatus(os.Args[2:])
			return
		}
	}
	runDaemon(os.Args[1:])
}

// daemonFlags is the flag set shared by running and installing; install
// turns the values into the service's ExecStart arguments.
type daemonFlags struct {
	consoleLogLevel *string
	fileLogLevel    *string
	fileLogSize     *int
	fileLogCount    *int
	socketPath      *string
	socketGroup     *string
	readOthers      *bool
	stateDir        *string
	coreBin         *string
	cliBin          *string
	rpc             *string
	configFile      *string
	configDir       *string
	monitorOnly     *bool
	interval        *time.Duration
	noRecovery      *bool
	showVersion     *bool
	networkManifest *string
}

func newDaemonFlags(fs *flag.FlagSet) *daemonFlags {
	return &daemonFlags{
		consoleLogLevel: fs.String("console-log-level", "", "core console log level: off/error/warn/info/debug/trace"),
		fileLogLevel:    fs.String("file-log-level", "", "core rotating file log level; files stay in each profile state directory"),
		fileLogSize:     fs.Int("file-log-size", 0, "core per-file log size in MiB (1–100; zero uses core default)"),
		fileLogCount:    fs.Int("file-log-count", 0, "core rotating file count (1–10; zero uses core default)"),
		socketPath:      fs.String("socket", "", "local API socket (default: per-user runtime directory)"),
		socketGroup:     fs.String("socket-group", "", "system scope: authorization group; its members get write access (default: easytier-mygo)"),
		readOthers:      fs.Bool("read-others", false, "system scope: let other local users read status (never config text)"),
		stateDir:        fs.String("state-dir", "", "state directory for events and logs (default: per-user)"),
		coreBin:         fs.String("core-bin", "", "path to easytier-core (default: PATH)"),
		cliBin:          fs.String("cli-bin", "", "path to easytier-cli (default: PATH)"),
		rpc:             fs.String("rpc", "", "core management portal address (default 127.0.0.1:15888)"),
		configFile:      fs.String("config", "", "network config TOML; enables managed mode"),
		configDir:       fs.String("config-dir", "", "directory of managed config documents (default: per-user)"),
		monitorOnly:     fs.Bool("monitor-only", false, "watch a core this daemon did not start"),
		interval:        fs.Duration("check-interval", 5*time.Second, "health check cadence"),
		noRecovery:      fs.Bool("no-recovery", false, "monitor only: never restart the core"),
		showVersion:     fs.Bool("version", false, "print the version and exit"),
		networkManifest: fs.String("network-manifest", "", "multi-network JSON manifest (absolute path)"),
	}
}

// execArgs reconstructs the daemon invocation for a service definition,
// in a stable order, omitting empty values.
func (f *daemonFlags) execArgs() []string {
	var args []string
	add := func(flagName, value string) {
		if value != "" {
			args = append(args, "--"+flagName, value)
		}
	}
	add("core-bin", *f.coreBin)
	add("console-log-level", *f.consoleLogLevel)
	add("file-log-level", *f.fileLogLevel)
	if *f.fileLogSize != 0 {
		add("file-log-size", strconv.Itoa(*f.fileLogSize))
	}
	if *f.fileLogCount != 0 {
		add("file-log-count", strconv.Itoa(*f.fileLogCount))
	}
	add("cli-bin", *f.cliBin)
	add("rpc", *f.rpc)
	add("config", *f.configFile)
	add("network-manifest", *f.networkManifest)
	add("config-dir", *f.configDir)
	add("socket", *f.socketPath)
	add("socket-group", *f.socketGroup)
	add("state-dir", *f.stateDir)
	if *f.readOthers {
		args = append(args, "--read-others")
	}
	if *f.monitorOnly {
		args = append(args, "--monitor-only")
	}
	if *f.noRecovery {
		args = append(args, "--no-recovery")
	}
	if d := *f.interval; d > 0 && d != 5*time.Second {
		args = append(args, "--check-interval", d.String())
	}
	return args
}

func runDaemon(args []string) {
	fs := flag.NewFlagSet(serviceName, flag.ExitOnError)
	f := newDaemonFlags(fs)
	_ = fs.Parse(args)
	if *f.showVersion {
		fmt.Println(serviceName, version)
		return
	}

	policy := health.DefaultPolicy()
	if *f.noRecovery {
		policy.RecoveryEnabled = false
	}

	opts := daemon.Options{
		Logging:       daemon.LoggingOptions{ConsoleLevel: *f.consoleLogLevel, FileLevel: *f.fileLogLevel, FileSizeMB: *f.fileLogSize, FileCount: *f.fileLogCount},
		Version:       version,
		SocketPath:    *f.socketPath,
		StateDir:      firstNonEmpty(*f.stateDir, defaultStateDir()),
		CoreBin:       *f.coreBin,
		CLIBin:        *f.cliBin,
		RPC:           *f.rpc,
		ConfigFile:    *f.configFile,
		ConfigDir:     *f.configDir,
		MonitorOnly:   *f.monitorOnly,
		CheckInterval: *f.interval,
		Policy:        policy,
		ReadOthers:    *f.readOthers,
		Logf:          log.Printf,
	}
	// A system service runs as root but is administered by a non-root
	// group: resolve it now so the socket is created owned by that group.
	// Defaulting is explicit: root + no flag means the standard group; a
	// user-scope daemon keeps its owner-only socket.
	group := *f.socketGroup
	if group == "" && os.Geteuid() == 0 && *f.socketPath != "" {
		group = systemSocketGroup()
	}
	if gid, err := resolveSocketGroup(group); err != nil {
		log.Fatalf("%s: %v", serviceName, err)
	} else {
		opts.SocketGroup = gid
		if gid != 0 {
			log.Printf("%s: local API socket authorized by group %s (gid %d)", serviceName, group, gid)
		}
	}
	requireAbsolute := func(name, value string) {
		if value != "" && !filepath.IsAbs(value) {
			log.Printf("warning: %s %q is not absolute; systemd installs require absolute paths", name, value)
		}
	}
	requireAbsolute("core-bin", *f.coreBin)
	requireAbsolute("cli-bin", *f.cliBin)
	requireAbsolute("config", *f.configFile)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	mode := "managed"
	if opts.MonitorOnly || opts.ConfigFile == "" {
		mode = "monitor"
	}
	if *f.networkManifest != "" {
		mode = "multi-managed"
	}
	log.Printf("%s %s: mode=%s socket=%s state=%s", serviceName, version, mode, opts.SocketPath, opts.StateDir)
	if err := daemon.RunService(ctx, opts, *f.networkManifest); err != nil {
		log.Fatalf("%s: %v", serviceName, err)
	}
}

func runInstall(args []string) {
	fs := flag.NewFlagSet(serviceName+" install", flag.ExitOnError)
	f := newDaemonFlags(fs)
	scope := fs.String("scope", "user", "service scope: user (start at login) or system (start at boot)")
	_ = fs.Parse(args)
	// Which scope the caller actually asked for, before any advice.
	if err := (daemon.LoggingOptions{ConsoleLevel: *f.consoleLogLevel, FileLevel: *f.fileLogLevel, FileSizeMB: *f.fileLogSize, FileCount: *f.fileLogCount}).Validate(); err != nil {
		log.Fatal(err)
	}
	scopeExplicit := ""
	fs.Visit(func(fl *flag.Flag) {
		if fl.Name == "scope" {
			scopeExplicit = *scope
		}
	})

	exe, err := os.Executable()
	if err != nil {
		log.Fatalf("install: %v", err)
	}
	if strings.Contains(exe, "go-build") {
		log.Fatalf("install: this looks like a temporary 'go run' binary (%s); build the binary first (go build -o …)", exe)
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		log.Fatalf("install: %v", err)
	}

	hint, haveHint := configScopeHint(*f.configFile)
	if *f.networkManifest != "" {
		if *f.configFile != "" || *f.monitorOnly {
			log.Fatal("install: network manifest requires managed mode without --config")
		}
		needsRoot, err := daemon.NetworkManifestNeedsRoot(*f.networkManifest)
		if err != nil {
			log.Fatalf("install: %v", err)
		}
		hint = platformservice.UserModeHint{UserMode: !needsRoot}
		haveHint = true
	}
	// An unspecified scope follows the config: a user-mode core gets a
	// user service, a TUN core a system service. An explicit --scope wins.
	if scopeExplicit == "" && haveHint {
		rec, reason := platformservice.RecommendScope(hint)
		scope = ptr(string(rec))
		fmt.Printf("%s\n", reason)
	}

	sp := platformservice.Scope(*scope)
	// A system service is root-owned; the local API is authorized by a
	// group so the installing user (and other administrators) can drive
	// it without root shells. Defaulting is explicit: system scope means
	// the standard group unless the caller opted out with `-`.
	if sp == platformservice.ScopeSystem && *f.socketGroup == "" {
		*f.socketGroup = socketGroupName
	}
	spec := platformservice.Spec{
		Name:          serviceName,
		Description:   "EasyTier MyGo management service (core health and recovery)",
		Executable:    exe,
		Args:          f.execArgs(),
		Scope:         sp,
		SocketGroup:   *f.socketGroup,
		CheckInterval: *f.interval,
	}
	if err := spec.Valid(); err != nil {
		log.Fatalf("install: %v", err)
	}
	if sp == platformservice.ScopeSystem {
		if *f.socketPath == "" || *f.stateDir == "" {
			log.Fatalf("install: the system scope needs explicit --socket and --state-dir (for example --socket /run/easytier-mygo/ctl.sock --state-dir /var/lib/easytier-mygo)")
		}
		if *f.coreBin == "" || (*f.configFile == "" && *f.networkManifest == "") {
			log.Fatalf("install: the system scope needs explicit --core-bin and --config or --network-manifest")
		}
		// The group must exist before the daemon starts: the socket is
		// chowned to it at startup.
		if err := ensureSocketGroup(*f.socketGroup); err != nil {
			log.Fatalf("install: %v", err)
		}
	}
	if *f.configFile == "" && *f.networkManifest == "" && !*f.monitorOnly {
		log.Fatalf("install: --config, --network-manifest or --monitor-only is required")
	}
	if *f.networkManifest != "" && sp == platformservice.ScopeUser && !hint.UserMode {
		log.Fatal("install: a user network service requires no_tun=true for every profile")
	}

	// A mismatch between the scope and what the config needs is a warning,
	// not a refusal: the user may know better, but they should see it.
	if haveHint {
		if warn := platformservice.ScopeWarning(sp, hint); warn != "" {
			fmt.Println("Warning: " + warn)
		}
	}

	if err := platformservice.Install(spec); err != nil {
		log.Fatalf("install: %v", err)
	}
	fmt.Printf("Installed %s (%s scope) and started it.\n", spec.Name, spec.Scope)
	fmt.Printf("Watchdog: the service manager restarts the daemon if check rounds stop\n"+
		"          completing for %s (WatchdogSec, derived from the check interval).\n",
		platformservice.WatchdogTimeout(spec))
	if spec.Scope == platformservice.ScopeUser {
		if linger, err := platformservice.LingerEnabled(); err == nil && !linger {
			fmt.Println("Note: user services start at login; to start the network at boot without a login,")
			fmt.Println("      enable lingering once:  sudo loginctl enable-linger $USER")
		}
	}
	fmt.Println("Logs: systemctl --user status " + spec.Name + "  /  journalctl --user -u " + spec.Name + " -f")
}

func runUninstall(args []string) {
	fs := flag.NewFlagSet(serviceName+" uninstall", flag.ExitOnError)
	scope := fs.String("scope", "user", "service scope to remove")
	_ = fs.Parse(args)
	spec := platformservice.Spec{
		Name:  serviceName,
		Scope: platformservice.Scope(*scope),
	}
	// Executable/Args are unused for uninstall but Valid wants them.
	spec.Executable = "/bin/false"
	if err := platformservice.Uninstall(spec); err != nil {
		log.Fatalf("uninstall: %v", err)
	}
	fmt.Printf("Uninstalled %s (%s scope).\n", spec.Name, spec.Scope)
}

func runServiceStatus(args []string) {
	fs := flag.NewFlagSet(serviceName+" service-status", flag.ExitOnError)
	scope := fs.String("scope", "user", "service scope to report")
	_ = fs.Parse(args)
	if !platformservice.Supported() {
		fmt.Println("service management is not implemented on this platform yet")
		return
	}
	st, err := platformservice.Inspect(platformservice.Spec{
		Name:  serviceName,
		Scope: platformservice.Scope(*scope),
	})
	if err != nil {
		log.Fatalf("service-status: %v", err)
	}
	fmt.Printf("installed=%v running=%v enabled=%v scope=%s\n", st.Installed, st.Running, st.Enabled, st.Scope)
	fmt.Printf("unit=%s\n", st.UnitPath)
	if st.Detail != "" {
		fmt.Println(st.Detail)
	}
}

func defaultStateDir() string {
	if base := os.Getenv("XDG_STATE_HOME"); base != "" {
		return filepath.Join(base, "easytier-mygo")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".local", "state", "easytier-mygo")
	}
	return filepath.Join(os.TempDir(), "easytier-mygo")
}

// ensureSocketGroup makes sure the authorization group exists and that
// the user running the install is in it, so the GUI they launch can drive
// the service. Creating the group needs privilege, which the system-scope
// install already has.
func ensureSocketGroup(name string) error {
	if name == "" || name == "-" {
		return nil
	}
	if _, err := user.LookupGroup(name); err != nil {
		// groupadd is the portable way to add a system group.
		if out, gerr := exec.Command("groupadd", "--system", name).CombinedOutput(); gerr != nil {
			return fmt.Errorf("creating group %q: %v: %s", name, gerr, strings.TrimSpace(string(out)))
		}
		fmt.Printf("Created group %s.\n", name)
	}
	invoker := os.Getenv("SUDO_USER")
	if invoker == "" {
		return nil
	}
	if inGroup(invoker, name) {
		return nil
	}
	if out, err := exec.Command("gpasswd", "-a", invoker, name).CombinedOutput(); err != nil {
		return fmt.Errorf("adding %s to group %q: %v: %s", invoker, name, err, strings.TrimSpace(string(out)))
	}
	fmt.Printf("Added %s to group %s (log out and back in for it to take effect).\n", invoker, name)
	return nil
}

// inGroup reports whether the named user is a member of the group.
func inGroup(username, group string) bool {
	u, err := user.Lookup(username)
	if err != nil {
		return false
	}
	g, err := user.LookupGroup(group)
	if err != nil {
		return false
	}
	ids, err := u.GroupIds()
	if err != nil {
		return false
	}
	for _, id := range ids {
		if id == g.Gid {
			return true
		}
	}
	return false
}

// configScopeHint reads a config file and reports what it needs of a
// service scope. ok is false when the file is missing or unreadable, so
// the caller falls back to its own default.
func configScopeHint(path string) (platformservice.UserModeHint, bool) {
	if path == "" {
		return platformservice.UserModeHint{}, false
	}
	text, err := os.ReadFile(path)
	if err != nil {
		return platformservice.UserModeHint{}, false
	}
	v, err := configdoc.Extract(string(text))
	if err != nil {
		return platformservice.UserModeHint{}, false
	}
	return platformservice.UserModeHint{UserMode: v.Flags["no_tun"]}, true
}

// ptr returns a pointer to v; flag values are strings and the scope flag
// is set through its pointer.
func ptr(v string) *string { return &v }

// socketGroupName is the authorization group for a system-scope socket.
// Members may drive the management service; other local users are refused
// on a system socket.
const socketGroupName = "easytier-mygo"

// resolveSocketGroup turns the --socket-group value into a gid. An empty
// value on the system scope means the default group; an empty value
// otherwise (a per-user daemon) means no group and no chown.
func resolveSocketGroup(name string) (int, error) {
	if name == "" {
		return 0, nil
	}
	if name == "-" {
		// Explicitly opt out of group authorization.
		return 0, nil
	}
	g, err := user.LookupGroup(name)
	if err != nil {
		return 0, fmt.Errorf("socket group %q: %w (create it first: sudo groupadd %s)", name, err, name)
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		return 0, fmt.Errorf("socket group %q has a non-numeric gid %q", name, g.Gid)
	}
	return gid, nil
}

// systemSocketGroup is what the running daemon uses when it runs as a
// system service without an explicit --socket-group.
func systemSocketGroup() string {
	if os.Geteuid() == 0 {
		return socketGroupName
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

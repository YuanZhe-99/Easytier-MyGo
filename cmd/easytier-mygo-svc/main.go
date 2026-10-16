// easytier-mygo-svc is the EasyTier MyGo daemon: it owns the networks, runs
// one easytier-core per running network, monitors health, recovers local
// faults and serves the local API.
//
//	easytier-mygo-svc run --host user    the no-privileges daemon (started by the GUI)
//	easytier-mygo-svc run --host helper  the privileged helper (started by the OS)
//	easytier-mygo-svc version
//
// See doc/en-us/run-modes.md.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/daemon"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/health"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/localapi"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/sdnotify"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/version"
)

func main() {
	log.SetFlags(0)
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "run":
		if err := run(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
	case "version", "--version":
		fmt.Println(version.Version)
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: easytier-mygo-svc run --host user|helper [flags] | version")
	os.Exit(2)
}

// run starts the daemon in the foreground until a signal, the lifeline,
// the idle timer or a shutdown request ends it.
func run(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	host := fs.String("host", "user", "user (no privileges, child of the GUI) or helper (privileged)")
	stateDir := fs.String("state-dir", "", "state directory (default per host)")
	socket := fs.String("socket", "", "socket path (default per host)")
	coreBin := fs.String("core-bin", "", "easytier-core (default: next to this binary, else PATH)")
	cliBin := fs.String("cli-bin", "", "easytier-cli (default: next to this binary, else PATH)")
	rpcBase := fs.Int("rpc-base", 25888, "first port for the cores' management portals")
	lifeline := fs.Int("lifeline-fd", -1, "file descriptor whose EOF stops the daemon (user host)")
	idle := fs.Duration("idle-exit", 0, "exit after this long without sessions or running networks (helper)")
	fs.Parse(args)

	opts := daemon.Options{
		Version:     version.Version,
		CoreBin:     companion(*coreBin, "easytier-core"),
		CLIBin:      companion(*cliBin, "easytier-cli"),
		RPCPortBase: *rpcBase,
		IdleExit:    *idle,
		Policy:      health.DefaultPolicy(),
		Logf:        log.Printf,
	}
	opts.Policy.RecoveryEnabled = true
	switch *host {
	case "user":
		opts.Host = localapi.HostUser
		opts.StateDir = orDefault(*stateDir, userStateDir())
		opts.SocketPath = orDefault(*socket, localapi.UserSocketPath())
		opts.Authorizer = localapi.Authorizer{OwnerUID: uint32(os.Getuid()), Private: true}
		opts.Privileged = os.Geteuid() == 0
	case "helper":
		opts.Host = localapi.HostHelper
		opts.StateDir = orDefault(*stateDir, "/var/lib/easytier-mygo")
		opts.SocketPath = orDefault(*socket, localapi.SystemSocketPath)
		opts.Privileged = os.Geteuid() == 0
		authz, gid, err := helperAuthorizer(opts.StateDir)
		if err != nil {
			return err
		}
		opts.Authorizer, opts.SocketGroup = authz, gid
		lns, err := sdnotify.Listeners()
		if err != nil {
			return fmt.Errorf("socket activation: %w", err)
		}
		if len(lns) > 0 {
			opts.Listener = lns[0]
		}
	default:
		return fmt.Errorf("unknown host %q", *host)
	}
	if *lifeline >= 0 {
		opts.Lifeline = os.NewFile(uintptr(*lifeline), "lifeline")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if n := sdnotify.FromEnv(); n != nil {
		go func() {
			time.Sleep(500 * time.Millisecond)
			_ = n.Ready()
		}()
	}
	return daemon.Run(ctx, opts)
}

// companion resolves a helper binary: the flag, else the file next to this
// executable (the bundle layout), else the name for a PATH lookup.
func companion(flagValue, name string) string {
	if flagValue != "" {
		return flagValue
	}
	if exe, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(exe), name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return name
}

func orDefault(v, def string) string {
	if v != "" {
		return v
	}
	return def
}

// userStateDir is $XDG_STATE_HOME/easytier-mygo (~/.local/state/easytier-mygo).
func userStateDir() string {
	if base := os.Getenv("XDG_STATE_HOME"); base != "" {
		return filepath.Join(base, "easytier-mygo")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "easytier-mygo")
}

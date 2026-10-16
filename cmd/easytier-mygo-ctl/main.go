// easytier-mygo-ctl is a small scriptable client of the management
// daemon's local API: status snapshots, a forced check, core start/stop,
// recovery budget renewal and daemon shutdown. The GUI uses the same
// socket; this command exists for development, testing and headless use.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/localapi"
)

func main() {
	fs := flag.NewFlagSet("easytier-mygo-ctl", flag.ExitOnError)
	socket := fs.String("socket", "", "daemon socket (default: per-user path)")
	timeout := fs.Duration("timeout", 5*time.Second, "command timeout")
	wantDetails := fs.Bool("want-details", false, "status: tell the daemon this caller has a window on screen (keeps detail queries at the cadence)")
	wantTraffic := fs.Bool("want-traffic", false, "status: tell the daemon this caller displays the traffic curve (raises the sampling tier)")
	repeat := fs.Int("repeat", 1, "status: number of requests, one per second (scripted polling)")
	profile := fs.String("profile", "", "explicit profile target for status/check/start/stop")
	_ = fs.Parse(os.Args[1:])

	client := localapi.NewClient(*socket)
	client.Timeout = *timeout
	client.SetWantDetails(*wantDetails)
	client.SetWantTraffic(*wantTraffic)
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	cmd := fs.Arg(0)
	if cmd == "rename-profile" {
		if *profile == "" || fs.Arg(1) == "" {
			fmt.Fprintln(os.Stderr, "rename-profile requires --profile ID and a display name")
			os.Exit(2)
		}
		dumpStatusAction(client, ctx, func() (*localapi.Status, error) { return client.RenameProfile(ctx, *profile, fs.Arg(1)) })
		return
	}
	if cmd == "networks" {
		states, err := client.Networks(ctx)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(states); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if *profile != "" {
		kinds := map[string]localapi.RequestType{"status": localapi.RequestStatus, "check": localapi.RequestCheck, "start": localapi.RequestCoreStart, "stop": localapi.RequestCoreStop}
		kind, ok := kinds[cmd]
		if !ok {
			fmt.Fprintln(os.Stderr, "--profile supports status/check/start/stop")
			os.Exit(2)
		}
		dumpStatusAction(client, ctx, func() (*localapi.Status, error) { return client.Target(ctx, *profile, kind) })
		return
	}
	if cmd == "" {
		fmt.Fprintln(os.Stderr, "usage: easytier-mygo-ctl [-socket path] status|check|start|stop|grant|shutdown|configs|config-get|config-save|config-validate|config-activate")
		os.Exit(2)
	}

	switch cmd {
	case "diagnostics":
		diagnostic, err := client.Diagnostics(ctx)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(diagnostic); err != nil {
			fmt.Fprintln(os.Stderr, "encode:", err)
			os.Exit(1)
		}
		return
	case "configs":
		dumpConfigs(client, ctx)
		return
	case "config-get":
		dumpConfigGet(client, ctx, fs.Arg(1))
		return
	case "config-save":
		dumpConfigSave(client, ctx, fs.Arg(1), fs.Arg(2), fs.Arg(3))
		return
	case "config-validate":
		dumpConfigValidate(client, ctx, fs.Arg(1), fs.Arg(2))
		return
	case "config-activate":
		dumpStatusAction(client, ctx, func() (*localapi.Status, error) { return client.ConfigActivate(ctx, fs.Arg(1)) })
		return
	}

	var (
		st  *localapi.Status
		err error
	)
	switch cmd {
	case "status":
		for i := 0; i < *repeat; i++ {
			if i > 0 {
				time.Sleep(time.Second)
			}
			// A fresh deadline per request: scripted polling may outlive
			// a single timeout.
			reqCtx, reqCancel := context.WithTimeout(context.Background(), *timeout)
			st, err = client.Status(reqCtx)
			reqCancel()
			if err != nil {
				break
			}
		}
	case "check":
		st, err = client.Check(ctx)
	case "start":
		st, err = client.StartCore(ctx)
	case "stop":
		st, err = client.StopCore(ctx)
	case "grant":
		component := fs.Arg(1)
		if component == "" {
			fmt.Fprintln(os.Stderr, "grant needs a component, e.g. core/core")
			os.Exit(2)
		}
		st, err = client.GrantRetry(ctx, component)
	case "shutdown":
		err = client.Shutdown(ctx)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", cmd)
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	if st != nil {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(st); err != nil {
			fmt.Fprintln(os.Stderr, "encode:", err)
			os.Exit(1)
		}
	} else {
		fmt.Println("ok")
	}
}

func dumpConfigs(client *localapi.Client, ctx context.Context) {
	st, err := client.Status(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(map[string]any{
		"config_dir":    st.ConfigDir,
		"active_config": st.ActiveConfig,
		"configs":       st.Configs,
	})
}

func dumpConfigGet(client *localapi.Client, ctx context.Context, name string) {
	if name == "" {
		fmt.Fprintln(os.Stderr, "config-get needs a name")
		os.Exit(2)
	}
	payload, err := client.ConfigGet(ctx, name)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	// Raw config to stdout so scripts can redirect it; verdict to stderr.
	if !payload.Valid {
		fmt.Fprintln(os.Stderr, "invalid:", payload.ParseError)
	}
	fmt.Print(payload.Content)
}

func dumpConfigSave(client *localapi.Client, ctx context.Context, name, file, expected string) {
	if name == "" || file == "" {
		fmt.Fprintln(os.Stderr, "config-save needs a name and a local file")
		os.Exit(2)
	}
	content, err := os.ReadFile(file)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	payload, err := client.ConfigSave(ctx, name, string(content), expected)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	fmt.Printf("saved %s revision=%s valid=%v\n", payload.Entry.Name, payload.Entry.Revision, payload.Valid)
	if !payload.Valid {
		fmt.Fprintln(os.Stderr, "parse error:", payload.ParseError)
		os.Exit(1)
	}
}

func dumpConfigValidate(client *localapi.Client, ctx context.Context, name, file string) {
	if name == "" || file == "" {
		fmt.Fprintln(os.Stderr, "config-validate needs a name and a local file")
		os.Exit(2)
	}
	content, err := os.ReadFile(file)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	payload, err := client.ConfigValidate(ctx, name, string(content), true)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	fmt.Printf("valid=%v core_check=%v core_ok=%v core_note=%s\n",
		payload.Valid, payload.CoreCheck, payload.CoreCheckOK, payload.CoreCheckNote)
	if payload.ParseError != "" {
		fmt.Fprintln(os.Stderr, "parse error:", payload.ParseError)
	}
	if !payload.Valid || (payload.CoreCheck && !payload.CoreCheckOK) {
		os.Exit(1)
	}
}

func dumpStatusAction(client *localapi.Client, ctx context.Context, run func() (*localapi.Status, error)) {
	st, err := run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	fmt.Printf("ok; active_config=%s\n", st.ActiveConfig)
}

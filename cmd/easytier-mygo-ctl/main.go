// easytier-mygo-ctl is a command-line client of the daemon's local API.
//
//	easytier-mygo-ctl [-socket PATH] list
//	easytier-mygo-ctl create NAME FILE.toml
//	easytier-mygo-ctl connect|disconnect|apply|retry|delete ID
//	easytier-mygo-ctl detail ID
//	easytier-mygo-ctl config ID              print the document
//	easytier-mygo-ctl save ID FILE.toml      save (conflict-checked)
//	easytier-mygo-ctl events | info
//	easytier-mygo-ctl watch [ID...]          attach a session and print frames
//	easytier-mygo-ctl background on|off      helper only
//	easytier-mygo-ctl cores | releases [--pre] | install-core TAG | remove-core TAG | use-core TAG
//
// Without -socket it uses the privileged helper's socket when it exists,
// else the per-user socket.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"text/tabwriter"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/localapi"
)

func main() {
	socket := flag.String("socket", "", "daemon socket")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: easytier-mygo-ctl [-socket PATH] list|info|events|create|connect|disconnect|apply|retry|delete|detail|config|save|watch|background ...")
	}
	flag.Parse()
	args := flag.Args()
	if len(args) == 0 {
		flag.Usage()
		os.Exit(2)
	}
	path := *socket
	if path == "" {
		path = localapi.UserSocketPath()
		if _, err := os.Stat(localapi.SystemSocketPath); err == nil {
			path = localapi.SystemSocketPath
		}
	}
	c := localapi.NewClient(path)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := runCmd(ctx, c, args); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func need(args []string, n int) {
	if len(args) < n {
		flag.Usage()
		os.Exit(2)
	}
}

func runCmd(ctx context.Context, c *localapi.Client, args []string) error {
	do := func(req localapi.Request) (*localapi.Response, error) { return c.Do(ctx, req) }
	switch args[0] {
	case "list":
		r, err := do(localapi.Request{Type: localapi.RequestNetworks})
		if err != nil {
			return err
		}
		printNetworks(r.Networks)
	case "info":
		r, err := do(localapi.Request{Type: localapi.RequestInfo})
		if err != nil {
			return err
		}
		return printJSON(r.Info)
	case "events":
		r, err := do(localapi.Request{Type: localapi.RequestEvents})
		if err != nil {
			return err
		}
		return printJSON(r.Events)
	case "create":
		need(args, 3)
		text, err := os.ReadFile(args[2])
		if err != nil {
			return err
		}
		r, err := do(localapi.Request{Type: localapi.RequestNetworkCreate, Name: args[1], Text: string(text)})
		if err != nil {
			return err
		}
		printNetworks(r.Networks)
	case "connect", "disconnect", "apply", "retry", "delete":
		need(args, 2)
		types := map[string]localapi.RequestType{"connect": localapi.RequestConnect, "disconnect": localapi.RequestDisconnect,
			"apply": localapi.RequestApply, "retry": localapi.RequestRetry, "delete": localapi.RequestNetworkDelete}
		r, err := do(localapi.Request{Type: types[args[0]], Network: args[1]})
		if err != nil {
			return err
		}
		printNetworks(r.Networks)
	case "detail":
		need(args, 2)
		r, err := do(localapi.Request{Type: localapi.RequestDetail, Network: args[1]})
		if err != nil {
			return err
		}
		return printJSON(r.Detail)
	case "config":
		need(args, 2)
		r, err := do(localapi.Request{Type: localapi.RequestConfigGet, Network: args[1]})
		if err != nil {
			return err
		}
		fmt.Print(r.Config.Text)
	case "save":
		need(args, 3)
		text, err := os.ReadFile(args[2])
		if err != nil {
			return err
		}
		cur, err := do(localapi.Request{Type: localapi.RequestConfigGet, Network: args[1]})
		if err != nil {
			return err
		}
		_, err = do(localapi.Request{Type: localapi.RequestConfigSave, Network: args[1], Text: string(text), Expected: cur.Config.Revision})
		return err
	case "cores":
		r, err := do(localapi.Request{Type: localapi.RequestCores})
		if err != nil {
			return err
		}
		for _, c := range r.Cores {
			mark := " "
			if c.Selected {
				mark = "*"
			}
			fmt.Printf("%s %-12s %-9s tested=%v pre=%v\n", mark, c.Tag, c.Source, c.Tested, c.Pre)
		}
	case "releases":
		r, err := do(localapi.Request{Type: localapi.RequestReleases, Pre: len(args) > 1 && args[1] == "--pre"})
		if err != nil {
			return err
		}
		for _, rel := range r.Releases {
			fmt.Printf("%-10s %s pre=%v installed=%v tested=%v %.1f MB\n", rel.Tag, rel.Published.Format("2006-01-02"), rel.Prerelease, rel.Installed, rel.Tested, float64(rel.Size)/1e6)
		}
	case "install-core", "remove-core":
		need(args, 2)
		t := localapi.RequestCoreInstall
		if args[0] == "remove-core" {
			t = localapi.RequestCoreRemove
		}
		_, err := do(localapi.Request{Type: t, Tag: args[1]})
		return err
	case "helper-update":
		need(args, 2)
		_, err := do(localapi.Request{Type: localapi.RequestHelperUpdate, Text: args[1]})
		return err
	case "add-core":
		need(args, 3)
		_, err := do(localapi.Request{Type: localapi.RequestCoreAdd, Name: args[1], Text: args[2]})
		return err
	case "use-core":
		need(args, 2)
		cur, err := do(localapi.Request{Type: localapi.RequestSettingsGet})
		if err != nil {
			return err
		}
		s := *cur.Settings
		s.Core = args[1]
		_, err = do(localapi.Request{Type: localapi.RequestSettingsSet, Settings: &s})
		return err
	case "background":
		need(args, 2)
		cur, err := do(localapi.Request{Type: localapi.RequestSettingsGet})
		if err != nil {
			return err
		}
		s := *cur.Settings
		s.Background = args[1] == "on"
		_, err = do(localapi.Request{Type: localapi.RequestSettingsSet, Settings: &s})
		return err
	case "watch":
		return c.Attach(ctx, args[1:], func(p *localapi.Push) error {
			if p.Session != "" {
				fmt.Println("session", p.Session)
			}
			printNetworks(p.Networks)
			for id, pts := range p.Traffic {
				last := pts[len(pts)-1]
				fmt.Printf("  traffic %s: +%d points, rx %.0f B/s tx %.0f B/s\n", id, len(pts), last.Rx, last.Tx)
			}
			for _, e := range p.Events {
				fmt.Printf("  event %s %s %s\n", e.Time.Format("15:04:05"), e.Kind, e.Reason)
			}
			return nil
		})
	default:
		flag.Usage()
		os.Exit(2)
	}
	return nil
}

func printNetworks(ns []localapi.NetworkStatus) {
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tSTATE\tVIRTUAL IP\tPEERS\tRX/TX B/s\tNOTE")
	for _, n := range ns {
		note := n.Error
		if note == "" && n.Reason != "" {
			note = n.Reason
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\t%.0f/%.0f\t%s\n", n.ID, n.Name, n.State, n.VirtualIPv4, n.PeerCount, n.RxRate, n.TxRate, note)
	}
	tw.Flush()
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

package gui

import (
	"slices"
	"testing"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/certmanager"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/localapi"
	L "github.com/YuanZhe-99/Easytier-MyGo/locales"
)

// TestJobFromDialog: the dialog changes only what it shows; settings made
// in the CLI (file names, modes, an argument-list command, a lifetime the
// dialog does not offer) survive an edit.
func TestJobFromDialog(t *testing.T) {
	nets := []localapi.NetworkStatus{{ID: "n1", Name: "Home"}, {ID: "n2", Name: "Lab"}}
	cli := certmanager.Job{ID: "web", KeyFile: "web.key", KeyMode: "0640", ValidityDays: 60,
		Hook: certmanager.Hook{Enabled: true, Args: []string{"systemctl", "reload", "nginx"}}}
	cases := []struct {
		name string
		d    certJobDialog
		ok   bool
		want func(j certmanager.Job) bool
	}{
		{"network and names", certJobDialog{network: "Lab", dns: "a.et.net, *.b.et.net", ips: "10.0.0.1 10.0.0.2", days: L.CertDays("180")}, true,
			func(j certmanager.Job) bool {
				return j.Network == "n2" && slices.Equal(j.DNSNames, []string{"a.et.net", "*.b.et.net"}) &&
					slices.Equal(j.IPs, []string{"10.0.0.1", "10.0.0.2"}) && j.ValidityDays == 180 && !j.Hook.Enabled
			}},
		{"nothing to name", certJobDialog{network: L.CertNetworkNone(), days: L.CertDays("90")}, false, nil},
		{"edit keeps the CLI's settings", certJobDialog{job: cli, editing: true, network: "Home", days: L.CertDays("60")}, true,
			func(j certmanager.Job) bool {
				return j.ID == "web" && j.Network == "n1" && j.KeyFile == "web.key" && j.KeyMode == "0640" &&
					j.ValidityDays == 60 && slices.Equal(j.Hook.Args, cli.Hook.Args)
			}},
		{"a typed command replaces arguments", certJobDialog{job: cli, editing: true, network: "Home", days: L.CertDays("90"), hook: " caddy reload "}, true,
			func(j certmanager.Job) bool {
				return j.Hook.Enabled && j.Hook.Command == "caddy reload" && j.Hook.Args == nil && j.ValidityDays == 90
			}},
		{"clearing the command turns it off", certJobDialog{job: certmanager.Job{DNSNames: []string{"x.et.net"},
			Hook: certmanager.Hook{Enabled: true, Command: "caddy reload"}}, editing: true, dns: "x.et.net", network: L.CertNetworkNone(), days: L.CertDays("90")}, true,
			func(j certmanager.Job) bool { return !j.Hook.Enabled && j.Hook.Command == "" && j.Network == "" }},
	}
	for _, c := range cases {
		j, ok := jobFromDialog(&c.d, nets)
		if ok != c.ok || (ok && !c.want(j)) {
			t.Errorf("%s: ok=%v %+v", c.name, ok, j)
		}
	}
}

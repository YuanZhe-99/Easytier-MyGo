package configdoc

import (
	"slices"
	"strings"
	"testing"
)

const portal27 = `instance_name = "x"

[vpn_portal_config]
wireguard_listen = "0.0.0.0:11013"
wireguard_private_key = "k"

[[vpn_portal_config.clients]]
name = "phone"
virtual_ip = "10.14.14.2/24"

[[vpn_portal_config.clients]]
name = "laptop"
virtual_ip = "10.14.14.3/24"
groups = [ "ops" ]
`

// The EasyTier 2.7 portal is read with its clients and written back
// without touching anything else; removing it removes the clients too.
func TestVpnPortal27(t *testing.T) {
	v, err := Extract(portal27)
	if err != nil {
		t.Fatal(err)
	}
	p := v.VpnPortal
	if p == nil || p.Legacy() || p.PrivateKey != "k" || len(p.Clients) != 2 || p.Clients[1].Groups[0] != "ops" {
		t.Fatalf("portal = %+v", p)
	}
	cur := v
	np := *p
	np.Clients = append(slices.Clone(p.Clients), VpnPortalClient{Name: "tv", VirtualIP: "10.14.14.4/24"})
	cur.VpnPortal = &np
	out, err := ApplyValues(portal27, Changes(v, cur))
	if err != nil {
		t.Fatal(err)
	}
	back, err := Extract(out)
	if err != nil || len(back.VpnPortal.Clients) != 3 || back.VpnPortal.PrivateKey != "k" || *back.InstanceName != "x" {
		t.Fatalf("after adding a client: %v\n%s", err, out)
	}
	cur.VpnPortal = nil
	out, err = ApplyValues(portal27, Changes(v, cur))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "vpn_portal") || !strings.Contains(out, `instance_name = "x"`) {
		t.Fatalf("after removing the portal:\n%s", out)
	}
}

// The 2.7 scalars and lists round-trip through the form projection.
func TestNewSettings27(t *testing.T) {
	text := "[flags]\nsocket_mark = 7\nprefer_peer_relay = true\n"
	v, err := Extract(text)
	if err != nil || v.SocketMark == nil || *v.SocketMark != 7 || !v.Flags["prefer_peer_relay"] {
		t.Fatalf("extract: %+v %v", v, err)
	}
	cur := v
	mark, tcp := uint32(9), []string{"tcp://stun.example:3478"}
	cur.SocketMark, cur.TCPStunServers = &mark, &tcp
	out, err := ApplyValues(text, Changes(v, cur))
	if err != nil {
		t.Fatal(err)
	}
	back, err := Extract(out)
	if err != nil || *back.SocketMark != 9 || len(*back.TCPStunServers) != 1 {
		t.Fatalf("round trip: %v\n%s", err, out)
	}
}

func TestStripForMini(t *testing.T) {
	cases := []struct {
		name, in string
		want     []string
		keep     []string // substrings that must survive
		gone     []string // substrings that must be removed
	}{
		{
			name: "already mini",
			in:   "# mine\nlisteners = [ \"tcp://0.0.0.0:11010\", \"udp://0.0.0.0:11010\" ]\n\n[[peer]]\nuri = \"tcp://a:11010\"\n",
			keep: []string{"# mine", "udp://0.0.0.0:11010", "tcp://a:11010"},
		},
		{
			name: "everything",
			in: `# keep me
listeners = [ "tcp://0.0.0.0:11010", "quic://0.0.0.0:11012", "wg://0.0.0.0:11011" ]
exit_nodes = [ "10.0.0.1" ]
socks5_proxy = "socks5://0.0.0.0:1080"

[flags]
accept_dns = true
enable_kcp_proxy = false
enable_quic_proxy = true
data_compress_algo = 2
encryption_algorithm = "chacha20"
latency_first = true

[[peer]]
uri = "tcp://a:11010"
peer_public_key = "pk"

[[peer]]
uri = "wss://b:443"

[[proxy_network]]
cidr = "192.168.1.0/24"

[[port_forward]]
bind_addr = "0.0.0.0:80"
dst_addr = "10.0.0.2:80"
proto = "tcp"

[vpn_portal_config]
client_cidr = "10.14.14.0/24"
wireguard_listen = "0.0.0.0:11013"
`,
			want: []string{MiniListeners, MiniPeers, MiniSubnetProxy, MiniExitNodes, MiniSocks5, MiniPortForward,
				MiniVpnPortal, MiniMagicDNS, MiniQUIC, MiniCompression, MiniEncryption},
			keep: []string{"# keep me", "tcp://0.0.0.0:11010", "tcp://a:11010", "peer_public_key", "latency_first = true", "enable_kcp_proxy = false"},
			gone: []string{"quic://", "wg://", "wss://", "exit_nodes", "socks5", "accept_dns", "enable_quic_proxy",
				"data_compress_algo", "chacha20", "proxy_network", "port_forward", "vpn_portal"},
		},
		{name: "2.7 portal", in: portal27, want: []string{MiniVpnPortal}, keep: []string{`instance_name = "x"`}, gone: []string{"phone", "vpn_portal"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, ids, err := StripForMini(c.in)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(ids, c.want) {
				t.Errorf("ids = %v, want %v", ids, c.want)
			}
			if len(c.want) == 0 && out != c.in {
				t.Errorf("a mini-ready document changed:\n%s", out)
			}
			if _, err := Parse(out); err != nil {
				t.Fatalf("result does not parse: %v\n%s", err, out)
			}
			for _, s := range c.keep {
				if !strings.Contains(out, s) {
					t.Errorf("lost %q:\n%s", s, out)
				}
			}
			for _, s := range c.gone {
				if strings.Contains(out, s) {
					t.Errorf("kept %q:\n%s", s, out)
				}
			}
			if again, _ := MiniUnsupported(out); len(again) != 0 {
				t.Errorf("still unsupported after stripping: %v", again)
			}
		})
	}
}

func TestVersionConflicts(t *testing.T) {
	legacy := "[vpn_portal_config]\nclient_cidr = \"10.14.14.0/24\"\nwireguard_listen = \"0.0.0.0:11013\"\n"
	newer := "tcp_stun_servers = [ \"a\" ]\n" + portal27 + "\n[flags]\nsocket_mark = 1\n"
	cases := []struct {
		name string
		text string
		v27  bool
		want []string
	}{
		{"legacy on 2.6", legacy, false, nil},
		{"legacy on 2.7", legacy, true, []string{LegacyPortal}},
		{"newer on 2.7", newer, true, nil},
		{"newer on 2.6", newer, false, []string{NewPortal, TCPStunServers, SocketMark}},
		{"plain", "instance_name = \"x\"\n", false, nil},
	}
	for _, c := range cases {
		got, err := VersionConflicts(c.text, c.v27)
		if err != nil || !slices.Equal(got, c.want) {
			t.Errorf("%s: %v %v, want %v", c.name, got, err, c.want)
		}
	}
}

func TestAtLeast27(t *testing.T) {
	for tag, want := range map[string]bool{
		"v2.6.4": false, "2.6.4-abc": false, "v2.7.0": true, "2.7.0-8b7f1f01": true,
		"v3.0.0": true, "v2.10.1": true, "custom": true, "": true,
	} {
		if got := AtLeast27(tag); got != want {
			t.Errorf("AtLeast27(%q) = %v, want %v", tag, got, want)
		}
	}
}

package daemon

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// profileResources is a read-only projection. Unverified resources never imply safety.
type profileResources struct {
	ID         string
	Ports      []boundPort
	Device     string
	Address    string
	Unverified []string
}

type boundPort struct {
	Field     string
	Transport string
	Address   netip.Addr
	Port      uint16
}

type resourceConflict struct {
	FirstID, SecondID       string
	FirstField, SecondField string
	Resource                string
	Possible                bool
}

func parseBoundPort(field, transport, endpoint string) (boundPort, error) {
	host, rawPort, err := net.SplitHostPort(endpoint)
	if err != nil {
		return boundPort{}, fmt.Errorf("%s: invalid bind endpoint", field)
	}
	port, err := strconv.ParseUint(rawPort, 10, 16)
	if err != nil {
		return boundPort{}, fmt.Errorf("%s: invalid bind port", field)
	}
	if host == "" {
		host = "0.0.0.0"
	}
	address, err := netip.ParseAddr(host)
	if err != nil {
		return boundPort{}, fmt.Errorf("%s: bind hostname resolution is unverified", field)
	}
	return boundPort{Field: field, Transport: transport, Address: address.Unmap(), Port: uint16(port)}, nil
}

func projectResources(id, text, rpc string) (profileResources, error) {
	r := profileResources{ID: id}
	var doc struct {
		Listeners []string `toml:"listeners"`
		Socks5    string   `toml:"socks5_proxy"`
		IPv4      string   `toml:"ipv4"`
		NetNS     string   `toml:"netns"`
		Flags     struct {
			NoTun   bool   `toml:"no_tun"`
			DevName string `toml:"dev_name"`
		} `toml:"flags"`
		VPN struct {
			Listen string `toml:"wireguard_listen"`
		} `toml:"vpn_portal_config"`
		Forwards []struct {
			Bind  string `toml:"bind_addr"`
			Proto string `toml:"proto"`
		} `toml:"port_forward"`
	}
	if err := toml.Unmarshal([]byte(text), &doc); err != nil {
		return r, fmt.Errorf("invalid resource configuration")
	}
	add := func(field, transport, endpoint string) {
		p, err := parseBoundPort(field, transport, endpoint)
		if err != nil {
			r.Unverified = append(r.Unverified, field)
			return
		}
		r.Ports = append(r.Ports, p)
	}
	if rpc != "" {
		add("rpc", "tcp", rpc)
	}
	for i, listener := range doc.Listeners {
		field := fmt.Sprintf("listeners[%d]", i)
		u, err := url.Parse(listener)
		if err != nil {
			r.Unverified = append(r.Unverified, field)
			continue
		}
		transport := u.Scheme
		switch transport {
		case "tcp", "ws", "wss":
			transport = "tcp"
		case "udp", "quic":
			transport = "udp"
		default:
			r.Unverified = append(r.Unverified, field)
			continue
		}
		add(field, transport, u.Host)
	}
	if doc.Socks5 != "" {
		u, err := url.Parse(doc.Socks5)
		if err != nil || u.Scheme != "socks5" {
			r.Unverified = append(r.Unverified, "socks5_proxy")
		} else {
			add("socks5_proxy", "tcp", u.Host)
		}
	}
	if doc.VPN.Listen != "" {
		add("vpn_portal_config.wireguard_listen", "udp", doc.VPN.Listen)
	}
	for i, f := range doc.Forwards {
		field := fmt.Sprintf("port_forward[%d].bind_addr", i)
		if f.Proto != "tcp" && f.Proto != "udp" {
			r.Unverified = append(r.Unverified, field)
			continue
		}
		add(field, f.Proto, f.Bind)
	}
	if !doc.Flags.NoTun {
		r.Device = doc.Flags.DevName
		if r.Device == "" {
			r.Unverified = append(r.Unverified, "flags.dev_name")
		}
	}
	if doc.IPv4 != "" {
		addr, err := netip.ParseAddr(strings.Split(doc.IPv4, "/")[0])
		if err != nil {
			r.Unverified = append(r.Unverified, "ipv4")
		} else {
			r.Address = addr.String()
		}
	}
	if doc.NetNS != "" {
		r.Unverified = append(r.Unverified, "netns")
	}
	return r, nil
}

func portOverlap(a, b boundPort) (bool, bool) {
	if a.Port == 0 || b.Port == 0 || a.Port != b.Port || a.Transport != b.Transport {
		return false, false
	}
	if a.Address == b.Address {
		return true, false
	}
	if a.Address.Is4() == b.Address.Is4() {
		return a.Address.IsUnspecified() || b.Address.IsUnspecified(), false
	}
	// IPv6 wildcard may also accept IPv4 depending on socket options.
	return a.Address.IsUnspecified() || b.Address.IsUnspecified(), true
}

func resourceConflicts(a, b profileResources) []resourceConflict {
	var conflicts []resourceConflict
	for _, first := range a.Ports {
		for _, second := range b.Ports {
			if overlap, possible := portOverlap(first, second); overlap {
				conflicts = append(conflicts, resourceConflict{a.ID, b.ID, first.Field, second.Field, first.Transport + ":" + strconv.Itoa(int(first.Port)), possible})
			}
		}
	}
	if a.Device != "" && a.Device == b.Device {
		conflicts = append(conflicts, resourceConflict{a.ID, b.ID, "flags.dev_name", "flags.dev_name", "tun-device", false})
	}
	if a.Address != "" && a.Address == b.Address {
		conflicts = append(conflicts, resourceConflict{a.ID, b.ID, "ipv4", "ipv4", "static-address", true})
	}
	return conflicts
}

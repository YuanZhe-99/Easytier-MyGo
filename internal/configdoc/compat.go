package configdoc

// What a core can run: the settings EasyTier mini drops, and the settings
// that need a newer or older EasyTier than the one a network runs on
// (doc/en-us/easytier-upstream.md). Features are named by stable IDs the
// GUI turns into words.

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
)

// Feature IDs of settings EasyTier mini does not run.
const (
	MiniListeners       = "listeners"        // listeners other than tcp:// and udp://
	MiniMappedListeners = "mapped_listeners" // the same for mapped listeners
	MiniPeers           = "peers"            // peers other than tcp:// and udp://
	MiniSubnetProxy     = "proxy_network"
	MiniExitNodes       = "exit_nodes" // using exit nodes, or being one
	MiniSocks5          = "socks5_proxy"
	MiniPortForward     = "port_forward"
	MiniVpnPortal       = "vpn_portal"
	MiniIPv6Public      = "ipv6_public_addr"
	MiniMagicDNS        = "magic_dns"
	MiniKCP             = "kcp"
	MiniQUIC            = "quic"
	MiniUDPBroadcast    = "udp_broadcast"
	MiniCompression     = "compression"
	MiniEncryption      = "encryption" // an algorithm other than AES-GCM
)

// miniFlags are flags.* switches EasyTier mini forces off, with the
// feature each one belongs to. Only an explicit true is a setting mini
// drops; the disable_* switches it forces on are not listed.
var miniFlags = map[string]string{
	"enable_exit_node":                  MiniExitNodes,
	"accept_dns":                        MiniMagicDNS,
	"enable_kcp_proxy":                  MiniKCP,
	"enable_relay_foreign_network_kcp":  MiniKCP,
	"enable_quic_proxy":                 MiniQUIC,
	"enable_relay_foreign_network_quic": MiniQUIC,
	"enable_udp_broadcast_relay":        MiniUDPBroadcast,
}

// MiniScheme reports whether EasyTier mini runs a listener or peer URL:
// only tcp:// and udp:// (it ignores the others).
func MiniScheme(uri string) bool {
	u, err := url.Parse(strings.TrimSpace(uri))
	if err != nil {
		return false
	}
	s := strings.ToLower(u.Scheme)
	return s == "tcp" || s == "udp"
}

// MiniUnsupported lists the settings of a document EasyTier mini would
// silently ignore, as feature IDs in a fixed order.
func MiniUnsupported(text string) ([]string, error) {
	_, ids, err := StripForMini(text)
	return ids, err
}

// StripForMini removes from a document what EasyTier mini would ignore,
// so a copy made for mini shows what actually runs.
// Returns: the new text and the feature IDs removed (none: text unchanged).
// Notes: everything else, comments included, stays as it was.
func StripForMini(text string) (string, []string, error) {
	var ids []string
	add := func(id string) {
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	var err error
	step := func(f func() (string, error)) {
		if err == nil {
			text, err = f()
		}
	}
	// Values are read from the original text, whose spans res describes;
	// the steps rewrite text and re-scan it themselves.
	orig := text
	res, err := scan(orig)
	if err != nil {
		return "", nil, err
	}

	// URL lists keep their tcp:// and udp:// entries.
	for path, id := range map[string]string{"listeners": MiniListeners, "mapped_listeners": MiniMappedListeners} {
		a, ok := res.assigns[path]
		if !ok {
			continue
		}
		list, de := decodeLiteral[[]string](orig, a)
		if de != nil {
			return "", nil, de
		}
		kept := slices.DeleteFunc(slices.Clone(list), func(u string) bool { return !MiniScheme(u) })
		if len(kept) != len(list) {
			add(id)
			step(func() (string, error) { return SetStringArray(text, path, kept) })
		}
	}
	if blocks := res.arrayFields["peer"]; len(blocks) > 0 {
		var kept []Peer
		dropped := false
		for _, b := range blocks {
			var p Peer
			if a, ok := b["uri"]; ok {
				if p.URI, err = decodeLiteral[string](orig, a); err != nil {
					return "", nil, err
				}
			}
			if a, ok := b["peer_public_key"]; ok {
				if p.PublicKey, err = decodeLiteral[string](orig, a); err != nil {
					return "", nil, err
				}
			}
			if MiniScheme(p.URI) {
				kept = append(kept, p)
			} else {
				dropped = true
			}
		}
		if dropped {
			add(MiniPeers)
			step(func() (string, error) { return SetPeers(text, kept) })
		}
	}

	// Whole structures mini does not have.
	for _, name := range []string{"proxy_network", "port_forward"} {
		if len(res.arrayBlocks[name]) > 0 {
			add(map[string]string{"proxy_network": MiniSubnetProxy, "port_forward": MiniPortForward}[name])
			step(func() (string, error) { return removeTableArray(text, name) })
		}
	}
	if _, ok := res.tables["vpn_portal_config"]; ok || len(res.arrayBlocks[portalClients]) > 0 {
		add(MiniVpnPortal)
		step(func() (string, error) { return removeVpnPortal(text) })
	}
	for _, k := range []struct{ path, id string }{
		{"exit_nodes", MiniExitNodes}, {"socks5_proxy", MiniSocks5},
		{"ipv6_public_addr_auto", MiniIPv6Public}, {"ipv6_public_addr_provider", MiniIPv6Public},
		{"ipv6_public_addr_prefix", MiniIPv6Public},
	} {
		if _, ok := res.assigns[k.path]; ok {
			add(k.id)
			step(func() (string, error) { return RemoveValue(text, k.path) })
		}
	}

	// Flags: switches mini forces off, and algorithms it does not carry.
	keys := make([]string, 0, len(miniFlags))
	for k := range miniFlags {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		a, ok := res.assigns["flags."+k]
		if !ok {
			continue
		}
		if on, de := decodeLiteral[bool](orig, a); de == nil && on {
			add(miniFlags[k])
			step(func() (string, error) { return RemoveValue(text, "flags."+k) })
		}
	}
	if a, ok := res.assigns["flags.data_compress_algo"]; ok && !noCompression(orig[a.vs:a.ve]) {
		add(MiniCompression)
		step(func() (string, error) { return RemoveValue(text, "flags.data_compress_algo") })
	}
	if a, ok := res.assigns["flags.encryption_algorithm"]; ok {
		if s, de := decodeLiteral[string](orig, a); de != nil || !miniCipher(s) {
			add(MiniEncryption)
			step(func() (string, error) { return RemoveValue(text, "flags.encryption_algorithm") })
		}
	}
	if err != nil {
		return "", nil, err
	}
	order := []string{MiniListeners, MiniMappedListeners, MiniPeers, MiniSubnetProxy, MiniExitNodes, MiniSocks5,
		MiniPortForward, MiniVpnPortal, MiniIPv6Public, MiniMagicDNS, MiniKCP, MiniQUIC, MiniUDPBroadcast,
		MiniCompression, MiniEncryption}
	slices.SortFunc(ids, func(a, b string) int { return slices.Index(order, a) - slices.Index(order, b) })
	return text, ids, nil
}

// noCompression reports whether a data_compress_algo literal means none
// (the enum's 0/1, or its name).
func noCompression(literal string) bool {
	l := strings.ToLower(strings.Trim(strings.TrimSpace(literal), `"'`))
	return l == "" || l == "0" || l == "1" || l == "none" || l == "invalid"
}

// miniCipher reports whether EasyTier mini carries an encryption
// algorithm: it is built with AES-GCM only.
func miniCipher(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "" || s == "aes-gcm" || s == "aes-256-gcm"
}

// Feature IDs of settings tied to an EasyTier version.
const (
	// NewPortal is the VPN portal's named clients (2.7+).
	NewPortal = "vpn_portal_clients"
	// LegacyPortal is the VPN portal's client range (2.6; 2.7 rejects it).
	LegacyPortal   = "vpn_portal_client_cidr"
	TCPStunServers = "tcp_stun_servers"
	SocketMark     = "socket_mark"
	PreferRelay    = "prefer_peer_relay"
	ManagedCreds   = "managed_credentials"
)

// VersionConflicts lists the settings of a document the network's core
// cannot run, as feature IDs: on EasyTier 2.7 or later (v27) the 2.6
// portal format, which it refuses; before 2.7 the settings 2.7 added,
// which older cores refuse or ignore.
func VersionConflicts(text string, v27 bool) ([]string, error) {
	res, err := scan(text)
	if err != nil {
		return nil, err
	}
	_, legacy := res.assigns["vpn_portal_config.client_cidr"]
	if v27 {
		if legacy {
			return []string{LegacyPortal}, nil
		}
		return nil, nil
	}
	var out []string
	_, inPortal := res.tables["vpn_portal_config"]
	if (inPortal && !legacy) || len(res.arrayBlocks[portalClients]) > 0 {
		out = append(out, NewPortal)
	}
	for _, k := range []struct{ path, id string }{
		{"tcp_stun_servers", TCPStunServers}, {"flags.socket_mark", SocketMark}, {"flags.prefer_peer_relay", PreferRelay},
	} {
		if _, ok := res.assigns[k.path]; ok {
			out = append(out, k.id)
		}
	}
	if len(res.arrayBlocks["managed_credentials"]) > 0 {
		out = append(out, ManagedCreds)
	}
	return out, nil
}

// AtLeast27 reports whether an EasyTier release tag ("v2.7.0",
// "2.7.0-8b7f1f01") is 2.7 or later; an unknown tag counts as later, so a
// custom build is never warned about the newer format.
func AtLeast27(tag string) bool {
	t := strings.TrimPrefix(strings.TrimSpace(tag), "v")
	var major, minor int
	n, _ := fmt.Sscanf(t, "%d.%d", &major, &minor)
	if n < 2 {
		return true
	}
	return major > 2 || major == 2 && minor >= 7
}

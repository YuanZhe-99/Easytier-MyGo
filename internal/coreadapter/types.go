// Package coreadapter is the application's typed view of a running
// EasyTier core. It runs the core's companion easytier-cli binary and
// parses its output; it never writes configuration and never changes the
// core's state.
//
// The types mirror the JSON that easytier-cli v2.6.4 prints (verified
// against real output, see testdata). Fields that the CLI renders as
// display strings (peer list bytes, latency, loss rate) keep the string
// representation and quote formatting; the verbose peer command returns
// raw counters instead.
package coreadapter

import (
	"net/netip"
)

// NatType is the STUN NAT type reported in node and peer output. Its
// names come from the core's common.proto.
type NatType int32

const (
	NatUnknown          NatType = 0
	NatOpenInternet     NatType = 1
	NatNoPAT            NatType = 2
	NatFullCone         NatType = 3
	NatRestricted       NatType = 4
	NatPortRestricted   NatType = 5
	NatSymmetric        NatType = 6
	NatSymUDPFirewall   NatType = 7
	NatSymmetricEasyInc NatType = 8
	NatSymmetricEasyDec NatType = 9
)

func (n NatType) String() string {
	switch n {
	case NatUnknown:
		return "Unknown"
	case NatOpenInternet:
		return "OpenInternet"
	case NatNoPAT:
		return "NoPAT"
	case NatFullCone:
		return "FullCone"
	case NatRestricted:
		return "Restricted"
	case NatPortRestricted:
		return "PortRestricted"
	case NatSymmetric:
		return "Symmetric"
	case NatSymUDPFirewall:
		return "SymUdpFirewall"
	case NatSymmetricEasyInc:
		return "SymmetricEasyInc"
	case NatSymmetricEasyDec:
		return "SymmetricEasyDec"
	default:
		return "Unknown"
	}
}

// STUNInfo is the public endpoint information the core learned from STUN.
type STUNInfo struct {
	UDPNATType     NatType  `json:"udp_nat_type"`
	TCPNATType     NatType  `json:"tcp_nat_type"`
	LastUpdateTime Uint64   `json:"last_update_time"`
	PublicIP       []string `json:"public_ip"`
	MinPort        uint32   `json:"min_port"`
	MaxPort        uint32   `json:"max_port"`
}

// IPAddr is a protobuf-encoded IPv4 address: the four octets as a
// big-endian uint32.
type IPAddr struct {
	Addr uint32 `json:"addr"`
}

// IPv4 decodes the address.
func (a IPAddr) IPv4() netip.Addr {
	return netip.AddrFrom4([4]byte{byte(a.Addr >> 24), byte(a.Addr >> 16), byte(a.Addr >> 8), byte(a.Addr)})
}

// IPAddr6 is a protobuf-encoded IPv6 address in four parts.
type IPAddr6 struct {
	Part1 uint32 `json:"part1"`
	Part2 uint32 `json:"part2"`
	Part3 uint32 `json:"part3"`
	Part4 uint32 `json:"part4"`
}

// IPv6 decodes the address.
func (a IPAddr6) IPv6() netip.Addr {
	var b [16]byte
	put := func(off int, v uint32) {
		b[off] = byte(v >> 24)
		b[off+1] = byte(v >> 16)
		b[off+2] = byte(v >> 8)
		b[off+3] = byte(v)
	}
	put(0, a.Part1)
	put(4, a.Part2)
	put(8, a.Part3)
	put(12, a.Part4)
	return netip.AddrFrom16(b)
}

// IPCIDR is an address with its prefix length, as the verbose peer
// output encodes it.
type IPCIDR struct {
	Address       IPAddr `json:"address"`
	NetworkLength int    `json:"network_length"`
}

// IPv4 returns the bare address.
func (c IPCIDR) IPv4() netip.Addr { return c.Address.IPv4() }

// String renders address/prefix.
func (c IPCIDR) String() string {
	return c.Address.IPv4().String() + "/" + itoa(c.NetworkLength)
}

// IPList is the node's view of its own addresses.
type IPList struct {
	PublicIPv4     IPAddr   `json:"public_ipv4"`
	InterfaceIPv4s []IPAddr `json:"interface_ipv4s"`
	PublicIPv6     IPAddr6  `json:"public_ipv6"`
	InterfaceIPv6s []IPAddr6
	Listeners      []string `json:"listeners"`
}

// NodeInfo is the output of `easytier-cli -o json node`.
type NodeInfo struct {
	PeerID      uint32          `json:"peer_id"`
	IPv4Addr    string          `json:"ipv4_addr"` // "10.0.0.1/24" or "" while unassigned
	ProxyCIDRs  []string        `json:"proxy_cidrs"`
	Hostname    string          `json:"hostname"`
	STUNInfo    STUNInfo        `json:"stun_info"`
	InstID      string          `json:"inst_id"`
	Listeners   []string        `json:"listeners"`
	Config      string          `json:"config"` // raw TOML, may contain secrets: never forward to logs
	Version     string          `json:"version"`
	FeatureFlag map[string]bool `json:"feature_flag"`
	IPList      IPList          `json:"ip_list"`
}

// Peer is one entry of `easytier-cli -o json peer`. The counters are the
// CLI's display strings and are "-" when not applicable (the local peer).
type Peer struct {
	CIDR        string `json:"cidr"`
	IPv4        string `json:"ipv4"`
	Hostname    string `json:"hostname"`
	Cost        string `json:"cost"`
	LatencyMS   string `json:"lat_ms"`
	LossRate    string `json:"loss_rate"`
	RxBytes     string `json:"rx_bytes"`
	TxBytes     string `json:"tx_bytes"`
	TunnelProto string `json:"tunnel_proto"`
	NatType     string `json:"nat_type"`
	ID          string `json:"id"`
	Version     string `json:"version"`
}

// IsLocal reports whether the entry is this core's own instance.
func (p Peer) IsLocal() bool { return p.Cost == "Local" }

// PeerRoutePair is one entry of `easytier-cli -v -o json peer`: the
// routing view and the raw connection view of a peer.
type PeerRoutePair struct {
	Route PeerRoute    `json:"route"`
	Peer  PeerConnInfo `json:"peer"`
}

// PeerRoute is the routing view of a remote peer.
type PeerRoute struct {
	PeerID                  uint32   `json:"peer_id"`
	IPv4Addr                *IPCIDR  `json:"ipv4_addr"`
	NextHopPeerID           uint32   `json:"next_hop_peer_id"`
	Cost                    uint32   `json:"cost"`
	PathLatency             uint32   `json:"path_latency"`
	ProxyCIDRs              []string `json:"proxy_cidrs"`
	Hostname                string   `json:"hostname"`
	STUNInfo                STUNInfo `json:"stun_info"`
	InstID                  string   `json:"inst_id"`
	Version                 string   `json:"version"`
	NextHopPeerIDLatFirst   uint32   `json:"next_hop_peer_id_latency_first"`
	CostLatencyFirst        uint32   `json:"cost_latency_first"`
	PathLatencyLatencyFirst uint32   `json:"path_latency_latency_first"`
}

// PeerConnInfo is the connection view of a remote peer.
type PeerConnInfo struct {
	PeerID                 uint32     `json:"peer_id"`
	Conns                  []PeerConn `json:"conns"`
	DefaultConnID          ConnID     `json:"default_conn_id"`
	DirectlyConnectedConns []ConnID   `json:"directly_connected_conns"`
}

// ConnID is a UUID as some verbose output fields encode it: four parts,
// unlike the string conn_id of a connection.
type ConnID struct {
	Part1 uint32 `json:"part1"`
	Part2 uint32 `json:"part2"`
	Part3 uint32 `json:"part3"`
	Part4 uint32 `json:"part4"`
}

// IsZero reports whether the field was absent.
func (c ConnID) IsZero() bool { return c == ConnID{} }

// String renders the usual 8-4-4-4-12 UUID form.
func (c ConnID) String() string {
	return hex32(c.Part1) + "-" + hex32(c.Part2 >> 16)[4:] + "-" + hex32(c.Part2)[4:] +
		"-" + hex32(c.Part3 >> 16)[4:] + "-" + hex32(c.Part3)[4:] + hex32(c.Part4)
}

func hex32(v uint32) string {
	const digits = "0123456789abcdef"
	var b [8]byte
	for i := 7; i >= 0; i-- {
		b[i] = digits[v&0xf]
		v >>= 4
	}
	return string(b[:])
}

// PeerConn is one tunnel to a remote peer.
type PeerConn struct {
	ConnID        string     `json:"conn_id"`
	MyPeerID      uint32     `json:"my_peer_id"`
	PeerID        uint32     `json:"peer_id"`
	Tunnel        TunnelInfo `json:"tunnel"`
	Stats         ConnStats  `json:"stats"`
	LossRate      float64    `json:"loss_rate"`
	IsClient      bool       `json:"is_client"`
	NetworkName   string     `json:"network_name"`
	IsClosed      bool       `json:"is_closed"`
	SecureAuthLvl uint32     `json:"secure_auth_level"`
}

// ConnStats are the raw tunnel counters of a peer connection.
type ConnStats struct {
	RxBytes   Uint64 `json:"rx_bytes"`
	TxBytes   Uint64 `json:"tx_bytes"`
	RxPackets Uint64 `json:"rx_packets"`
	TxPackets Uint64 `json:"tx_packets"`
	LatencyUS Uint64 `json:"latency_us"`
}

// TunnelInfo describes a peer connection's tunnel endpoints.
type TunnelInfo struct {
	TunnelType         string  `json:"tunnel_type"`
	LocalAddr          *URLBox `json:"local_addr"`
	RemoteAddr         *URLBox `json:"remote_addr"`
	ResolvedRemoteAddr *URLBox `json:"resolved_remote_addr"`
}

// URLBox wraps the CLI's {"url": "..."} objects.
type URLBox struct {
	URL string `json:"url"`
}

// Route is one entry of `easytier-cli -o json route`.
type Route struct {
	IPv4                    string `json:"ipv4"`
	Hostname                string `json:"hostname"`
	ProxyCIDRs              string `json:"proxy_cidrs"`
	NextHopIPv4             string `json:"next_hop_ipv4"`
	NextHopHostname         string `json:"next_hop_hostname"`
	NextHopLat              any    `json:"next_hop_lat"`
	PathLen                 uint32 `json:"path_len"`
	PathLatency             any    `json:"path_latency"`
	NextHopIPv4LatFirst     string `json:"next_hop_ipv4_lat_first"`
	NextHopHostnameLatFirst any    `json:"next_hop_hostname_lat_first"`
	PathLenLatFirst         uint32 `json:"path_len_lat_first"`
	PathLatencyLatFirst     any    `json:"path_latency_lat_first"`
	Version                 string `json:"version"`
}

// ConnectorStatus is the state of a configured connector.
type ConnectorStatus int32

// The connector states come from the core's api_instance.proto.
const (
	ConnectorConnected    ConnectorStatus = 0
	ConnectorDisconnected ConnectorStatus = 1
	ConnectorConnecting   ConnectorStatus = 2
)

func (s ConnectorStatus) String() string {
	switch s {
	case ConnectorConnected:
		return "Connected"
	case ConnectorDisconnected:
		return "Disconnected"
	case ConnectorConnecting:
		return "Connecting"
	default:
		return "Unknown"
	}
}

// Connector is one entry of `easytier-cli -o json connector`.
type Connector struct {
	URL    URLBox          `json:"url"`
	Status ConnectorStatus `json:"status"`
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

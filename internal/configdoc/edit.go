package configdoc

// The form editor's engine: read the fields a form shows out of raw TOML,
// and write single fields back by replacing only their spans. Comments,
// unknown fields, formatting and everything the form does not touch stay
// byte-identical. Every write re-parses the result and refuses to return
// text that is no longer valid TOML, so a bug here can produce a clear
// error but never a broken saved document (the caller keeps the original).

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

// Values are the fields the basic form edits. A nil pointer means "the
// document has no such key" on extract, and "leave it alone" on apply.
// List and block pointers distinguish "no change" from "remove all".
type Values struct {
	InstanceName  *string
	InstanceID    *string
	Hostname      *string
	IPv4          *string
	DHCP          *bool
	Listeners     *[]string
	NetworkName   *string
	NetworkSecret *string
	Peers         *[]Peer

	// Flags are flags.* booleans from the covered set (see DefaultFlags).
	// On extract they hold what the document sets; on apply they are the
	// changed entries.
	Flags map[string]bool
	// Advanced scalars, all inside [flags] except IPv6PublicAddrAuto.
	DevName               *string
	MTU                   *uint32
	SocketMark            *uint32 // EasyTier 2.7+, Linux only
	InstanceRecvBpsLimit  *uint64
	RelayNetworkWhitelist *string
	IPv6PublicAddrAuto    *bool

	// Top-level singletons and lists.
	Socks5Proxy     *string
	Routes          *[]string
	ExitNodes       *[]string
	MappedListeners *[]string
	// Extra projection fields are pinned to upstream v2.6.4 top-level keys.
	StunServers          *[]string
	StunServersV6        *[]string
	TCPStunServers       *[]string // EasyTier 2.7+
	TCPWhitelist         *[]string
	UDPWhitelist         *[]string
	CredentialFile       *string
	NetNS                *string
	IPv6PublicAddrPrefix *string
	SecureModeEnabled    *bool
	SecurePrivateKey     *string
	SecurePublicKey      *string

	// Table arrays and the VPN portal.
	ProxyNetworks *[]ProxyNetwork
	PortForwards  *[]PortForward
	VpnPortal     *VpnPortalConfig

	// Remove lists key paths to delete (for example disabling the VPN
	// portal removes its table). Apply processes removals first.
	Remove []string
}

// Peer is one [[peer]] block.
type Peer struct {
	URI       string
	PublicKey string // peer_public_key, optional
}

// ProxyNetwork is one [[proxy_network]] block.
type ProxyNetwork struct {
	CIDR       string
	MappedCIDR string // mapped_cidr, optional
	Allow      []string
}

// PortForward is one [[port_forward]] block.
type PortForward struct {
	Proto    string // tcp or udp
	BindAddr string
	DstAddr  string
}

// VpnPortalConfig is the [vpn_portal_config] table (WireGuard portal) in
// either format: EasyTier 2.6 hands clients addresses from a range
// (ClientCIDR); EasyTier 2.7 names each client in
// [[vpn_portal_config.clients]] and rejects client_cidr.
type VpnPortalConfig struct {
	ClientCIDR      string // 2.6 format only
	WireguardListen string
	PrivateKey      string // wireguard_private_key, 2.7 format, optional
	Enabled         *bool  // enabled, 2.7 format, optional
	Clients         []VpnPortalClient
}

// Legacy reports whether the portal uses the EasyTier 2.6 format.
func (v VpnPortalConfig) Legacy() bool { return v.ClientCIDR != "" }

// VpnPortalClient is one [[vpn_portal_config.clients]] block.
type VpnPortalClient struct {
	Name      string
	VirtualIP string // virtual_ip: the client's address with its prefix
	Groups    []string
}

// DefaultFlags are the TOML-semantics defaults of the covered flags.*
// booleans (from the core's gen_default_flags). The GUI shows these for
// keys a document does not set, so a diff never materializes a default.
func DefaultFlags() map[string]bool {
	return map[string]bool{
		"latency_first":                     false,
		"use_smoltcp":                       false,
		"enable_ipv6":                       true,
		"enable_kcp_proxy":                  false,
		"disable_kcp_input":                 false,
		"enable_quic_proxy":                 false,
		"disable_quic_input":                false,
		"disable_p2p":                       false,
		"p2p_only":                          false,
		"lazy_p2p":                          false,
		"bind_device":                       true,
		"no_tun":                            false,
		"enable_exit_node":                  false,
		"relay_all_peer_rpc":                false,
		"need_p2p":                          false,
		"multi_thread":                      true,
		"proxy_forward_by_system":           false,
		"enable_encryption":                 true,
		"disable_tcp_hole_punching":         false,
		"disable_udp_hole_punching":         false,
		"enable_udp_broadcast_relay":        false,
		"disable_upnp":                      false,
		"disable_sym_hole_punching":         false,
		"accept_dns":                        false,
		"private_mode":                      false,
		"disable_relay_kcp":                 false,
		"enable_relay_foreign_network_kcp":  false,
		"disable_relay_quic":                false,
		"enable_relay_foreign_network_quic": false,
		"disable_relay_data":                false,
		"prefer_peer_relay":                 false, // EasyTier 2.7+
	}
}

// flagKeyKnown reports whether a flags.* key is part of the covered set.
func flagKeyKnown(key string) bool {
	_, ok := DefaultFlags()[key]
	return ok
}

// decodeLiteral parses one value span as a TOML literal of type T.
func decodeLiteral[T any](text string, a assignment) (T, error) {
	var v T
	wrap := struct {
		X *T `toml:"x"`
	}{X: &v}
	if err := toml.Unmarshal([]byte("x = "+text[a.vs:a.ve]), &wrap); err != nil {
		var zero T
		return zero, fmt.Errorf("%w: %s has an unexpected type", ErrUnsupported, a.path)
	}
	return v, nil
}

// ErrUnsupported marks documents the form engine refuses to rewrite
// (inline tables, non-contiguous peer blocks, duplicate keys). The caller
// falls back to the raw editor.
var ErrUnsupported = errors.New("configdoc: structure is not form-editable")

// assignment is one key = value site in the text.
type assignment struct {
	path   string
	vs, ve int // value span
}

// tableSpan is a [table] or [[table]] header and the area its keys live
// in. end is where a new key can be inserted (before the next header).
type tableSpan struct {
	start, end int
	array      bool
}

type scanResult struct {
	assigns map[string]assignment // last wins; duplicates recorded too
	dups    map[string]bool
	order   []string
	tables  map[string]tableSpan
	// arrayBlocks collects every [[name]] block span in order (tables
	// holds only the latest one per name).
	arrayBlocks map[string][]tableSpan
	// arrayFields holds, per array table, each block's assignments by key,
	// so block-oriented structures (peers, proxy networks, port forwards)
	// can be read back.
	arrayFields map[string][]map[string]assignment
	// topLevelEnd is the offset after the last top-level assignment's
	// line; top-level insertions go there, keeping table blocks grouped.
	topLevelEnd int
	// tableKeyEnd is the offset after the last assignment line inside a
	// table, so insertions stay with the table's keys.
	tableKeyEnd map[string]int
	// peers collects the uri values of [[peer]] blocks in order.
	peers    []string
	hasPeers bool
}

// scan walks the document and records assignment spans and table bodies.
func scan(text string) (*scanResult, error) {
	res := &scanResult{
		assigns:     map[string]assignment{},
		dups:        map[string]bool{},
		tables:      map[string]tableSpan{},
		arrayBlocks: map[string][]tableSpan{},
		arrayFields: map[string][]map[string]assignment{},
		tableKeyEnd: map[string]int{},
	}
	table := ""
	var openTable string
	blockName := ""
	blockIndex := -1
	closeTable := func(end int) {
		if openTable == "" {
			return
		}
		span := res.tables[openTable]
		span.end = end
		res.tables[openTable] = span
		if blocks := res.arrayBlocks[openTable]; len(blocks) > 0 {
			blocks[len(blocks)-1].end = end
			res.arrayBlocks[openTable] = blocks
		}
		openTable = ""
	}

	i := 0
	for i <= len(text) {
		lineStart := i
		lineEnd := strings.IndexByte(text[lineStart:], '\n')
		if lineEnd < 0 {
			lineEnd = len(text)
		} else {
			lineEnd = lineStart + lineEnd
		}
		line := strings.TrimSuffix(text[lineStart:lineEnd], "\r")
		trimmed := strings.TrimSpace(line)
		next := -1

		switch {
		case trimmed == "" || strings.HasPrefix(trimmed, "#"):
			// blank or comment: nothing to record

		case trimmed[0] == '[':
			name, isArray, ok := parseHeader(trimmed)
			if !ok {
				return nil, fmt.Errorf("%w: cannot parse table header %q", ErrUnsupported, trimmed)
			}
			closeTable(lineStart)
			table = name
			if isArray {
				// Array-of-tables entries share the name; keep one span
				// per block, but a later entry overwrites the table span
				// for insertion purposes.
				if name == "peer" {
					res.hasPeers = true
				}
				res.arrayFields[name] = append(res.arrayFields[name], map[string]assignment{})
				blockName = name
				blockIndex = len(res.arrayFields[name]) - 1
			} else {
				blockName = ""
				blockIndex = -1
			}
			res.tables[name] = tableSpan{start: lineStart, end: len(text), array: isArray}
			if isArray {
				res.arrayBlocks[name] = append(res.arrayBlocks[name], tableSpan{start: lineStart, end: len(text), array: true})
			}
			openTable = name

		default:
			key, valueStart, ok := parseAssignment(line, lineStart)
			if !ok {
				// A line that is neither header nor assignment is not
				// valid TOML; the caller has parsed first, so treat it
				// as uneditable rather than guessing.
				return nil, fmt.Errorf("%w: unexpected line %q", ErrUnsupported, trimmed)
			}
			ve, err := scanValue(text, valueStart)
			if err != nil {
				return nil, err
			}
			path := joinPath(table, key)
			if _, dup := res.assigns[path]; dup {
				res.dups[path] = true
			}
			res.assigns[path] = assignment{path: path, vs: valueStart, ve: ve}
			res.order = append(res.order, path)
			if blockName != "" && blockIndex >= 0 {
				if res.arrayFields[blockName] == nil {
					res.arrayFields[blockName] = make([]map[string]assignment, blockIndex+1)
				}
				res.arrayFields[blockName][blockIndex][key] = assignment{path: path, vs: valueStart, ve: ve}
			}
			if table == "peer" && key == "uri" && len(res.peers) < 1024 {
				res.peers = append(res.peers, text[valueStart:ve])
			}
			if table == "" {
				if nl := strings.IndexByte(text[ve:], '\n'); nl >= 0 {
					res.topLevelEnd = ve + nl + 1
				} else {
					res.topLevelEnd = len(text)
				}
			} else {
				if nl := strings.IndexByte(text[ve:], '\n'); nl >= 0 {
					res.tableKeyEnd[table] = ve + nl + 1
				} else {
					res.tableKeyEnd[table] = len(text)
				}
			}
			if ve > lineEnd {
				// A multiline value (array, inline table, multiline
				// string): its continuation lines must not be scanned as
				// lines of their own. Continue after its closing token.
				if nl := strings.IndexByte(text[ve:], '\n'); nl >= 0 {
					next = ve + nl + 1
				} else {
					next = len(text) + 1
				}
			}
		}

		if lineEnd >= len(text) {
			break
		}
		if next >= 0 {
			i = next
		} else {
			i = lineEnd + 1
		}
	}
	closeTable(len(text))
	return res, nil
}

// parseHeader reads `[name]` / `[[name]]`, tolerating trailing comments.
func parseHeader(trimmed string) (string, bool, bool) {
	isArray := strings.HasPrefix(trimmed, "[[")
	open := "["
	closeTok := "]"
	if isArray {
		open, closeTok = "[[", "]]"
	}
	if !strings.HasPrefix(trimmed, open) {
		return "", false, false
	}
	rest := trimmed[len(open):]
	end := strings.Index(rest, closeTok)
	if end < 0 {
		return "", false, false
	}
	name := strings.TrimSpace(rest[:end])
	after := strings.TrimSpace(rest[end+len(closeTok):])
	if after != "" && !strings.HasPrefix(after, "#") {
		return "", false, false
	}
	if name == "" || strings.ContainsAny(name, "\"'[]") {
		return "", false, false
	}
	return name, isArray, true
}

// parseAssignment reads `key = ` on one line (after leading whitespace)
// and returns the key and the byte offset where its value begins.
func parseAssignment(line string, lineStart int) (string, int, bool) {
	i := 0
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	keyStart := i
	for i < len(line) {
		c := line[i]
		if c == ' ' || c == '\t' || c == '=' {
			break
		}
		if !(c == '.' || c == '_' || c == '-' ||
			(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			return "", 0, false
		}
		i++
	}
	key := line[keyStart:i]
	if key == "" {
		return "", 0, false
	}
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	if i >= len(line) || line[i] != '=' {
		return "", 0, false
	}
	i++
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	return key, lineStart + i, true
}

// scanValue finds the end of a TOML value starting at pos.
func scanValue(text string, pos int) (int, error) {
	if pos >= len(text) {
		return 0, fmt.Errorf("%w: missing value", ErrUnsupported)
	}
	switch text[pos] {
	case '"', '\'':
		return scanString(text, pos)
	case '[', '{':
		return scanBrackets(text, pos)
	default:
		end := len(text)
		if nl := strings.IndexByte(text[pos:], '\n'); nl >= 0 {
			end = pos + nl
		}
		if hash := strings.IndexByte(text[pos:end], '#'); hash >= 0 {
			end = pos + hash
		}
		for end > pos && (text[end-1] == ' ' || text[end-1] == '\t' || text[end-1] == '\r') {
			end--
		}
		return end, nil
	}
}

// scanString finds the end of a basic or literal (or multiline) string
// whose opening quote is at i.
func scanString(text string, i int) (int, error) {
	q := text[i]
	triple := strings.Repeat(string(q), 3)
	if strings.HasPrefix(text[i:], triple) {
		rest := text[i+3:]
		idx := strings.Index(rest, triple)
		if idx < 0 {
			return 0, fmt.Errorf("%w: unclosed multiline string", ErrUnsupported)
		}
		return i + 3 + idx + 3, nil
	}
	j := i + 1
	for j < len(text) {
		c := text[j]
		if q == '"' && c == '\\' {
			j += 2
			continue
		}
		if c == '\n' {
			break
		}
		if c == q {
			return j + 1, nil
		}
		j++
	}
	return 0, fmt.Errorf("%w: unclosed string", ErrUnsupported)
}

// scanBrackets matches [] and {} while skipping strings and comments.
func scanBrackets(text string, i int) (int, error) {
	depth := 0
	for j := i; j < len(text); {
		switch c := text[j]; c {
		case '"', '\'':
			end, err := scanString(text, j)
			if err != nil {
				return 0, err
			}
			j = end
		case '#':
			nl := strings.IndexByte(text[j:], '\n')
			if nl < 0 {
				j = len(text)
			} else {
				j += nl
			}
		case '[', '{':
			depth++
			j++
		case ']', '}':
			depth--
			if depth == 0 {
				return j + 1, nil
			}
			j++
		default:
			j++
		}
	}
	return 0, fmt.Errorf("%w: unclosed array or inline table", ErrUnsupported)
}

func joinPath(table, key string) string {
	if table == "" {
		return key
	}
	return table + "." + key
}

// splitPath separates the last key segment from its parent table path.
func splitPath(path string) (parent, key string) {
	if idx := strings.LastIndexByte(path, '.'); idx >= 0 {
		return path[:idx], path[idx+1:]
	}
	return "", path
}

// SetValue replaces the raw value text at path with literal, inserting the
// key when the document does not have it. Only that span (or the inserted
// lines) changes; the result is re-parsed and a failure leaves text
// untouched.
func SetValue(text, path, literal string) (string, error) {
	res, err := scan(text)
	if err != nil {
		return "", err
	}
	if res.dups[path] {
		return "", fmt.Errorf("%w: %s is defined more than once", ErrUnsupported, path)
	}
	if a, ok := res.assigns[path]; ok {
		out := text[:a.vs] + literal + text[a.ve:]
		if err := verify(out); err != nil {
			return "", err
		}
		return out, nil
	}

	parent, key := splitPath(path)
	if _, ok := res.assigns[parent]; ok && parent != "" {
		return "", fmt.Errorf("%w: %s is not a table", ErrUnsupported, parent)
	}
	if res.dups[parent] {
		return "", fmt.Errorf("%w: %s is defined more than once", ErrUnsupported, parent)
	}

	var out string
	if span, ok := res.tables[parent]; ok && parent != "" {
		// Insert as the table's last key, right after its last
		// assignment line (or right after an empty table's header).
		insert := res.tableKeyEnd[parent]
		if insert == 0 {
			insert = span.start
			if nl := strings.IndexByte(text[insert:], '\n'); nl >= 0 {
				insert += nl + 1
			} else {
				insert = len(text)
			}
		}
		line := key + " = " + literal + "\n"
		if insert > 0 && text[insert-1] != '\n' {
			line = "\n" + line
		}
		out = text[:insert] + line + text[insert:]
	} else if parent == "" {
		// Top-level: insert right after the last top-level key (keeping
		// the table blocks grouped), or before the first header when
		// there are none.
		insert := res.topLevelEnd
		if insert == 0 {
			insert = len(text)
			for _, span := range res.tables {
				if span.start < insert {
					insert = span.start
				}
			}
		}
		line := key + " = " + literal + "\n"
		if insert > 0 && text[insert-1] != '\n' {
			line = "\n" + line
		}
		out = text[:insert] + line + text[insert:]
	} else {
		// Create the table block at the end.
		block := "[" + parent + "]\n" + key + " = " + literal + "\n"
		if len(text) > 0 && !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		if len(text) > 0 && !strings.HasSuffix(text, "\n\n") {
			block = "\n" + block
		}
		out = text + block
	}
	if err := verify(out); err != nil {
		return "", err
	}
	return out, nil
}

// SetString, SetBool and SetStringArray wrap SetValue with TOML literals.
func SetString(text, path, v string) (string, error) {
	return SetValue(text, path, tomlQuote(v))
}

func SetBool(text, path string, v bool) (string, error) {
	if v {
		return SetValue(text, path, "true")
	}
	return SetValue(text, path, "false")
}

func SetStringArray(text, path string, vs []string) (string, error) {
	parts := make([]string, len(vs))
	for i, v := range vs {
		parts[i] = tomlQuote(v)
	}
	return SetValue(text, path, "[ "+strings.Join(parts, ", ")+" ]")
}

// SetPeers rewrites all [[peer]] blocks (which must be contiguous) with
// the given URIs, or appends the blocks when there are none.
func SetPeers(text string, peers []Peer) (string, error) {
	var b strings.Builder
	for _, p := range peers {
		if strings.TrimSpace(p.URI) == "" {
			continue
		}
		b.WriteString("[[peer]]\nuri = ")
		b.WriteString(tomlQuote(strings.TrimSpace(p.URI)))
		b.WriteString("\n")
		if strings.TrimSpace(p.PublicKey) != "" {
			b.WriteString("peer_public_key = ")
			b.WriteString(tomlQuote(strings.TrimSpace(p.PublicKey)))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	return setTableArray(text, "peer", b.String())
}

// SetProxyNetworks rewrites all [[proxy_network]] blocks.
func SetProxyNetworks(text string, nets []ProxyNetwork) (string, error) {
	var b strings.Builder
	for _, n := range nets {
		if strings.TrimSpace(n.CIDR) == "" {
			continue
		}
		b.WriteString("[[proxy_network]]\ncidr = ")
		b.WriteString(tomlQuote(strings.TrimSpace(n.CIDR)))
		b.WriteString("\n")
		if strings.TrimSpace(n.MappedCIDR) != "" {
			b.WriteString("mapped_cidr = ")
			b.WriteString(tomlQuote(strings.TrimSpace(n.MappedCIDR)))
			b.WriteString("\n")
		}
		if len(n.Allow) > 0 {
			parts := make([]string, 0, len(n.Allow))
			for _, a := range n.Allow {
				if strings.TrimSpace(a) != "" {
					parts = append(parts, tomlQuote(strings.TrimSpace(a)))
				}
			}
			if len(parts) > 0 {
				b.WriteString("allow = [ ")
				b.WriteString(strings.Join(parts, ", "))
				b.WriteString(" ]\n")
			}
		}
		b.WriteString("\n")
	}
	return setTableArray(text, "proxy_network", b.String())
}

// SetPortForwards rewrites all [[port_forward]] blocks.
func SetPortForwards(text string, forwards []PortForward) (string, error) {
	var b strings.Builder
	for _, f := range forwards {
		if strings.TrimSpace(f.BindAddr) == "" && strings.TrimSpace(f.DstAddr) == "" {
			continue
		}
		proto := strings.TrimSpace(f.Proto)
		if proto != "udp" {
			proto = "tcp"
		}
		b.WriteString("[[port_forward]]\nbind_addr = ")
		b.WriteString(tomlQuote(strings.TrimSpace(f.BindAddr)))
		b.WriteString("\ndst_addr = ")
		b.WriteString(tomlQuote(strings.TrimSpace(f.DstAddr)))
		b.WriteString("\nproto = ")
		b.WriteString(tomlQuote(proto))
		b.WriteString("\n\n")
	}
	return setTableArray(text, "port_forward", b.String())
}

// setTableArray replaces all contiguous [[name]] blocks with blocksText
// (or appends when there are none). Non-contiguous blocks, gaps or other
// headers inside the region refuse the rewrite.
func setTableArray(text, name, blocksText string) (string, error) {
	res, err := scan(text)
	if err != nil {
		return "", err
	}
	blocks := res.arrayBlocks[name]
	if len(blocks) == 0 {
		if strings.TrimSpace(blocksText) == "" {
			return text, nil
		}
		out := text
		if len(out) > 0 && !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
		if len(out) > 0 && !strings.HasSuffix(out, "\n\n") {
			out += "\n"
		}
		out += blocksText
		if err := verify(out); err != nil {
			return "", err
		}
		return out, nil
	}
	regionStart, regionEnd := -1, -1
	for i, span := range blocks {
		if regionStart == -1 || span.start < regionStart {
			regionStart = span.start
		}
		if span.end > regionEnd {
			regionEnd = span.end
		}
		if i > 0 && span.start != blocks[i-1].end {
			return "", fmt.Errorf("%w: [[%s]] blocks are not contiguous", ErrUnsupported, name)
		}
	}
	out := text[:regionStart] + blocksText + text[regionEnd:]
	out = strings.TrimRight(out, "\n") + "\n"
	if err := verify(out); err != nil {
		return "", err
	}
	return out, nil
}

// SetInt writes an integer literal.
func SetInt(text, path string, v int64) (string, error) {
	return SetValue(text, path, fmt.Sprintf("%d", v))
}

// RemoveKey deletes a key (its whole line) or, failing that, a table
// (its header and body). Missing paths are a no-op.
func RemoveKey(text, path string) (string, error) {
	res, err := scan(text)
	if err != nil {
		return "", err
	}
	if _, ok := res.assigns[path]; ok {
		return RemoveValue(text, path)
	}
	if _, ok := res.tables[path]; ok {
		return RemoveTable(text, path)
	}
	return text, nil
}

// RemoveValue deletes one assignment line.
func RemoveValue(text, path string) (string, error) {
	res, err := scan(text)
	if err != nil {
		return "", err
	}
	a, ok := res.assigns[path]
	if !ok {
		return text, nil
	}
	if res.dups[path] {
		return "", fmt.Errorf("%w: %s is defined more than once", ErrUnsupported, path)
	}
	start := a.vs
	for start > 0 && text[start-1] != '\n' {
		start--
	}
	end := a.ve
	for end < len(text) && text[end] != '\n' {
		end++
	}
	if end < len(text) {
		end++ // include the newline
	}
	out := text[:start] + text[end:]
	if err := verify(out); err != nil {
		return "", err
	}
	return out, nil
}

// RemoveTable deletes a [table] header and everything up to the next
// header (or EOF). Comments directly above the header stay.
func RemoveTable(text, path string) (string, error) {
	res, err := scan(text)
	if err != nil {
		return "", err
	}
	span, ok := res.tables[path]
	if !ok {
		return text, nil
	}
	out := text[:span.start] + text[span.end:]
	if err := verify(out); err != nil {
		return "", err
	}
	return out, nil
}

// Extract reads the form fields. Missing keys yield nil pointers.
func Extract(text string) (Values, error) {
	res, err := scan(text)
	if err != nil {
		return Values{}, err
	}
	// Inline tables (`network_identity = { … }`) are valid TOML the form
	// must not silently duplicate; refuse up front so the user is told to
	// use the raw editor instead of failing at save time.
	for _, parent := range []string{"network_identity", "flags"} {
		if _, ok := res.assigns[parent]; ok {
			return Values{}, fmt.Errorf("%w: %s is an inline table", ErrUnsupported, parent)
		}
	}
	var v Values
	str := func(path string) (*string, error) {
		a, ok := res.assigns[path]
		if !ok {
			return nil, nil
		}
		var s string
		if err := toml.Unmarshal([]byte("x = "+text[a.vs:a.ve]), &struct {
			X *string `toml:"x"`
		}{X: &s}); err != nil {
			return nil, fmt.Errorf("%w: %s is not a string", ErrUnsupported, path)
		}
		return &s, nil
	}
	boolean := func(path string) (*bool, error) {
		a, ok := res.assigns[path]
		if !ok {
			return nil, nil
		}
		var b bool
		if err := toml.Unmarshal([]byte("x = "+text[a.vs:a.ve]), &struct {
			X *bool `toml:"x"`
		}{X: &b}); err != nil {
			return nil, fmt.Errorf("%w: %s is not a boolean", ErrUnsupported, path)
		}
		return &b, nil
	}

	if v.InstanceName, err = str("instance_name"); err != nil {
		return Values{}, err
	}
	if v.InstanceID, err = str("instance_id"); err != nil {
		return Values{}, err
	}
	if v.Hostname, err = str("hostname"); err != nil {
		return Values{}, err
	}
	if v.IPv4, err = str("ipv4"); err != nil {
		return Values{}, err
	}
	if v.DHCP, err = boolean("dhcp"); err != nil {
		return Values{}, err
	}
	if v.NetworkName, err = str("network_identity.network_name"); err != nil {
		return Values{}, err
	}
	if v.NetworkSecret, err = str("network_identity.network_secret"); err != nil {
		return Values{}, err
	}
	if a, ok := res.assigns["listeners"]; ok {
		var list []string
		if err := toml.Unmarshal([]byte("x = "+text[a.vs:a.ve]), &struct {
			X *[]string `toml:"x"`
		}{X: &list}); err != nil {
			return Values{}, fmt.Errorf("%w: listeners is not a string array", ErrUnsupported)
		}
		v.Listeners = &list
	}
	if blocks := res.arrayFields["peer"]; len(blocks) > 0 {
		peers := make([]Peer, 0, len(blocks))
		for _, b := range blocks {
			var p Peer
			for key, a := range b {
				switch key {
				case "uri":
					s, err := decodeLiteral[string](text, a)
					if err != nil {
						return Values{}, err
					}
					p.URI = s
				case "peer_public_key":
					s, err := decodeLiteral[string](text, a)
					if err != nil {
						return Values{}, err
					}
					p.PublicKey = s
				default:
					return Values{}, fmt.Errorf("%w: [[peer]] has unsupported key %q", ErrUnsupported, key)
				}
			}
			peers = append(peers, p)
		}
		v.Peers = &peers
	}
	flags := map[string]bool{}
	for path, a := range res.assigns {
		if !strings.HasPrefix(path, "flags.") {
			continue
		}
		key := strings.TrimPrefix(path, "flags.")
		if !flagKeyKnown(key) {
			continue
		}
		b, err := decodeLiteral[bool](text, a)
		if err != nil {
			return Values{}, err
		}
		flags[key] = b
	}
	if len(flags) > 0 {
		v.Flags = flags
	}
	if v.DevName, err = str("flags.dev_name"); err != nil {
		return Values{}, err
	}
	if a, ok := res.assigns["flags.mtu"]; ok {
		n, de := decodeLiteral[uint32](text, a)
		if de != nil {
			return Values{}, de
		}
		v.MTU = &n
	}
	if a, ok := res.assigns["flags.socket_mark"]; ok {
		n, de := decodeLiteral[uint32](text, a)
		if de != nil {
			return Values{}, de
		}
		v.SocketMark = &n
	}
	if a, ok := res.assigns["flags.instance_recv_bps_limit"]; ok {
		n, de := decodeLiteral[uint64](text, a)
		if de != nil {
			return Values{}, de
		}
		v.InstanceRecvBpsLimit = &n
	}
	if v.RelayNetworkWhitelist, err = str("flags.relay_network_whitelist"); err != nil {
		return Values{}, err
	}
	if v.IPv6PublicAddrAuto, err = boolean("ipv6_public_addr_auto"); err != nil {
		return Values{}, err
	}
	if v.SecureModeEnabled, err = boolean("secure_mode.enabled"); err != nil {
		return Values{}, err
	}
	if v.SecurePrivateKey, err = str("secure_mode.local_private_key"); err != nil {
		return Values{}, err
	}
	if v.SecurePublicKey, err = str("secure_mode.local_public_key"); err != nil {
		return Values{}, err
	}
	if v.Socks5Proxy, err = str("socks5_proxy"); err != nil {
		return Values{}, err
	}
	stringList := func(path string) (*[]string, error) {
		a, ok := res.assigns[path]
		if !ok {
			return nil, nil
		}
		list, de := decodeLiteral[[]string](text, a)
		if de != nil {
			return nil, fmt.Errorf("%w: %s is not a string array", ErrUnsupported, path)
		}
		return &list, nil
	}
	if v.Routes, err = stringList("routes"); err != nil {
		return Values{}, err
	}
	if v.ExitNodes, err = stringList("exit_nodes"); err != nil {
		return Values{}, err
	}
	if v.MappedListeners, err = stringList("mapped_listeners"); err != nil {
		return Values{}, err
	}
	for path, target := range map[string]**[]string{"stun_servers": &v.StunServers, "stun_servers_v6": &v.StunServersV6, "tcp_stun_servers": &v.TCPStunServers, "tcp_whitelist": &v.TCPWhitelist, "udp_whitelist": &v.UDPWhitelist} {
		if *target, err = stringList(path); err != nil {
			return Values{}, err
		}
	}
	for path, target := range map[string]**string{"credential_file": &v.CredentialFile, "netns": &v.NetNS, "ipv6_public_addr_prefix": &v.IPv6PublicAddrPrefix} {
		if *target, err = str(path); err != nil {
			return Values{}, err
		}
	}

	if v.ProxyNetworks, err = extractProxyNetworks(res, text); err != nil {
		return Values{}, err
	}
	if v.PortForwards, err = extractPortForwards(res, text); err != nil {
		return Values{}, err
	}
	if v.VpnPortal, err = extractVpnPortal(res, text); err != nil {
		return Values{}, err
	}
	return v, nil
}

// extractProxyNetworks reads [[proxy_network]] blocks; unknown keys in a
// block refuse the document, because rewriting would drop them.
func extractProxyNetworks(res *scanResult, text string) (*[]ProxyNetwork, error) {
	blocks := res.arrayFields["proxy_network"]
	if len(blocks) == 0 {
		return nil, nil
	}
	out := make([]ProxyNetwork, 0, len(blocks))
	for _, b := range blocks {
		var n ProxyNetwork
		for key, a := range b {
			switch key {
			case "cidr":
				s, err := decodeLiteral[string](text, a)
				if err != nil {
					return nil, err
				}
				n.CIDR = s
			case "mapped_cidr":
				s, err := decodeLiteral[string](text, a)
				if err != nil {
					return nil, err
				}
				n.MappedCIDR = s
			case "allow":
				list, err := decodeLiteral[[]string](text, a)
				if err != nil {
					return nil, err
				}
				n.Allow = list
			default:
				return nil, fmt.Errorf("%w: [[proxy_network]] has unsupported key %q", ErrUnsupported, key)
			}
		}
		if strings.TrimSpace(n.CIDR) == "" {
			return nil, fmt.Errorf("%w: a [[proxy_network]] block has no cidr", ErrUnsupported)
		}
		out = append(out, n)
	}
	return &out, nil
}

// extractPortForwards reads [[port_forward]] blocks.
func extractPortForwards(res *scanResult, text string) (*[]PortForward, error) {
	blocks := res.arrayFields["port_forward"]
	if len(blocks) == 0 {
		return nil, nil
	}
	out := make([]PortForward, 0, len(blocks))
	for _, b := range blocks {
		p := PortForward{Proto: "tcp"}
		for key, a := range b {
			switch key {
			case "proto":
				s, err := decodeLiteral[string](text, a)
				if err != nil {
					return nil, err
				}
				p.Proto = s
			case "bind_addr":
				s, err := decodeLiteral[string](text, a)
				if err != nil {
					return nil, err
				}
				p.BindAddr = s
			case "dst_addr":
				s, err := decodeLiteral[string](text, a)
				if err != nil {
					return nil, err
				}
				p.DstAddr = s
			default:
				return nil, fmt.Errorf("%w: [[port_forward]] has unsupported key %q", ErrUnsupported, key)
			}
		}
		out = append(out, p)
	}
	return &out, nil
}

// extractVpnPortal reads the [vpn_portal_config] table and its
// [[vpn_portal_config.clients]] blocks, in either format.
func extractVpnPortal(res *scanResult, text string) (*VpnPortalConfig, error) {
	_, inTable := res.tables["vpn_portal_config"]
	clients := res.arrayFields[portalClients]
	if !inTable && len(clients) == 0 {
		return nil, nil
	}
	var v VpnPortalConfig
	for path, dst := range map[string]*string{
		"vpn_portal_config.client_cidr":           &v.ClientCIDR,
		"vpn_portal_config.wireguard_listen":      &v.WireguardListen,
		"vpn_portal_config.wireguard_private_key": &v.PrivateKey,
	} {
		if a, ok := res.assigns[path]; ok {
			s, err := decodeLiteral[string](text, a)
			if err != nil {
				return nil, err
			}
			*dst = s
		}
	}
	if a, ok := res.assigns["vpn_portal_config.enabled"]; ok {
		b, err := decodeLiteral[bool](text, a)
		if err != nil {
			return nil, err
		}
		v.Enabled = &b
	}
	for _, block := range clients {
		var c VpnPortalClient
		for key, a := range block {
			var err error
			switch key {
			case "name":
				c.Name, err = decodeLiteral[string](text, a)
			case "virtual_ip":
				c.VirtualIP, err = decodeLiteral[string](text, a)
			case "groups":
				c.Groups, err = decodeLiteral[[]string](text, a)
			default:
				return nil, fmt.Errorf("%w: [[%s]] has unsupported key %q", ErrUnsupported, portalClients, key)
			}
			if err != nil {
				return nil, err
			}
		}
		v.Clients = append(v.Clients, c)
	}
	return &v, nil
}

// portalClients names the EasyTier 2.7 portal's client blocks.
const portalClients = "vpn_portal_config.clients"

// SetVpnPortal writes the portal: the 2.6 keys for a legacy one, else
// the 2.7 keys and its client blocks. Callers remove the old portal first.
func SetVpnPortal(text string, v VpnPortalConfig) (string, error) {
	var err error
	step := func(f func() (string, error)) {
		if err == nil {
			text, err = f()
		}
	}
	if v.Legacy() {
		step(func() (string, error) { return SetString(text, "vpn_portal_config.client_cidr", v.ClientCIDR) })
	}
	step(func() (string, error) {
		return SetString(text, "vpn_portal_config.wireguard_listen", v.WireguardListen)
	})
	if v.Legacy() {
		return text, err
	}
	if v.PrivateKey != "" {
		step(func() (string, error) {
			return SetString(text, "vpn_portal_config.wireguard_private_key", v.PrivateKey)
		})
	}
	if v.Enabled != nil {
		step(func() (string, error) { return SetBool(text, "vpn_portal_config.enabled", *v.Enabled) })
	}
	var b strings.Builder
	for _, c := range v.Clients {
		b.WriteString("[[" + portalClients + "]]\nname = " + tomlQuote(c.Name) + "\nvirtual_ip = " + tomlQuote(c.VirtualIP) + "\n")
		if len(c.Groups) > 0 {
			parts := make([]string, len(c.Groups))
			for i, g := range c.Groups {
				parts[i] = tomlQuote(g)
			}
			b.WriteString("groups = [ " + strings.Join(parts, ", ") + " ]\n")
		}
		b.WriteString("\n")
	}
	step(func() (string, error) { return setTableArray(text, portalClients, b.String()) })
	return text, err
}

// removeVpnPortal deletes the portal table and every client block.
func removeVpnPortal(text string) (string, error) {
	text, err := RemoveTable(text, "vpn_portal_config")
	if err != nil {
		return "", err
	}
	return removeTableArray(text, portalClients)
}

// removeTableArray deletes every [[name]] block.
func removeTableArray(text, name string) (string, error) {
	for range 1024 {
		res, err := scan(text)
		if err != nil {
			return "", err
		}
		if len(res.arrayBlocks[name]) == 0 {
			return text, nil
		}
		if text, err = RemoveTable(text, name); err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("%w: too many [[%s]] blocks", ErrUnsupported, name)
}

// Changes compares the loaded base with the current form values and
// returns only the differences, ready for ApplyValues.
func Changes(base, current Values) Values {
	var c Values
	if changedString(base.InstanceName, current.InstanceName) {
		c.InstanceName = current.InstanceName
	}
	if changedString(base.InstanceID, current.InstanceID) {
		c.InstanceID = current.InstanceID
	}
	if changedString(base.Hostname, current.Hostname) {
		c.Hostname = current.Hostname
	}
	if changedString(base.IPv4, current.IPv4) {
		c.IPv4 = current.IPv4
	}
	if changedBool(base.DHCP, current.DHCP) {
		c.DHCP = current.DHCP
	}
	if !slices.Equal(derefStrings(base.Listeners), derefStrings(current.Listeners)) {
		c.Listeners = current.Listeners
	}
	if changedString(base.NetworkName, current.NetworkName) {
		c.NetworkName = current.NetworkName
	}
	if changedString(base.NetworkSecret, current.NetworkSecret) {
		c.NetworkSecret = current.NetworkSecret
	}
	if !slices.Equal(derefPeers(base.Peers), derefPeers(current.Peers)) {
		c.Peers = current.Peers
	}

	if len(current.Flags) > 0 {
		flags := map[string]bool{}
		for k, v := range current.Flags {
			if bv, ok := base.Flags[k]; !ok || bv != v {
				flags[k] = v
			}
		}
		if len(flags) > 0 {
			c.Flags = flags
		}
	}

	if changedString(base.DevName, current.DevName) {
		c.DevName = current.DevName
	}
	if changedUint32(base.MTU, current.MTU) {
		c.MTU = current.MTU
	}
	if changedUint32(base.SocketMark, current.SocketMark) {
		c.SocketMark = current.SocketMark
	}
	if changedUint64(base.InstanceRecvBpsLimit, current.InstanceRecvBpsLimit) {
		c.InstanceRecvBpsLimit = current.InstanceRecvBpsLimit
	}
	if changedString(base.RelayNetworkWhitelist, current.RelayNetworkWhitelist) {
		c.RelayNetworkWhitelist = current.RelayNetworkWhitelist
	}
	if changedBool(base.IPv6PublicAddrAuto, current.IPv6PublicAddrAuto) {
		c.IPv6PublicAddrAuto = current.IPv6PublicAddrAuto
	}
	if changedBool(base.SecureModeEnabled, current.SecureModeEnabled) {
		c.SecureModeEnabled = current.SecureModeEnabled
	}
	if changedString(base.SecurePrivateKey, current.SecurePrivateKey) {
		c.SecurePrivateKey = current.SecurePrivateKey
	}
	if changedString(base.SecurePublicKey, current.SecurePublicKey) {
		c.SecurePublicKey = current.SecurePublicKey
	}
	if changedString(base.Socks5Proxy, current.Socks5Proxy) {
		c.Socks5Proxy = current.Socks5Proxy
	}
	if !slices.Equal(derefStrings(base.Routes), derefStrings(current.Routes)) {
		c.Routes = current.Routes
	}
	if !slices.Equal(derefStrings(base.ExitNodes), derefStrings(current.ExitNodes)) {
		c.ExitNodes = current.ExitNodes
	}
	if !slices.Equal(derefStrings(base.MappedListeners), derefStrings(current.MappedListeners)) {
		c.MappedListeners = current.MappedListeners
	}
	for _, field := range []struct {
		path          string
		base, current *[]string
		out           **[]string
	}{
		{"stun_servers", base.StunServers, current.StunServers, &c.StunServers},
		{"stun_servers_v6", base.StunServersV6, current.StunServersV6, &c.StunServersV6},
		{"tcp_stun_servers", base.TCPStunServers, current.TCPStunServers, &c.TCPStunServers},
		{"tcp_whitelist", base.TCPWhitelist, current.TCPWhitelist, &c.TCPWhitelist},
		{"udp_whitelist", base.UDPWhitelist, current.UDPWhitelist, &c.UDPWhitelist},
	} {
		if !reflect.DeepEqual(field.base, field.current) {
			if field.current == nil {
				c.Remove = append(c.Remove, field.path)
			} else {
				*field.out = field.current
			}
		}
	}
	for _, field := range []struct {
		path          string
		base, current *string
		out           **string
	}{
		{"credential_file", base.CredentialFile, current.CredentialFile, &c.CredentialFile},
		{"netns", base.NetNS, current.NetNS, &c.NetNS},
		{"ipv6_public_addr_prefix", base.IPv6PublicAddrPrefix, current.IPv6PublicAddrPrefix, &c.IPv6PublicAddrPrefix},
	} {
		if !reflect.DeepEqual(field.base, field.current) {
			if field.current == nil {
				c.Remove = append(c.Remove, field.path)
			} else {
				*field.out = field.current
			}
		}
	}
	if !reflect.DeepEqual(derefProxy(base.ProxyNetworks), derefProxy(current.ProxyNetworks)) {
		c.ProxyNetworks = current.ProxyNetworks
	}
	if !reflect.DeepEqual(derefForwards(base.PortForwards), derefForwards(current.PortForwards)) {
		c.PortForwards = current.PortForwards
	}

	// The VPN portal is presence-based: enabling sets both keys, disabling
	// removes the table. "current" for a disabled form is nil plus a
	// removal request, expressed by the caller through the nil pointer.
	if base.VpnPortal != nil && current.VpnPortal == nil && !currentRemoved(current, "vpn_portal_config") {
		c.Remove = append(c.Remove, "vpn_portal_config")
	}
	if current.VpnPortal != nil && (base.VpnPortal == nil || !reflect.DeepEqual(*base.VpnPortal, *current.VpnPortal)) {
		c.VpnPortal = current.VpnPortal
		if base.VpnPortal != nil {
			c.Remove = append(c.Remove, "vpn_portal_config")
		}
	}
	// Socks5 is a singleton URL: clearing it removes the key (an empty
	// URL would be invalid for the core).
	if base.Socks5Proxy != nil && current.Socks5Proxy == nil {
		c.Remove = append(c.Remove, "socks5_proxy")
	}

	// Callers may request removals explicitly.
	for _, path := range current.Remove {
		if !slices.Contains(c.Remove, path) && removalApplies(base, path) {
			c.Remove = append(c.Remove, path)
		}
	}
	return c
}

// currentRemoved reports whether current already asks for a removal.
func currentRemoved(current Values, path string) bool {
	return slices.Contains(current.Remove, path)
}

// removalApplies avoids removing keys the document does not have.
func removalApplies(base Values, path string) bool {
	switch path {
	case "socks5_proxy":
		return base.Socks5Proxy != nil
	case "vpn_portal_config":
		return base.VpnPortal != nil
	default:
		return false
	}
}

func changedString(base, current *string) bool {
	if current == nil {
		return false
	}
	return base == nil || *base != *current
}

func changedBool(base, current *bool) bool {
	if current == nil {
		return false
	}
	return base == nil || *base != *current
}

func changedUint32(base, current *uint32) bool {
	if current == nil {
		return false
	}
	return base == nil || *base != *current
}

func changedUint64(base, current *uint64) bool {
	if current == nil {
		return false
	}
	return base == nil || *base != *current
}

func derefStrings(list *[]string) []string {
	if list == nil {
		return nil
	}
	return *list
}

func derefPeers(list *[]Peer) []Peer {
	if list == nil {
		return nil
	}
	return *list
}

func derefProxy(list *[]ProxyNetwork) []ProxyNetwork {
	if list == nil {
		return nil
	}
	return *list
}

func derefForwards(list *[]PortForward) []PortForward {
	if list == nil {
		return nil
	}
	return *list
}

// ApplyValues applies the non-nil fields of v in a fixed order.
func ApplyValues(text string, v Values) (string, error) {
	var err error
	step := func(f func() (string, error)) {
		if err != nil {
			return
		}
		text, err = f()
	}
	// Removals first: disabling presence-based structures.
	for _, path := range v.Remove {
		if path == "vpn_portal_config" {
			step(func() (string, error) { return removeVpnPortal(text) })
			continue
		}
		step(func() (string, error) { return RemoveKey(text, path) })
	}
	if v.InstanceName != nil {
		step(func() (string, error) { return SetString(text, "instance_name", *v.InstanceName) })
	}
	if v.InstanceID != nil {
		step(func() (string, error) { return SetString(text, "instance_id", *v.InstanceID) })
	}
	if v.Hostname != nil {
		step(func() (string, error) { return SetString(text, "hostname", *v.Hostname) })
	}
	if v.IPv4 != nil {
		step(func() (string, error) { return SetString(text, "ipv4", *v.IPv4) })
	}
	if v.DHCP != nil {
		step(func() (string, error) { return SetBool(text, "dhcp", *v.DHCP) })
	}
	if v.Listeners != nil {
		step(func() (string, error) { return SetStringArray(text, "listeners", *v.Listeners) })
	}
	if v.NetworkName != nil {
		step(func() (string, error) { return SetString(text, "network_identity.network_name", *v.NetworkName) })
	}
	if v.NetworkSecret != nil {
		step(func() (string, error) { return SetString(text, "network_identity.network_secret", *v.NetworkSecret) })
	}
	// Flags in sorted order for a reproducible rewrite.
	if len(v.Flags) > 0 {
		keys := make([]string, 0, len(v.Flags))
		for k := range v.Flags {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			b := v.Flags[k]
			step(func() (string, error) { return SetBool(text, "flags."+k, b) })
		}
	}
	if v.DevName != nil {
		step(func() (string, error) { return SetString(text, "flags.dev_name", *v.DevName) })
	}
	if v.MTU != nil {
		step(func() (string, error) { return SetInt(text, "flags.mtu", int64(*v.MTU)) })
	}
	if v.SocketMark != nil {
		step(func() (string, error) { return SetInt(text, "flags.socket_mark", int64(*v.SocketMark)) })
	}
	if v.InstanceRecvBpsLimit != nil {
		step(func() (string, error) {
			return SetValue(text, "flags.instance_recv_bps_limit", fmt.Sprintf("%d", *v.InstanceRecvBpsLimit))
		})
	}
	if v.RelayNetworkWhitelist != nil {
		step(func() (string, error) {
			return SetString(text, "flags.relay_network_whitelist", *v.RelayNetworkWhitelist)
		})
	}
	if v.IPv6PublicAddrAuto != nil {
		step(func() (string, error) { return SetBool(text, "ipv6_public_addr_auto", *v.IPv6PublicAddrAuto) })
	}
	if v.Socks5Proxy != nil {
		step(func() (string, error) { return SetString(text, "socks5_proxy", *v.Socks5Proxy) })
	}
	if v.Routes != nil {
		step(func() (string, error) { return SetStringArray(text, "routes", *v.Routes) })
	}
	if v.ExitNodes != nil {
		step(func() (string, error) { return SetStringArray(text, "exit_nodes", *v.ExitNodes) })
	}
	if v.MappedListeners != nil {
		step(func() (string, error) { return SetStringArray(text, "mapped_listeners", *v.MappedListeners) })
	}
	for _, field := range []struct {
		path  string
		value *[]string
	}{
		{"stun_servers", v.StunServers}, {"stun_servers_v6", v.StunServersV6}, {"tcp_stun_servers", v.TCPStunServers}, {"tcp_whitelist", v.TCPWhitelist}, {"udp_whitelist", v.UDPWhitelist},
	} {
		if field.value != nil {
			step(func() (string, error) { return SetStringArray(text, field.path, *field.value) })
		}
	}
	for _, field := range []struct {
		path  string
		value *string
	}{
		{"credential_file", v.CredentialFile}, {"netns", v.NetNS}, {"ipv6_public_addr_prefix", v.IPv6PublicAddrPrefix},
		{"secure_mode.local_private_key", v.SecurePrivateKey}, {"secure_mode.local_public_key", v.SecurePublicKey},
	} {
		if field.value != nil {
			step(func() (string, error) { return SetString(text, field.path, *field.value) })
		}
	}
	if v.ProxyNetworks != nil {
		step(func() (string, error) { return SetProxyNetworks(text, *v.ProxyNetworks) })
	}
	if v.SecureModeEnabled != nil {
		step(func() (string, error) { return SetBool(text, "secure_mode.enabled", *v.SecureModeEnabled) })
	}
	if v.PortForwards != nil {
		step(func() (string, error) { return SetPortForwards(text, *v.PortForwards) })
	}
	if v.VpnPortal != nil {
		step(func() (string, error) { return SetVpnPortal(text, *v.VpnPortal) })
	}
	if v.Peers != nil {
		step(func() (string, error) { return SetPeers(text, *v.Peers) })
	}
	return text, err
}

// IsEmpty reports whether no field would be written.
func (v Values) IsEmpty() bool {
	return v.InstanceName == nil && v.InstanceID == nil && v.Hostname == nil && v.IPv4 == nil &&
		v.DHCP == nil && v.Listeners == nil && v.NetworkName == nil &&
		v.NetworkSecret == nil && v.Peers == nil && len(v.Flags) == 0 &&
		v.DevName == nil && v.MTU == nil && v.SocketMark == nil && v.InstanceRecvBpsLimit == nil &&
		v.RelayNetworkWhitelist == nil && v.IPv6PublicAddrAuto == nil &&
		v.Socks5Proxy == nil && v.Routes == nil && v.ExitNodes == nil &&
		v.MappedListeners == nil && v.ProxyNetworks == nil &&
		v.StunServers == nil && v.StunServersV6 == nil && v.TCPStunServers == nil && v.TCPWhitelist == nil && v.UDPWhitelist == nil && v.CredentialFile == nil && v.NetNS == nil && v.IPv6PublicAddrPrefix == nil &&
		v.PortForwards == nil && v.VpnPortal == nil && v.SecureModeEnabled == nil && v.SecurePrivateKey == nil && v.SecurePublicKey == nil && len(v.Remove) == 0
}

// verify re-parses the result; a rewrite must never produce invalid TOML.
func verify(text string) error {
	var anyDoc map[string]any
	if err := toml.Unmarshal([]byte(text), &anyDoc); err != nil {
		return fmt.Errorf("%w: edit would produce invalid TOML (%v)", ErrUnsupported, err)
	}
	return nil
}

// tomlQuote encodes a string as a TOML basic string.
func tomlQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\u%04X`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

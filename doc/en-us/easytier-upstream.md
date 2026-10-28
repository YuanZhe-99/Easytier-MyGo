# EasyTier upstream

Facts about EasyTier that this application depends on. Each was verified against the stated
version; re-check them when the bundled version moves.

## Releases

- Pinned in `third_party/easytier.json` (bundled tag, tested tags, per-platform URL and SHA-256).
  `go run ./tools/deps check` compares it with the latest stable release.
- Asset names: `easytier-<os>-<arch>-<tag>.zip` with `linux|macos|windows` and `x86_64`, plus
  `aarch64` for Linux and macOS but `arm64` for Windows. Each archive has one top-level directory
  holding `easytier-core`, `easytier-cli` and the web console binaries (unused here). Windows
  archives also carry `wintun.dll`, `Packet.dll` and `WinDivert64.sys`.
- Checksums: v2.6.4 publishes none as a file; the GitHub API's `assets[].digest` carries SHA-256.
  From v2.7.0 a `SHA256SUMS` asset exists.
- **The release executables are UPX-packed.** Every start decompresses them, which costs about
  250 ms of CPU per `easytier-cli` call (v2.6.4, Linux x86_64, measured 2026-10-16); unpacked, the
  same call takes about 3 ms, and a real query against a running core (`node`, `-v peer`, `peer`,
  `route`) about 4 ms. `tools/deps fetch` unpacks them with `upx -d`.
- Always ship `easytier-core` and `easytier-cli` from the same release; their RPC client changed in
  v2.7.0.
- License: LGPL-3.0.

## CLI contract (v2.6.4)

- `-o json node` returns node info. Its `config` field holds the full TOML including
  `network_secret`.
- `-o json peer` returns display rows (byte counts and latency as strings); `-v -o json peer`
  returns route/connection pairs with numeric counters, which traffic rates need.
- With one instance the JSON is the value itself; with several it is
  `[{instance_id, instance_name, result}]`.
- No running instance: `peer` fails with `no running instances found` (exit 1).
- RPC unreachable: the first line is `Error: failed to get manage client`, the body contains
  `Connection refused`.
- `service status` ignores `-o json` and prints `Service is running`, `Service is stopped` or
  `Service is not installed`.
- `easytier-core --check-config -c FILE` parses a config without starting a network; it does not
  check ports, TUN or permissions.

## Changes seen in v2.7.0 (pre-release)

Verified 2026-10-16 with `v2.7.0-8b7f1f01`: 64-bit counters (`rx_bytes`, `latency_us`, …) are
quoted strings, NAT types are names (`"Symmetric"`), and default-valued fields are omitted. The
adapter accepts both forms (`coreadapter.Uint64`, `NatType.UnmarshalJSON`); fixtures for both live
in `internal/coreadapter/testdata`. Networks connected and peered on v2.7.0 through the core
manager.

Configuration, read from the v2.7.0 source (`easytier-core/src/config/toml.rs`) and checked live
on 2026-10-28 (the live adapter test passes; `--check-config` accepts the documents):

- `[vpn_portal_config]` refuses unknown keys. `client_cidr` (v2.6) is gone; the portal is
  `wireguard_listen`, an optional `wireguard_private_key` and `enabled`, and named clients in
  `[[vpn_portal_config.clients]]` (`name`, `virtual_ip` with its prefix, `groups`). v2.6.4 in turn
  requires `client_cidr`, so no portal document works on both.
- New keys: `tcp_stun_servers` (top level), `[flags] socket_mark` (Linux, CAP_NET_ADMIN),
  `[flags] prefer_peer_relay`, and `[[managed_credentials]]`. v2.6.4 ignores unknown top-level
  keys.
- `--config-server` needs a full URL (`udp://host:22020/TOKEN`); this application does not pass
  it.
- `configdoc.VersionConflicts` names what a network's core version refuses or ignores; the
  editor shows it. Applying runs `--check-config` with the network's own core.

## EasyTier mini (v2.7.0)

A compact client built from the same source, released apart in `EasyTier/easytier-mini`
(LGPL-3.0). Read from `easytier-contrib/easytier-mini` and the compact runtime
(`compact_runtime_core_host_config`), verified live on 2026-10-28:

- Assets: `easytier-mini-<os>-<arch>-<tag>.zip` with the same platform names; the files sit at the
  archive's root: `easytier-mini` alone (plus `wintun.dll` on Windows), about 2.4 MB, not
  UPX-packed. The GitHub API publishes each asset's SHA-256, and `SHA256SUMS` exists.
- No CLI of its own. Its read-only management RPC answers the full release's `easytier-cli` of
  the same tag for `node`, `peer`, `-v peer`, `route` and `connector`; `node` carries no `config`
  field. The core manager installs mini with that CLI (`mini-<tag>`).
- Arguments: `-c/--config`, `-w/--config-server`, `--machine-id`, `--hostname`, `--secure-mode`;
  anything else is refused. No `--check-config`; a document that does not load ends the process.
- The management port is fixed at `127.0.0.1:15888` (`--rpc-portal` and `rpc_portal` are not
  read), so one mini process runs per computer.
- It runs `tcp://` and `udp://` listeners, mapped listeners and peers, TUN or `no_tun`
  (smoltcp), DHCP, STUN, UDP hole punching and AES-GCM. It silently ignores other URL schemes,
  subnet proxy (`proxy_network`), exit nodes (using or being one), SOCKS5, port forwarding, the
  WireGuard portal, public IPv6 addresses, MagicDNS, KCP and QUIC proxying, UDP broadcast relay,
  UPnP, TCP hole punching and compression; ChaCha20 falls back to AES-GCM.
  `configdoc.StripForMini` removes exactly these from a copy.
- A v2.7.0 core and a mini node peer directly over TCP.

## Verified quirks

- Two local cores do not handshake over loopback: the connector binds source addresses to
  interfaces, so dial the machine's interface address instead (`tcp://<iface-ip>:<port>`).
- `--rpc-portal` defaults to 15888 when unset; always pass an explicit port.
- Without `--daemon`-style supervision a core can be alive with no running instance; health checks
  cover this.
- MagicDNS (v2.6.4 source, `instance/dns_server`): with `[flags] accept_dns = true` a node
  publishes an A record `<hostname>.<zone>` for every route with a hostname. The zone is
  `[flags] tld_dns_zone`, `et.net.` by default (`DEFAULT_ET_DNS_ZONE`); the hostname is used as it
  is, so names with spaces or dots do not form a single label.

## Privileges

- TUN needs CAP_NET_ADMIN (Linux), root (macOS, `utun`) or administrator rights with `wintun.dll`
  (Windows). `no_tun = true` needs none: the node is reachable through its virtual IP and can act as
  a subnet proxy, but cannot open connections to other nodes.
- The official EasyTier GUI elevates itself as a whole (pkexec, AuthorizationExecuteWithPrivileges,
  UAC) and runs the core in-process. This application keeps the GUI unprivileged
  ([run-modes.md](run-modes.md)).

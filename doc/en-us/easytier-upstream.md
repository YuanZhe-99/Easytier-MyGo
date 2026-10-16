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
  same call takes about 3 ms. `tools/deps fetch` unpacks them with `upx -d`.
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

## Verified quirks

- Two local cores do not handshake over loopback: the connector binds source addresses to
  interfaces, so dial the machine's interface address instead (`tcp://<iface-ip>:<port>`).
- `--rpc-portal` defaults to 15888 when unset; always pass an explicit port.
- Without `--daemon`-style supervision a core can be alive with no running instance; health checks
  cover this.

## Privileges

- TUN needs CAP_NET_ADMIN (Linux), root (macOS, `utun`) or administrator rights with `wintun.dll`
  (Windows). `no_tun = true` needs none: the node is reachable through its virtual IP and can act as
  a subnet proxy, but cannot open connections to other nodes.
- The official EasyTier GUI elevates itself as a whole (pkexec, AuthorizationExecuteWithPrivileges,
  UAC) and runs the core in-process. This application keeps the GUI unprivileged
  ([run-modes.md](run-modes.md)).

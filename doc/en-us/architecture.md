# Architecture

## Processes

| Process | Binary | Runs as | Role |
|---|---|---|---|
| GUI | `easytier-mygo` | the user, never elevated | Window, tray, settings. A client of the daemon; holds no network state of its own. |
| Daemon | `easytier-mygo-svc` | the user (no-privilege mode) or root/SYSTEM (privileged helper) | Owns network profiles, starts one `easytier-core` per running network, monitors health, performs bounded recovery, serves the local API. |
| Core | `easytier-core` + `easytier-cli` | inherited from the daemon | The EasyTier network itself and its management CLI, always a matching pair from one release. |
| Control CLI | `easytier-mygo-ctl` | the user | Scriptable client of the local API. |

The daemon runs in one of two privilege contexts, chosen by the run mode
([run-modes.md](run-modes.md)): a user-level child of the GUI, or the privileged helper registered
once with the operating system. The API and behavior are the same in both; only authorization,
paths and lifetime differ.

## Packages

| Package | Role |
|---|---|
| `internal/health` | Side-effect-free health engine: verdicts, causes, bounded recovery decisions. |
| `internal/configdoc` | Raw TOML documents: revisions, conflict-checked atomic saves, backups, lossless form projection. |
| `internal/coreadapter` | Typed, bounded wrapper around `easytier-cli` output with error classification. |
| `internal/telemetry` | Counter samples to rates and a sequenced ring of curve points; gaps and resets stay visible. |
| `internal/localapi` | The versioned newline-delimited JSON protocol over a local socket, sessions, peer-credential authorization. |
| `internal/profiles` | Network store: raw documents, metadata, run intent, frozen applied revisions. |
| `internal/supervisor` | One worker per network: core process, checks, bounded recovery, apply with rollback, traffic. |
| `internal/daemon` | The host: workers, run-mode decisions, sessions, API handlers. |
| `internal/helper` | Installing, removing and inspecting the privileged helper (Linux: systemd). |
| `internal/bundlesig` | Signing and verifying the app bundle the helper updates itself from (Linux, Windows; macOS checks the code signature); verified files are returned as bytes, never re-read. |
| `internal/sdnotify` | systemd notifications and socket activation. |
| `internal/fsutil` | Atomic file writes and JSON documents. |
| `internal/gui` | The GUI: daemon connection, session, views, tray. |
| `internal/version` | The single product version. |
| `third_party` | The embedded EasyTier release manifest. |
| `tools/deps` | Upstream pin maintenance and bundled-core download. |

A network runs when the user wants it connected **and** the run mode allows it: a GUI session is
attached, or the helper is in background mode. Each running network has its own core with an
explicit management port allocated from 25888 upwards. Applying a saved document first runs the
core's `--check-config`, then restarts the core with a frozen copy; if the new revision fails within
the confirmation window (30 s), the worker returns to the revision that was running.

Checks run every second while a session displays the network (or while its core is not healthy)
and every 10 s otherwise; one `node` and one `-v peer` query feed both health and traffic.

## Performance

Targets: with the dashboard open and refreshing every second, the daemon and GUI together use at
most 5 % of one core per network; hidden in the tray, at most 1 %. Sessions receive changes as
pushed increments (at most every 500 ms), the curve lives in a fixed ring buffer, and one
`node` plus one `-v peer` query feeds both health and traffic. Measured values are in
[platform-notes.md](platform-notes.md).

## Cores

The daemon runs the core selected in its settings: the bundled one (default), an official release
it downloaded on request, or a custom build. `internal/cores` reads the release list from the
GitHub API (cached with its ETag), downloads only on request, verifies the published SHA-256, and
unpacks UPX-packed executables with the bundled `upx`. Each core lives in `<state>/cores/<tag>/`
with its matching CLI. Switching cores restarts running networks on the new core. Versions in
`third_party/easytier.json` → `tested` are labeled tested. The privileged helper downloads official
cores itself; a custom build for the helper is added only through
`easytier-mygo-svc helper add-core` with administrator approval.

## Local API

- Transport: a Unix domain socket (named pipe on Windows planned); newline-delimited JSON with a
  `version` field. One request per connection, except a session (`session.attach`), which keeps
  its connection and receives pushed frames (network statuses, new traffic points, new events) at
  most every 500 ms. Errors carry a code (`not-found`, `conflict`, `invalid-config`,
  `needs-helper`, …).
- `localapi.ProtocolVersion` is checked by both sides. Enums cross the socket as names. Changing a
  field's meaning requires a version bump.
- Authorization comes from the operating system's peer credentials, never from the request body.
  Read-only and write tiers protect this application's control entry point.
- The core's own `--rpc-portal` (TCP on loopback) is a separate entry point reachable by local
  users; this application does not claim to close it.

## Cross-cutting rules

- **Health semantics are the product contract** — see [health-monitoring.md](health-monitoring.md).
- **Never show stale data as current.** Every snapshot carries its sample times; the GUI marks
  stale data and lost connections.
- **Secrets never travel.** `easytier-cli node` returns the raw TOML including `network_secret`;
  it is blanked before anything is cached, logged or sent over the API. Event details are scrubbed
  before they are stored.
- **Configs are raw documents** — see [configuration.md](configuration.md).
- **One instance per core.** Multi-instance CLI output is reported as `ErrMultiInstance`, never
  misparsed.

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
| `internal/telemetry` | Counter samples to rates; gaps and resets stay visible. |
| `internal/localapi` | The versioned newline-delimited JSON protocol over a local socket, peer-credential authorization. |
| `internal/daemon` | Process supervision, the check loop, the API handlers. |
| `internal/platformservice` | Linux systemd unit generation and installation. |
| `internal/sdnotify` | systemd readiness and watchdog notifications. |
| `internal/app` | The GUI. |
| `internal/version` | The single product version. |
| `third_party` | The embedded EasyTier release manifest. |
| `tools/deps` | Upstream pin maintenance and bundled-core download. |

The daemon and GUI packages still carry the inherited single/manifest design; the rework replaces
them (profiles, per-network supervisors, the privileged helper, the core manager). This document is
updated as each part lands.

## Local API

- Transport: a Unix domain socket (named pipe on Windows), one request per connection today;
  newline-delimited JSON with a `version` field.
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

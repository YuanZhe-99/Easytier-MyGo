# Function index

One row per package: its role and the declarations to read first. Declarations carry the Function
Explanation Layer in their Go doc comments (first sentence: purpose; then inputs, returns, side
effects and notes where non-trivial).

| Package | Role | Start with |
|---|---|---|
| `cmd/easytier-mygo` | GUI entry point | `main` |
| `cmd/easytier-mygo-svc` | Daemon entry point and service subcommands | `main`, `runDaemon` |
| `cmd/easytier-mygo-ctl` | Local API command-line client | `main` |
| `internal/health` | Health engine | `Engine.Observe`, `Policy`, `DefaultPolicy` |
| `internal/configdoc` | Raw config documents | `Store`, `Extract`, `Changes`, `ApplyValues` |
| `internal/coreadapter` | `easytier-cli` wrapper | `CLI`, `KindOf`, `ErrorKind` |
| `internal/telemetry` | Rates from counters | `Tracker.Observe`, `Point` |
| `internal/localapi` | Local API protocol and server | `ProtocolVersion`, `Client`, `Authorizer` |
| `internal/daemon` | Supervision and API handlers | `RunService`, `Daemon` |
| `internal/platformservice` | systemd units | `Install`, `Uninstall` |
| `internal/sdnotify` | systemd notifications | `FromEnv`, `Notifier.Watchdog` |
| `internal/app` | GUI views and tray | `App`, `App.View` |
| `internal/version` | Product version | `Version` |
| `third_party` | Embedded EasyTier manifest | `EasyTier`, `EasyTierPlatformName` |
| `tools/deps` | Upstream pins and core download | `check`, `update`, `fetch` |
| `tools/checkdocs` | Documentation mirror check | `main` |

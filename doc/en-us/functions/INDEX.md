# Function index

One row per package: its role and the declarations to read first. Declarations carry the Function
Explanation Layer in their Go doc comments (first sentence: purpose; then inputs, returns, side
effects and notes where non-trivial).

| Package | Role | Start with |
|---|---|---|
| `cmd/easytier-mygo` | GUI entry point | `main` |
| `internal/gui` | GUI state, views, tray | `App`, `App.view`, `App.connectLoop`, `App.peerList`, `App.titleBar`, `TitleBarOptions`, `friendlyName`, `vendorLogo`, `magicDNSName` |
| `cmd/easytier-mygo-svc` | Daemon entry point and helper subcommands | `run`, `helperCmd` |
| `cmd/easytier-mygo-ctl` | Local API command-line client | `main` |
| `internal/health` | Health engine | `Engine.Observe`, `Policy`, `DefaultPolicy` |
| `internal/configdoc` | Raw config documents | `Store`, `Extract`, `Changes`, `ApplyValues`, `NewUUID`, `MagicDNSZone` |
| `internal/coreadapter` | `easytier-cli` wrapper | `CLI`, `KindOf`, `ErrorKind` |
| `internal/telemetry` | Rates from counters, curve ring | `Tracker.Observe`, `Ring` |
| `internal/localapi` | Local API protocol and server | `ProtocolVersion`, `Request`, `Client.Attach`, `Authorizer` |
| `internal/profiles` | Network store | `Store`, `Profile` |
| `internal/supervisor` | Per-network worker | `Worker`, `Config`, `Preflight` |
| `internal/daemon` | Host, run-mode decisions, API | `Run`, `Options`, `Daemon.reconcile` |
| `internal/helper` | Privileged helper installation and self-update | `Install`, `Uninstall`, `Inspect`, `SelfUpdate`, `installBundle`, `replaceSigned` (macOS) |
| `internal/bundlesig` | Signed app bundles for helper self-update | `Sign`, `Read`, `Bundle` |
| `internal/cores` | EasyTier core manager | `Manager.Releases`, `Manager.Install`, `Binaries` |
| `internal/fsutil` | Atomic writes | `WriteFileAtomic`, `WriteJSON` |
| `internal/sdnotify` | systemd notifications, socket activation | `FromEnv`, `Listeners` |
| `internal/version` | Product version | `Version` |
| `third_party` | Embedded EasyTier manifest | `EasyTier`, `EasyTierPlatformName` |
| `tools/deps` | Upstream pins and core download | `check`, `update`, `fetch` |
| `tools/checkdocs` | Documentation mirror check | `main` |

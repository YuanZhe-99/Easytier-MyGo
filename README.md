# EasyTier MyGo

A native desktop client for [EasyTier](https://github.com/EasyTier/EasyTier), built in Go with
[MyGo](https://github.com/egoist/mygo). A small daemon runs the EasyTier core, watches its health,
and recovers local faults without ever overriding what you asked for.

> **Status:** version 0.1.0 is being rebuilt and has not been released. Linux is the primary
> platform; Windows and macOS backends are experimental. See [PLAN.md](PLAN.md).

## Highlights (0.1.0 goals)

- Several EasyTier networks side by side, each with live traffic, peers and routes.
- Three run modes: **No privileges** (`no_tun` networks), **Follow GUI** (networks run while the app
  runs) and **Background service** (networks run from boot). Administrator authorization happens
  once, when the privileged helper is installed — never when the app starts.
- Choose your EasyTier core: the bundled stable release, another official release, or your own
  build, without updating the app.
- Lossless configuration editing (form or raw TOML) with conflict checks, backups and rollback.
- Health monitoring that tells local faults from outages, with bounded recovery.
- English, Simplified Chinese, Traditional Chinese and Japanese.

## Build

Requirements: Go 1.27.2; on Linux a graphical desktop, `libayatana-appindicator3-1` for the tray and
`upx` to unpack the bundled core.

```bash
go run ./tools/deps fetch      # pinned EasyTier core, verified and unpacked into resources/
go build -o build/easytier-mygo-svc ./cmd/easytier-mygo-svc
go build -o build/easytier-mygo-ctl ./cmd/easytier-mygo-ctl
go tool mygo build
```

Development commands and the dev loop are in [doc/en-us/development.md](doc/en-us/development.md).

## Network access

The app itself contacts only GitHub: `api.github.com` and `github.com` when you check for or
download EasyTier cores. Networks you configure connect wherever their EasyTier configuration says.

## Documentation

- [English documentation](doc/en-us/README.md) · [中文文档](doc/zh-cn/README.md)
- [Architecture](doc/en-us/architecture.md) · [Run modes](doc/en-us/run-modes.md) ·
  [Version history](doc/en-us/version-history.md)

## Acknowledgements

- **[EasyTier](https://github.com/EasyTier/EasyTier)** provides the networking core and CLI.
- **[MyGo](https://github.com/egoist/mygo)** provides the native Go UI framework and tooling.
- **[EasyTier-iOS](https://github.com/EasyTier/EasyTier-iOS)** inspired the interface design; its
  icon is used temporarily (see [resources/NOTICE](resources/NOTICE)).

## License

EasyTier MyGo is licensed under [GPL-3.0](LICENSE). The bundled EasyTier binaries are LGPL-3.0;
distributions include their license text and source location.

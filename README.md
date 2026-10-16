# EasyTier MyGo

A native desktop client for [EasyTier](https://github.com/EasyTier/EasyTier), built in Go with
[MyGo](https://github.com/egoist/mygo). A small daemon runs the EasyTier core, watches its health,
and recovers local faults without ever overriding what you asked for.

> **Status:** 0.1.0 is the first release. Linux is fully tested; the Windows (x64, ARM64) and macOS
> backends pass CI smoke tests but have not been tried on real machines yet. See the
> [platform notes](doc/en-us/platform-notes.md).

## Highlights

- Several EasyTier networks side by side, each with live traffic, nodes and shared subnets.
- Three run modes: **No privileges** (`no_tun` networks), **Follow GUI** (networks run while the app
  runs) and **Background service** (networks run from boot). Administrator authorization happens
  once, when the privileged helper is installed — never when the app starts.
- Choose your EasyTier core: the bundled stable release, another official release, or your own
  build, without updating the app.
- Lossless configuration editing (form or raw TOML) with conflict checks, backups and rollback.
- Health monitoring that tells local faults from outages, with bounded recovery.
- English, Simplified Chinese, Traditional Chinese and Japanese.

## Build

Requirements: Go 1.27.2; on Linux a graphical desktop and `libayatana-appindicator3-1` for the tray.

```bash
scripts/build.sh               # this platform: core, daemon, control CLI, then `mygo build`
scripts/build.sh linux/amd64 windows/amd64
```

Artifacts land in `build/<goos>-<goarch>/`: on Linux a `.tar.gz` (install with `install.sh`, no
root) and a `.deb`.

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
- **[EasyTier-iOS](https://github.com/EasyTier/EasyTier-iOS)** inspired the interface design; the app
  icon uses its artwork (see [resources/NOTICE](resources/NOTICE)).

## License

EasyTier MyGo is licensed under [GPL-3.0](LICENSE). The bundled EasyTier binaries are LGPL-3.0;
distributions include their license text and source location.

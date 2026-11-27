# EasyTier MyGo

A native desktop client for [EasyTier](https://github.com/EasyTier/EasyTier), built in Go with
[MyGo](https://github.com/egoist/mygo). A small daemon runs the EasyTier core, watches its health,
and recovers local faults without ever overriding what you asked for.

> **Status:** 0.3.0. Linux is fully tested; Windows (x64, ARM64) and macOS pass CI smoke tests and
> are partly verified on real machines. See the [platform notes](doc/en-us/platform-notes.md) and
> the [version history](doc/en-us/version-history.md).

## Highlights

- Several EasyTier networks side by side, each with live traffic, nodes and shared subnets.
- A peer list that reads like a device list: friendly names (`macbook-pro` → MacBook Pro), vendor
  logos matched from the hostname, and one-click copy of a peer's IP address or MagicDNS name.
- Three run modes: **No privileges** (`no_tun` networks), **Follow GUI** (networks run while the app
  runs) and **Background service** (networks run from boot). Administrator authorization happens
  once, when the privileged helper is installed — never when the app starts.
- A first-run tour that shows what each run mode means for this computer — the virtual device,
  startup and sign-in, staying awake — with animated illustrations; it can be skipped and watched
  again from Settings.
- Choose your EasyTier core: the bundled stable release, another official release, or your own
  build, without updating the app.
- Lossless configuration editing (form or raw TOML) with conflict checks, backups and rollback.
- Health monitoring that tells local faults from outages, with bounded recovery.
- HTTPS for MagicDNS names: a private CA issues certificates for this computer's MagicDNS name,
  hostname and virtual IP, keeps them renewed in a folder of your choice, and reloads your web
  server afterwards (see [Local HTTPS certificates](#local-https-certificates)).
- Signed self-update from GitHub releases, checked daily (can be turned off) and installed only when
  you ask.
- Lives in the tray; on macOS closing the window also hides the Dock icon.
- English, Simplified Chinese, Traditional Chinese and Japanese.

## Build

Requirements: Go 1.27.3; on Linux a graphical desktop and `libayatana-appindicator3-1` for the tray.

```bash
scripts/build.sh               # this platform: core, daemon, control CLI, then `mygo build`
scripts/build.sh linux/amd64 windows/amd64
```

Artifacts land in `build/<goos>-<goarch>/`: on Linux a `.tar.gz` (install with `install.sh`, no
root) and a `.deb`; on Windows an `.exe` and an installer; on macOS an `.app` and a `.dmg`.

Development commands and the dev loop are in [doc/en-us/development.md](doc/en-us/development.md).

## Local HTTPS certificates

EasyTier's MagicDNS names (`laptop.et.net`) have no certificates of their own. The daemon can run a
private certificate authority and keep server certificates current for them; details are in
[doc/en-us/certificates.md](doc/en-us/certificates.md). In the app it is all in Settings ›
Certificates; the same from a terminal:

```bash
# 1. Create the CA (ECDSA P-256, 10 years), or import yours: cert ca-import CA.pem CA-KEY.pem
easytier-mygo-ctl cert ca-generate -cn "Home CA"

# 2. Trust it on this computer; without sudo this prints the exact command to run instead
sudo easytier-mygo-ctl cert trust install -file ~/.local/state/easytier-mygo/certs/ca.crt
#    and export it for phones and other computers
easytier-mygo-ctl cert ca-export home-ca.crt

# 3. A certificate for a network's names plus extra ones, renewed automatically
easytier-mygo-ctl list                       # the network ID
easytier-mygo-ctl cert job-set -id web -network NETWORK-ID \
  -dns '*.laptop.et.net' -ip 192.168.1.20 \
  -out /srv/certs/web -hook 'caddy reload --config /etc/caddy/Caddyfile'
easytier-mygo-ctl cert status
```

The same job as JSON (`easytier-mygo-ctl cert job-set -file web.json`):

```json
{
  "id": "web",
  "network": "NETWORK-ID",
  "dns_names": ["*.laptop.et.net"],
  "ips": ["192.168.1.20"],
  "out_dir": "/srv/certs/web",
  "validity_days": 90,
  "renew_before_days": 30,
  "key_mode": "0640",
  "hook": {
    "enabled": true,
    "command": "caddy reload --config /etc/caddy/Caddyfile",
    "timeout_seconds": 30
  }
}
```

The folder then holds `cert.pem` (the certificate), `fullchain.pem` (certificate + CA) and
`key.pem`. Certificates are valid for 90 days and renewed 30 days before they expire, when the
network's name or address changes, or when the CA changes; the hook runs only after a new
certificate was written. Use them in Caddy with:

```caddyfile
laptop.et.net, grafana.laptop.et.net {
	tls /srv/certs/web/fullchain.pem /srv/certs/web/key.pem
	reverse_proxy 127.0.0.1:3000
}
```

Nginx takes the same files (`ssl_certificate .../fullchain.pem; ssl_certificate_key .../key.pem;`);
for a container, mount the folder read-only and reload with a hook such as `docker restart caddy`.
The hook runs with the daemon's rights. In the privileged helper (Follow GUI and Background service
modes) it runs as administrator, so a folder of your choice and a hook need an administrator's
approval once: the app asks when you save; `cert job-set` prints the command to run.

## Network access

The app itself contacts only GitHub: `api.github.com` and `github.com` when you check for or
download EasyTier cores, and when it checks for or downloads its own signed updates (the daily
check can be turned off in Settings → About). Vendor logos are bundled, never fetched. Networks you
configure connect wherever their EasyTier configuration says.

## Documentation

- [English documentation](doc/en-us/README.md) · [中文文档](doc/zh-cn/README.md)
- [Architecture](doc/en-us/architecture.md) · [Run modes](doc/en-us/run-modes.md) ·
  [Version history](doc/en-us/version-history.md)

## Acknowledgements

- **[EasyTier](https://github.com/EasyTier/EasyTier)** provides the networking core and CLI.
- **[MyGo](https://github.com/egoist/mygo)** provides the native Go UI framework and tooling.
- **[EasyTier-iOS](https://github.com/EasyTier/EasyTier-iOS)** inspired the interface design; the app
  icon uses its artwork (see [resources/NOTICE](resources/NOTICE)).
- **[Arion](https://github.com/AstraFoundry/Arion)** inspired the first-run tour: its onboarding's
  layout of an illustration beside each step. No code is taken from it.
- **[Lucide](https://lucide.dev)** provides the interface icons (ISC).
- **Vendor logos** come mostly from [Simple Icons](https://simpleicons.org/) (CC0) and Wikimedia
  Commons; each is a trademark of its owner, used only to identify a device's maker (see
  [SOURCES.md](internal/gui/assets/logos/SOURCES.md)).

## License

EasyTier MyGo is licensed under [GPL-3.0](LICENSE). The bundled EasyTier binaries are LGPL-3.0;
distributions include their license text and source location.

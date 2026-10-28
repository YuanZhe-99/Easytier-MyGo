# Platform notes

| Platform | Status |
|---|---|
| Linux (systemd) | Primary platform. New daemon, run modes and helper implemented and verified (below); the GUI's shell is rebuilt in 0.2.0 (verified in the view tests and screenshots, not yet on a real desktop). |
| macOS | Implemented; CI smoke tests (user daemon and helper, `no_tun`) pass on GitHub runners; the user daemon, the GUI's daemon child and the LaunchDaemon helper (installed with `sudo`, `no_tun` and TUN) are verified on a real machine (below); not verified on a real machine: administrator prompt through `osascript`, signed self-update. |
| Windows (x64, ARM64) | Implemented; CI smoke tests (user daemon and helper, `no_tun`) pass on GitHub runners; the user daemon and the service helper (installed through UAC, SID authorization over AF_UNIX, `no_tun`) are verified on a real ARM64 machine (below); not verified on a real machine: the GUI, TUN (WinTun), job object for cores, signed self-update. |

## Linux

- Development machine: Debian 13, XFCE on X11, StatusNotifier tray host present, pkexec available.
- The tray uses `libayatana-appindicator3` (falling back to `libappindicator3`); without it the
  app runs without a tray.
- MyGo's login item writes an XDG autostart entry.

Verified on the development machine (2026-10-16, EasyTier v2.6.4, isolated ports):

| Check | Result |
|---|---|
| User daemon, two `no_tun` networks | Start when a session attaches, stop when it ends; intent kept |
| Lifeline | Pipe EOF stops the cores and the daemon |
| Crash recovery | `kill -9` of a core → restarted by bounded recovery |
| Apply rollback | A revision whose core exits returns to the running revision |
| Helper install (pkexec) | Binaries, group, operator, units; socket active, service on demand |
| Helper with TUN | Two TUN networks (`etm0`, `etm1`) run as root, peer, ping |
| Follow GUI | Session end stops cores and removes the devices; 60 s idle exit; socket reactivation |
| Background mode | Service enabled at boot; networks run without a session; off restores Follow GUI (no reboot tested) |
| Core manager (user daemon) | Download v2.7.0 (SHA-256 verified), switch running networks to it and back, remove it |
| Core manager (helper) | The helper downloads v2.7.0 itself; TUN networks run on it and back on the bundled core |
| GUI (real desktop, XFCE) | Live data, chart, peers, tray registered; kill -9 or SIGTERM of the GUI stops its daemon and cores |
| CPU, two `no_tun` networks, 120 s | Dashboard visible: GUI 3.5 %, daemon 1.3 %, cores 0.9 % (5.7 % of one core in total). Hidden in the tray: GUI 0.4 %, daemon 0.4 %, cores 0.6 % (1.4 %). GUI RSS 140–160 MB, daemon 18 MB |
| GUI run-mode switch | No privileges → Follow GUI without a prompt; networks copied to the helper and connected; quitting the GUI stops them |
| GUI Background service | No privileges → Background: networks move to the helper, service enabled at boot; quitting the GUI keeps them running; Background → No privileges stops them in the helper, disables the boot start and connects them in the user daemon; no port conflicts during switches |
| Open at login | The switch writes `~/.config/autostart/EasyTier-MyGo.desktop`; a login launch starts hidden in the tray and connects the networks |
| Packages | tar.gz installed with `install.sh` into a test home runs a network on the bundled core; `.deb` depends on GTK 3 and the tray library only |
| Smoke script | `scripts/smoke.sh user` and `helper` pass on this machine |
| Helper self-update | Signed bundle accepted without a prompt (binary replaced, helper restarted); unsigned and tampered bundles refused; the bundle signed by `release.yml` with the GitHub secret is accepted too |
| GUI helper buttons | Remove and install through the real polkit dialog (pkexec), operator recorded |
| EasyTier 2.7.0 and mini (WSL Debian 13 aarch64, 2026-10-28) | Live adapter test passes on a v2.7.0 core and on mini (queried by the v2.7.0 CLI); the user daemon downloads mini v2.7.0 with its CLI from GitHub (SHA-256 verified), makes "Default Network (mini)" without a QUIC listener and MagicDNS (the original unchanged), runs it beside the full network on v2.7.0 — both connected and peered over TCP — and refuses a second mini network |

## macOS and Windows

- **macOS:** the helper is the LaunchDaemon `com.yuanzhe.easytiermygo.helper`
  (`/Library/LaunchDaemons`, binaries in `/Library/Application Support/EasyTier MyGo/bin`), always
  loaded (`RunAtLoad`/`KeepAlive`): launchd socket activation needs a C API, so Follow GUI is
  enforced by the daemon (networks run only while a session is attached unless background mode is
  on). Socket `/var/run/easytier-mygo/ctl.sock`; callers identified by `LOCAL_PEERCRED`. The
  operator is passed with `--operator` because `osascript` carries no `SUDO_UID`.
- **Windows:** the helper is the automatic service `EasyTierMyGoHelper` (binaries in
  `%ProgramFiles%\EasyTier MyGo\helper`, state in `%ProgramData%\EasyTier MyGo\state`, restricted
  with `icacls`), restarted on failure. The local API is an AF_UNIX socket; callers are identified
  by `SIO_AF_UNIX_GETPEERPID` and their token (user SID, Administrators membership). The user daemon
  watches the GUI's process (`--parent-pid`) because a pipe cannot be handed to a child; cores run
  in a kill-on-close job object. A self-update renames the running executable aside and exits so
  the service manager restarts it.

Until verified on real machines, these are designs that compile and pass vet, not results.
Verified so far on Windows 11 ARM64 (Go 1.27.2, no C compiler): the local gate passes, and
`scripts/smoke.sh user` runs a `no_tun` network on the bundled v2.6.4 core through the user daemon
and shuts it down through the API. The helper, installed and removed through UAC, runs the same
network for a non-administrator operator while a session is attached (`ctl watch`); uninstalling
removes the service, the binaries, the state and the `EasyTier MyGo` folders under `%ProgramFiles%`
and `%ProgramData%`.

Verified on macOS 27.0.1, Apple M1 (2026-10-16, Go 1.27.2 from Homebrew, EasyTier v2.6.4):

| Check | Result |
|---|---|
| Local gate | Passes, including `CGO_ENABLED=0` |
| Smoke script | `scripts/smoke.sh user` passes |
| User daemon, two `no_tun` networks | Peer over the interface address and connect; live adapter test passes on both cores |
| Crash recovery | `kill -9` of a core → restarted by bounded recovery, back to healthy |
| Lifeline | Pipe EOF stops the cores and the daemon; the socket is removed |
| Packages | `scripts/build.sh` builds the `.app` and the `.dmg` |
| GUI daemon child | In no-privileges mode the `.app` starts the daemon from its resources (socket 0600 under the temporary directory); `kill -9` of the GUI stops the daemon and its core |
| Helper install (`sudo … --operator`) | LaunchDaemon loaded, binaries root-owned, the operator gets write access as a non-root user |
| Helper, Follow GUI | A connected network stays stopped without a session, runs as root with one; ending the sessions stops every core |
| Helper with TUN | Two TUN networks get `utun` devices with their addresses and peer; the devices go away when they stop |
| Helper uninstall `--purge` | Removes the LaunchDaemon, the binaries, the state, the `EasyTier MyGo` folder and the socket directory |
| Daemon tests | `internal/daemon` tests run on macOS too (they used to be Linux-only) |
| Helper reinstall, 0.1.5 → 0.1.6 (2026-10-17) | A 0.1.5 helper left installed but not loaded by the 0.1.5 reinstall: after updating the app to 0.1.6 and one administrator approval, the 0.1.6 install reloads it (launchd `state = running`, version 0.1.6, `ctl.sock` back) and the networks reconnect |
| Helper self-update by code signature (2026-10-17) | Unit test with ad hoc signed copies: a binary satisfying the installed copy's designated requirement is installed; another signature or an older version is refused, the installed files stay and nothing staged is left |
| Code signature (2026-10-17) | `MYGO_SIGN` build: the app, its executables and the `.dmg` carry `TeamIdentifier=982C6JDNCS` with the hardened runtime and verify strictly; the signed daemon runs the signed core (`no_tun` network up); a rebuild keeps the same designated requirement |

Not verified on macOS: the `osascript` prompt from the GUI's install button, helper self-update,
starting in the tray at login (fixed in 0.2.3), automatic app updates, EasyTier mini through
the app, and the GUI's appearance (no screenshot was taken).

## Known limitations

- The EasyTier core's own RPC portal (TCP on loopback, a port from 25888 up) is reachable by every
  local user, so a local user can query or reconfigure a running core through `easytier-cli`. The
  app's local API is protected; the core's portal is not. Single-user desktops are the target.
- The privileged helper runs cores as root/SYSTEM; its API is limited to the operator and the
  `easytier-mygo` group, and path-valued config options are refused.
- Windows has passed `no_tun` networks only; WinTun is untested. macOS TUN (utun) is verified
  through the helper.
- EasyTier mini runs one network at a time (its management port is fixed at 15888, which another
  program may hold) and has no `--check-config`: a mini copy whose document does not load fails at
  start and returns to its previous revision. On Windows, mini ran on its own on ARM64 (the x64
  build, emulated) but not yet through the app.
- Cores marked untested may change their CLI output or config format; the app degrades to
  "status unreadable" without restarting anything, and the previous core can be selected again.
- Unauthenticated GitHub API requests are limited to 60 per hour; release lists are cached with
  their ETag and fetched only on request or by the daily check.
- The `.deb` does not depend on WebKitGTK (`linux.webview: false` in `mygo.json`).
- The macOS app is signed but not notarized: Gatekeeper blocks a downloaded copy. Remove its
  quarantine once after copying it to Applications (`xattr -dr com.apple.quarantine
  "/Applications/EasyTier MyGo.app"`), or allow it in System Settings → Privacy & Security.
  Updating from a build signed before the Apple Development certificate (ad hoc) works, but macOS
  asks for its privacy permissions again once; later updates keep them.
- On macOS a helper of 0.1.5 or earlier cannot update itself by code signature (0.1.5 checks the
  bundle manifest; earlier builds are ad hoc signed), so its update asks for administrator
  approval once ([packaging.md](packaging.md)).

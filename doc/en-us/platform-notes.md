# Platform notes

| Platform | Status |
|---|---|
| Linux (systemd) | Primary platform. New daemon, run modes and helper implemented and verified (below); the GUI is being rebuilt. |
| macOS | Implemented, not verified on a real machine: user daemon, LaunchDaemon helper, administrator prompt through `osascript`, signed self-update. |
| Windows (x64, ARM64) | Implemented, not verified on a real machine: user daemon, service helper, UAC through PowerShell, SID authorization over AF_UNIX, job object for cores, signed self-update. |

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
| Helper self-update | Signed bundle accepted without a prompt (binary replaced, helper restarted); unsigned and tampered bundles refused |

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

## Machine caution

The development machine runs a production EasyTier core as root (RPC 15888, listeners 11010,
device `easytier0`). Never touch it. Development cores use explicit RPC ports from 25888 and
listener ports from 21110.

# Platform notes

| Platform | Status |
|---|---|
| Linux (systemd) | Primary platform. New daemon, run modes and helper implemented and verified (below); the GUI is being rebuilt. |
| macOS | Builds. Helper (launchd) and run modes not implemented; nothing verified on a real machine. |
| Windows | Builds. Helper (service) and run modes not implemented; nothing verified on a real machine. |

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
| Helper self-update | Signed bundle accepted without a prompt (binary replaced, helper restarted); unsigned and tampered bundles refused |

## macOS and Windows

Planned helper backends: a launchd daemon (on-demand through `Sockets`, `RunAtLoad`/`KeepAlive`
for background) and a Windows service (demand start with an operator DACL, automatic start for
background). Until they are verified on real machines, every statement about them is a design, not
a result.

## Machine caution

The development machine runs a production EasyTier core as root (RPC 15888, listeners 11010,
device `easytier0`). Never touch it. Development cores use explicit RPC ports from 25888 and
listener ports from 21110.

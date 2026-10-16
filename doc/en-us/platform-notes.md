# Platform notes

| Platform | Status |
|---|---|
| Linux (systemd) | Primary platform. Inherited daemon and GUI run; the run modes, helper and new GUI are being rebuilt. |
| macOS | Builds. Helper (launchd) and run modes not implemented; nothing verified on a real machine. |
| Windows | Builds. Helper (service) and run modes not implemented; nothing verified on a real machine. |

## Linux

- Development machine: Debian 13, XFCE on X11, StatusNotifier tray host present, pkexec available.
- The tray uses `libayatana-appindicator3` (falling back to `libappindicator3`); without it the
  app runs without a tray.
- MyGo's login item writes an XDG autostart entry.

## macOS and Windows

Planned helper backends: a launchd daemon (on-demand through `Sockets`, `RunAtLoad`/`KeepAlive`
for background) and a Windows service (demand start with an operator DACL, automatic start for
background). Until they are verified on real machines, every statement about them is a design, not
a result.

## Machine caution

The development machine runs a production EasyTier core as root (RPC 15888, listeners 11010,
device `easytier0`). Never touch it. Development cores use explicit RPC ports from 25888 and
listener ports from 21110.

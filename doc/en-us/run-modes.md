# Run modes

Creating a TUN device needs administrator rights on every platform. EasyTier MyGo follows the
model Tailscale uses: administrator authorization happens **once**, when a privileged helper is
installed, and never again when the GUI starts. The GUI itself never runs elevated.

## The three modes

| Mode | Requirement | When networks run | Linux | macOS | Windows |
|---|---|---|---|---|---|
| No privileges | none | While the GUI runs; `no_tun` networks only | The GUI starts a user-level daemon as its child | same | same |
| Follow GUI | privileged helper installed once | While a GUI session is attached. When the GUI quits or crashes, networks stop and the idle helper exits. Nothing runs at boot. | systemd socket-activated system unit (not enabled at boot) | launchd daemon started on demand through `Sockets` | demand-start service whose DACL lets the operator start it |
| Background service | same helper; switching from Follow GUI needs no new authorization | From boot, independent of the GUI | the same unit enabled for `multi-user.target` | `RunAtLoad` + `KeepAlive` | automatic start |

## Authorization

- Installing or uninstalling the helper asks once: pkexec (Linux), the administrator password
  (macOS), UAC (Windows). The installing user becomes the **operator**.
- The helper's socket accepts writes from the operator and from members of the `easytier-mygo`
  group (supplementary groups included); other users are refused.
- Prompts appear only for: installing or uninstalling the helper, installing a custom core binary,
  and updating the helper from an unsigned build. Never on GUI start.

## Session binding

The GUI keeps one long-lived session connection to the helper. When it closes and the mode is
Follow GUI, the helper stops the networks; with no session and no running network it exits after
an idle period, and systemd/launchd starts it again on the next connection.

## Safety rules for the helper

The helper runs cores as root/SYSTEM, so:

- path-valued config options (log directories) are forced under the helper's state directory;
- official cores are downloaded and SHA-256-verified by the helper itself, never copied from a
  user-writable file;
- custom core binaries require administrator authorization each time;
- the helper binary lives in a root-owned location; it replaces itself only with a build carrying
  a valid release signature.

## Isolation from other EasyTier installations

Unit, socket and state names use `easytier-mygo`; TUN device names use the `etm` prefix; the daemon
always passes an explicit `--rpc-portal`. The EasyTier defaults (RPC 15888, listeners 11010,
device `easytier0`) are never assumed.

## Current implementation

Linux: the user daemon (`easytier-mygo-svc run --host user --lifeline-fd N`), the helper
(`run --host helper`, socket-activated) and `easytier-mygo-svc helper install|uninstall|status`
are implemented; the GUI flow comes with the new interface. `helper install` must run as root
(the GUI uses pkexec) and records the user from `PKEXEC_UID`/`SUDO_UID` as operator. Helper
self-update by signature and the macOS/Windows backends are not implemented yet.
[platform-notes.md](platform-notes.md) records what was verified.

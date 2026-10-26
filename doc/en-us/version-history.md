# Version history

The project restarted from 0.1.0 on 2026-10-16. The earlier, inherited history (versions up to
0.10.80) is preserved under the git tag `archive/v0.10.80` and is not continued.

## 0.2.2 (2026-10-25)

- First-run tour: the computers are drawn as Kyokai's tour draws them — one fixed view from the
  front left above, without perspective, larger, the system's logo lying square on the top (the
  Apple logo a little larger), and a softer shadow. The Windows Dev Kit and the Mac mini lose
  their ports; the Linux mini PC keeps its grooves, now on the faces in view. Labels under the
  computer and the virtual-device tag moved to make room.
- MyGo v0.6.4 / EasyTier v2.6.4 (checked 2026-10-25, unchanged).

## 0.2.1 (2026-10-25)

- First run: a tour of six steps replaces the welcome page — the virtual device, the three run
  modes side by side, startup and sign-in, staying online, then the choice. Each step has an
  animated illustration: this computer in perspective as a Windows Dev Kit, a Mac mini or a mini
  PC, with its system's logo on top. Skip starts in No privileges; Settings › Run mode › Watch
  tour shows it again. Open at login and the language can be set in the tour.
- MyGo v0.6.4 (updated from v0.6.3; Go 1.27.3) / EasyTier v2.6.4 (checked 2026-10-25, unchanged).

## 0.2.0 (2026-10-24)

- A new interface after Kyokai's: the sidebar sits on the window's material (Mica on Windows 11,
  the sidebar material on macOS) and the content is one floating pane whose header is the
  window's title bar. The 0.1.7 title strip is gone; Windows' window buttons are drawn in the
  header, macOS' traffic lights sit over the sidebar, Linux keeps the desktop's buttons.
- Sidebar: networks with a tile and a status dot, Activity, a light/dark toggle, Settings and the
  run mode at the foot; it folds to a rail of icons.
- Network pane: Edit and Connect/Disconnect in the header; a disconnected network shows its
  configuration (never its secret); without current data only "No current data" shows. Problems
  with the background service are cards under the header.
- Editor and Add network: fields as rows with one sentence each, Form | TOML and Save / Save and
  apply in the header, Delete network last. Logs became Activity, grouped by day.
- Settings is a sheet over the window with search, regrouped: Preferences › General; This
  computer › Run mode, EasyTier core; EasyTier MyGo › About & updates (Advanced moved into its
  Diagnostics).
- First run: the run modes are tiles to choose from, with one Continue button.
- MyGo v0.6.3 (updated from v0.6.2) / EasyTier v2.6.4 (checked 2026-10-24, unchanged).

## 0.1.7 (2026-10-22)

- The window's title bar is part of the interface: a slim strip across the top with the app chip
  replaces the system title bar, and the system's window buttons sit in it (traffic lights on
  macOS; caption buttons with Snap Layouts on Windows). The strip drags the window, a double-click
  maximizes it, and it hides in full screen.
- Tray: the icon's colors follow a dark or light menu bar or taskbar.
- Linux: the `.deb` and `install.sh` no longer need WebKitGTK (MyGo's `linux.webview`).
- MyGo v0.6.2 (updated from v0.5.5) / EasyTier v2.6.4 (checked, unchanged).

## 0.1.6 (2026-10-17)

- macOS: reinstalling the privileged helper (the "Update helper" banner) could leave it not
  running: `launchctl bootout` returns before the old job is gone, and the following `bootstrap`
  failed. The install now waits for the old job and retries; a helper that is installed but not
  running shows a banner with "Start helper".
- macOS: the helper updates itself by code signature (the new executables must satisfy the
  installed copies' designated requirement), so updates after this one need no administrator
  approval. Updating a 0.1.5 or earlier helper asks once more.
- Network page: without current data from the background service the status reads Unknown and
  the traffic, tiles and peers are hidden, instead of the last reported "Connected".
- MyGo v0.5.5 / EasyTier v2.6.4 (checked, unchanged).

## 0.1.5 (2026-10-17)

- macOS: the app is signed with the maintainer's Apple Development certificate (Team ID
  `982C6JDNCS`) instead of ad hoc, so macOS keeps its privacy grants across updates. The first
  update from an earlier version asks for them once more. The app is still not notarized: remove
  the quarantine of a downloaded copy once ([platform-notes.md](platform-notes.md)).
- macOS: updating the privileged helper asks for administrator approval, because the code
  signature changes the files its bundle manifest covers.
- MyGo v0.5.5 / EasyTier v2.6.4 (checked, unchanged).

## 0.1.4 (2026-10-16)

- Peer list: machine-style hostnames are shown as friendly names (`macbook-pro` → MacBook Pro,
  `Intel-NUC8i5BEH` → Intel NUC8i5BEH); the raw hostname is the row's tooltip.
- Peer list: the avatar shows the vendor's logo when the hostname names a known maker, product
  line, operating system or provider (Apple, Intel, Linksys, Windows, …); 63 bundled logos, sources
  in `internal/gui/assets/logos/SOURCES.md`.
- Peer list: right-click a peer to copy its IP address, MagicDNS name (when `accept_dns` is on) or
  hostname; a copy button beside the address appears on hover.
- Security: the privileged helper's signed self-update checked the bundle in the user-writable
  app folder and then read every file there again to copy it, so a program running as the same
  user could swap in its own binary between the two reads and have it installed and run as
  root/SYSTEM. The helper now reads each file once and installs exactly the bytes it verified
  (Windows, macOS, Linux).
- MyGo v0.5.5 / EasyTier v2.6.4 (checked, unchanged).

## 0.1.3 (2026-10-16)

- macOS: closing the window into the tray also hides the Dock icon; a switch in Settings → General
  turns this off. Minimizing keeps the Dock icon.
- The network form shows the instance ID: an empty or missing one is replaced by a fresh UUID,
  and a button generates a new one. New networks get one when created.
- MyGo v0.5.5 / EasyTier v2.6.4 (checked, unchanged).

## 0.1.2 (2026-10-16)

- The app updates itself from GitHub: a daily check (can be turned off) and "Check for updates" in
  Settings → About; updates are signed (`MYGO_UPDATER_PRIVATE_KEY`) and installed in place on
  request. Packages from a package manager keep updating through it.
- Windows: the privileged helper refused every network with a virtual device ("needs the
  privileged helper") because it took itself for unprivileged; it now checks its token. Adding a
  custom core to the helper had the same fault.
- Error messages at the bottom of the window were invisible (text color left unset); they are
  white on red now.
- An `instance_id` that is not a UUID is named before the core refuses it without a reason.
- New bundle signing key (`internal/bundlesig/release.pub`): helpers of 0.1.0 and 0.1.1 know only
  the old one, so updating them to 0.1.2 asks for administrator approval once.
- MyGo v0.5.5 / EasyTier v2.6.4 (checked, unchanged).

## 0.1.1 (2026-10-16)

- Windows: installing the privileged helper from the GUI failed when the app was installed under
  a path with a space (the default `…\Programs\EasyTier MyGo`): the administrator command split
  the bundle path. Arguments are now quoted for the Windows command line.
- The welcome page shows why a run-mode choice failed instead of staying silent.
- MyGo v0.5.5 / EasyTier v2.6.4 (checked, unchanged).

## 0.1.0 — First release of the rework (2026-10-16)

- Reset baseline: inherited tests, measurement scripts, acceptance documents and the ACL editor
  engine removed; documentation rewritten.
- Identifier `com.yuanzhe.easytiermygo`, module `github.com/YuanZhe-99/Easytier-MyGo`.
- MyGo v0.5.5 / EasyTier v2.6.4 (checked 2026-10-16).
- EasyTier release binaries are unpacked from UPX when fetched (about 250 ms → 3 ms of CPU per CLI
  call).
- New runtime: network store, per-network supervisor with apply rollback, local API v2 with
  sessions, run modes (no privileges / Follow GUI / background) and the Linux privileged helper.
- Health grace period after a deliberate start lowered from 45 s to 15 s.
- New GUI: dashboard with a live traffic chart, nodes with connection details, form and TOML
  editing, run modes, core manager, tray, four languages, light and dark themes.
- Packages: Linux tar.gz and `.deb`, Windows installer and portable zip (x64, ARM64), macOS disk
  image; signed bundles let the privileged helper update itself without a prompt.
- macOS and Windows backends: implemented, CI smoke tests with `no_tun` networks; verified on real
  machines (macOS 27 on Apple M1, including the helper with TUN; Windows 11 ARM64, user daemon and
  helper with `no_tun`).
- Not yet included: structured ACL editor, TCP/DNS probes, persistent traffic history, monitoring
  an external core.
- Core manager: choose the bundled core, download official releases (pre-releases on request) or
  add a custom build; upx 5.2.1 bundled; the adapter reads both v2.6 and v2.7 output.

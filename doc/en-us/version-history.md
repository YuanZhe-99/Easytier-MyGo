# Version history

The project restarted from 0.1.0 on 2026-10-16. The earlier, inherited history (versions up to
0.10.80) is preserved under the git tag `archive/v0.10.80` and is not continued.

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

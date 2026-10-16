# Version history

The project restarted from 0.1.0 on 2026-10-16. The earlier, inherited history (versions up to
0.10.80) is preserved under the git tag `archive/v0.10.80` and is not continued.

## 0.1.0 — Rework (unreleased)

- Reset baseline: inherited tests, measurement scripts, acceptance documents and the ACL editor
  engine removed; documentation rewritten.
- Identifier `com.yuanzhe.easytiermygo`, module `github.com/YuanZhe-99/Easytier-MyGo`.
- MyGo v0.5.5 / EasyTier v2.6.4 (checked 2026-10-16).
- EasyTier release binaries are unpacked from UPX when fetched (about 250 ms → 3 ms of CPU per CLI
  call).
- New runtime: network store, per-network supervisor with apply rollback, local API v2 with
  sessions, run modes (no privileges / Follow GUI / background) and the Linux privileged helper.
- Health grace period after a deliberate start lowered from 45 s to 15 s.
- Core manager: choose the bundled core, download official releases (pre-releases on request) or
  add a custom build; upx 5.2.1 bundled; the adapter reads both v2.6 and v2.7 output.

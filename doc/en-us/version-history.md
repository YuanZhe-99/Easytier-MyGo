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

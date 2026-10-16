# EasyTier MyGo documentation

**These docs are the authoritative description of the code.** `doc/en-us/` is the source;
`doc/zh-cn/` mirrors it file for file. Plans and open work are not documented here; while a
multi-step plan is active it lives only in the repository's `PLAN.md`.

EasyTier MyGo is a native desktop client for [EasyTier](https://github.com/EasyTier/EasyTier),
written in Go with [MyGo](https://github.com/egoist/mygo). Linux is the primary platform; the
Windows and macOS backends are experimental and unverified on real machines.

| Document | Covers |
|---|---|
| [architecture.md](architecture.md) | Processes, packages, the local API, cross-cutting rules |
| [run-modes.md](run-modes.md) | The three run modes, the privileged helper, authorization |
| [health-monitoring.md](health-monitoring.md) | Health vocabulary, policy, bounded recovery |
| [configuration.md](configuration.md) | Raw config documents, form projection, save vs. apply |
| [gui.md](gui.md) | Interface structure, wording rules, charts, tray, languages |
| [easytier-upstream.md](easytier-upstream.md) | The EasyTier CLI contract, release assets and verified quirks |
| [platform-notes.md](platform-notes.md) | What is implemented and verified on each platform |
| [development.md](development.md) | Environment, commands, the dev loop |
| [ci-cd.md](ci-cd.md) | Workflows, the upstream update procedure, releases |
| [packaging.md](packaging.md) | Release artifacts, bundled binaries, licenses |
| [translation-guide.md](translation-guide.md) | Mirror rules and the glossary |
| [version-history.md](version-history.md) | Releases and the upstream versions they pin |
| [functions/INDEX.md](functions/INDEX.md) | Per-package roles and key declarations |

## Pinned versions

| Component | Version | Source of truth |
|---|---|---|
| Go | 1.27.2 | `go.mod` |
| MyGo | v0.5.5 | `go.mod` |
| EasyTier (bundled) | v2.6.4 | `third_party/easytier.json` |

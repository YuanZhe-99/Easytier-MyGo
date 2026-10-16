# AGENTS.md

Operating guide for agents working on **EasyTier MyGo**. This file holds **only** rules about how to
work here. Everything describing what the code *is* or *does* lives in `doc/en-us/` — see
[Where to read what](#where-to-read-what).

EasyTier MyGo is a native desktop client for EasyTier (Go + MyGo; Linux first, Windows and macOS
backends experimental) with a daemon that runs the EasyTier core, monitors its health and recovers
local faults. Treat the user's message as the change request: plan, implement, verify, report.

## Reading order

When you need to understand code, read in this order and stop as soon as you have what you need:

1. **`doc/en-us/`** — start here, always. `architecture.md` for shape and rules;
   `functions/INDEX.md` to find the right package; the concept docs for behavior.
2. **Comments in the source** — the Function Explanation Layer on each declaration.
3. **The implementation** — only when the docs and comments are insufficient, or when you must
   confirm actual behavior before changing it.

Do not jump straight to reading source bodies. Where docs and code disagree on something you are
about to change, verify against the code, then fix the docs in the same commit.

## Where to read what

| Question | Read |
|---|---|
| Current plan and open work (only while a multi-step plan is active) | `PLAN.md` |
| Processes, packages, local API, cross-cutting rules | `doc/en-us/architecture.md` |
| Run modes, privileged helper, authorization | `doc/en-us/run-modes.md` |
| Health vocabulary, policy, recovery semantics | `doc/en-us/health-monitoring.md` |
| Raw config documents, form projection, save vs. apply | `doc/en-us/configuration.md` |
| Interface structure, wording, charts, tray, languages | `doc/en-us/gui.md` |
| EasyTier CLI contract, release assets, verified quirks | `doc/en-us/easytier-upstream.md` |
| What is implemented and verified per platform | `doc/en-us/platform-notes.md` |
| Environment, commands, dev loop | `doc/en-us/development.md` |
| Workflows, upstream update procedure, releases | `doc/en-us/ci-cd.md`, `doc/en-us/packaging.md` |
| English→Chinese terminology | `doc/en-us/translation-guide.md` |
| Past releases and pinned upstream versions | `doc/en-us/version-history.md` |

## Required workflow

1. Treat the user's message as the modification request.
2. Before editing, fetch the relevant remote(s) and check whether the local branch is behind.
   Resolve any divergence before starting.
3. Run `go run ./tools/deps check`. If MyGo or EasyTier has a newer release, follow
   [Upstream versions](#upstream-versions) first.
4. Read per [Reading order](#reading-order). Plan when the work is non-trivial; multi-step plans go
   into `PLAN.md`.
5. Keep changes scoped. Do not revert unrelated work in the tree.
6. Update documentation in the same change set — see
   [Documentation maintenance](#documentation-maintenance).
7. Verify — see [Verification](#verification).
8. Report briefly, in English and Chinese: what changed, what was verified (with evidence), the
   current version, the configured remotes, and anything that could not be done.
9. Ask before pushing. The user decides when to release.

## Documentation maintenance

**Docs are the primary artifact. Update them first, and never let them drift.**

Any change to a behavior, a data format, the local API, a run mode or a user-visible feature
updates, in the same commit, the affected concept docs and the `functions/INDEX.md` row.

Every language directory under `doc/` (currently `en-us` and `zh-cn`) mirrors the others exactly —
same files, headings, tables, and code blocks. `doc/en-us/` is authoritative: any documentation
change updates **all** language directories in the same commit, translated per
`translation-guide.md`; new terms go into its glossary. `go run ./tools/checkdocs` must pass.

**Put explanation in the docs, not here.** This file is for agent instructions only.

**Plans live only in `PLAN.md`** — never in `doc/`. Delete items from `PLAN.md` as soon as they are
done; long-lived contracts move into the docs. When the plan is complete, delete `PLAN.md`.

Add a `doc/en-us/version-history.md` entry for each release. Documentation-only commits do not
bump versions or create tags.

## Authoring rules

**Function Explanation Layer.** Every function, method and significant type carries a Go doc
comment whose first sentence states its purpose (godoc style: "Observe records …"). Non-trivial
declarations add short lines as needed:

- `Inputs:` important parameters only
- `Returns:` what the caller receives
- `Side effects:` state, file, network, process or UI effects
- `Notes:` assumptions, edge cases, invariants

Keep comments accurate in the same change as the code: they are the second layer of the reading
order, and a stale comment leads to a wrong change.

Other conventions:

- MyGo UI code follows `.agents/skills/mygo-maintenance`: views are functions of state, elements
  are never stored, background results reach the view through `Window.Update`, controls get stable
  keys where state applies.
- Interface copy is written for the person using the app. Never surface reason codes, hashes,
  policy numbers, socket paths or profile IDs in the main interface.
- Interface strings go through the ARB catalogs (en, zh-Hans, zh-Hant, ja);
  `go tool mygo i18n check` must pass.
- Layout numbers and colors are named tokens, not literals scattered through views.

## Behavior contract

Do not change these without the user explicitly deciding to:

- **Health semantics.** Only `local-fault` and `unresponsive` may trigger recovery. External
  causes, collector failures and normal transients degrade the verdict but never restart anything.
  A manual stop is never overridden. Recovery is bounded; `internal/health.Policy` holds the
  numbers.
- **Never show stale data as current.** A green state must not outlive its evidence.
- **Secrets never travel.** `NodeInfo.Config` (raw TOML with `network_secret`) is never logged,
  cached into API snapshots, sent in diagnostics or persisted. Event details are scrubbed before
  they are stored.
- **Configs are raw documents.** Edits rewrite only changed spans; saves are conflict-checked and
  backed up; applying is a separate explicit action; invalid drafts may be saved but never
  activated.
- **The GUI never runs elevated.** Administrator prompts appear only when installing or
  uninstalling the privileged helper, installing a custom core binary, or updating the helper from
  an unsigned build — never when the GUI starts.
- **The privileged helper** authorizes the operator and the `easytier-mygo` group only, forces
  path-valued config options under its own state directory, and verifies every official core it
  installs. In Follow GUI mode it stops networks when the GUI session ends.
- **Cores.** Never download automatically. `easytier-core` and `easytier-cli` always come from the
  same release. Untested versions are labeled; a failed `--check-config` blocks activation.
- **`internal/localapi` is versioned.** Enums cross the socket as names; changing a field's meaning
  bumps `ProtocolVersion`.
- **One instance per core.** Multi-instance output is reported (`ErrMultiInstance`), never
  misparsed.

## Upstream versions

Every change set keeps MyGo and EasyTier current (latest MyGo tag; latest **stable** EasyTier
release). When `go run ./tools/deps check` reports an update, move everything together in one
commit, following `doc/en-us/ci-cd.md`: `tools/deps update`, `go tool mygo migrate-ui` (review by
hand), `tools/deps fetch`, re-captured adapter fixtures, the live adapter test, the full local gate,
and a `version-history.md` line naming both versions. Record "checked, unchanged" too. Add an
EasyTier tag to `tested` only after its live checks pass.

## Verification

```bash
gofmt -l cmd internal tools third_party
go vet ./...
go test ./...
CGO_ENABLED=0 go test ./...
go tool mygo vet .
go tool mygo i18n check
go run ./tools/checkdocs
```

- Run the gated live adapter test (`EASYTIER_CLI`, `EASYTIER_RPC`) and a real run when a change
  touches the adapter, the daemon or a run mode. Inspect the native app (screenshots) when a change
  touches layout.
- Tests protect product contracts and error-prone logic, preferably table-driven; every fixed bug
  gets a regression test. Do not write tests for wording, internal state details or to collect
  evidence.
- **Never claim "verified" without an actual run;** record what was verified where.
- **This machine runs a production EasyTier core as root** (RPC 15888, listeners 11010, device
  `easytier0`). Never touch it and never rely on defaults: pass an explicit `--rpc` (25888, 25889,
  …), write explicit `listeners` (21110, 21111, …) and use the `etm` TUN prefix in every dev config.
- Local core-to-core connections must dial an interface address, not loopback.
- Installing the privileged helper on this machine needs the user's password: ask first, and
  uninstall it after testing unless told otherwise.

## Release, version, commit, tag, push

The version lives in `internal/version` (`Version`); `mygo.json` must match (a test enforces it).

For ordinary work, do not bump versions or tag until the user confirms the release version and
confirms pushing. When the user confirms:

1. Set the version in `internal/version/version.go` and `mygo.json`.
2. Bring MyGo and EasyTier to their latest versions and re-run verification.
3. Commit, including the `doc/en-us/version-history.md` entry (and its mirror).
4. Create an annotated tag `vX.Y.Z`.
5. Push **the commit first**, then the tag, to `origin` and (once it exists) `github`.

GitHub Actions release builds trigger on tag pushes to `github`. Signing keys live only in GitHub
secrets.

## Agent co-author attribution

An agent that made a real, material contribution to a commit may add its own accurate
`Co-authored-by:` trailer. Attribution is per commit: do not add an agent merely because it
reviewed, observed, or continued another agent's work, and never copy a trailer automatically from
an earlier commit. When multiple agents materially contributed, include one accurate trailer each.
Use the agent's actual documented identity; never invent a provider, model, name, or email.
Approved examples are `Co-authored-by: Codex <noreply@openai.com>` and, for Claude Code,
`Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>` with the model name replaced by the actual
model that did the work.

## Remotes and secrets

- `origin` → `<local_gitea_address>` (private Gitea)
- `github` → `git@github.com:YuanZhe-99/Easytier-MyGo.git` (to be added)

Determine the repository path from the runtime workspace; do not hardcode a machine-specific
absolute path here.

**Masking rule:** keep the `origin` URL written as `<local_gitea_address>` in every committed file.
Never write the underlying Tailscale host or port anywhere in the repo.

**Never commit:** secrets, signing keys, credentials, network secrets, `.dev/`, `build/`, fetched
binaries under `resources/<goos>-<goarch>/`, or machine-specific state.

## MyApps milestone releases

For each completed milestone, increment the patch version by 0.0.1; the user decides when to
release. Documentation-only changes do not bump the version.

License: GPL-3.0 (`LICENSE`). Do not copy code from repositories with incompatible licenses.
Bundled EasyTier binaries are LGPL-3.0 and ship with their license text and source location.

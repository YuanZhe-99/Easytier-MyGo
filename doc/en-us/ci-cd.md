# CI/CD

Workflows live in `.github/workflows` and run on GitHub. The private Gitea remote has no runner,
so a push there is checked only by the local gate.

| Workflow | Trigger | Does |
|---|---|---|
| `ci.yml` | push to `main`, pull requests, manual | Formatting, vet, tests (also with `CGO_ENABLED=0`), MyGo vet, interface catalogs, doc mirror check, upstream pins; build and unit tests on Windows (x64 and ARM64) and macOS. |
| `ci.yml` → smoke | after the checks | `scripts/smoke.sh user` and `helper` on Linux, macOS and Windows (x64, ARM64) runners: a `no_tun` network through the user daemon and through an installed helper, then uninstall. |
| `upstream.yml` | weekly, manual | `go run ./tools/deps check -strict`; opens an issue when MyGo or EasyTier has a newer release. |
| `dependabot.yml` | weekly | Action and Go module updates. |

`release.yml` (tag `v*`, or manual) runs `scripts/build.sh` per platform: Linux and Windows on
Ubuntu (NSIS for the installers), macOS on a macOS runner. A tag publishes the downloads with
`SHA256SUMS.txt` after checking that it matches `internal/version`. The secret
`EASYTIER_MYGO_SIGNING_KEY` (from `go run ./tools/sign keygen`) signs the bundles; keep the
private key only there and in the maintainer's offline copy. The secret `MYGO_UPDATER_PRIVATE_KEY`
(from `go tool mygo keygen`; its public key is `updates.publicKey` in `mygo.json`) signs the app
updates: per platform an archive, deltas from the last versions and `update-<target>.json`, all
attached to the release. Installed apps read the manifests of the latest release; without the
secret no updates are published and installed apps stay on their version.

## Upstream update procedure

Every change set starts with `go run ./tools/deps check`. When MyGo or EasyTier has a newer
version, move everything together in one commit:

1. `go run ./tools/deps update` (MyGo to latest; EasyTier manifest to the latest stable release).
2. `go tool mygo migrate-ui .`, then `-write`, and review identity/bound-value changes by hand.
3. `go run ./tools/deps fetch`, re-capture `internal/coreadapter/testdata`, run the live adapter
   test and the full local gate.
4. Add the new EasyTier tag to `tested` in `third_party/easytier.json` only after the live checks
   pass; update the version table in [README.md](README.md).
5. Record "MyGo vX / EasyTier vY (checked YYYY-MM-DD)" in [version-history.md](version-history.md),
   also when nothing changed.

The bundled core follows stable releases only (GitHub's "latest"). Pre-releases are tried through
the in-app core manager and are never shipped.

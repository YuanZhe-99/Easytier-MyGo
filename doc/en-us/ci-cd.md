# CI/CD

Workflows live in `.github/workflows` and run on GitHub. The private Gitea remote has no runner,
so a push there is checked only by the local gate.

| Workflow | Trigger | Does |
|---|---|---|
| `ci.yml` | push to `main`, pull requests, manual | Formatting, vet, tests (also with `CGO_ENABLED=0`), MyGo vet, doc mirror check, upstream pins; build and unit tests on Windows and macOS. |
| `upstream.yml` | weekly, manual | `go run ./tools/deps check -strict`; opens an issue when MyGo or EasyTier has a newer release. |
| `dependabot.yml` | weekly | Action and Go module updates. |

The release workflow (tag `v*`) is added with packaging; see [packaging.md](packaging.md).

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

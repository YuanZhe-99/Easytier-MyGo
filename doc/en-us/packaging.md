# Packaging

`go tool mygo build` produces the platform artifacts: on Linux a `.tar.gz` (with `install.sh`, a
per-user install) and a `.deb`; on Windows an `.exe` and an NSIS installer; on macOS an `.app` and
a `.dmg` (macOS host required).

## Bundled binaries

Files under `resources/<goos>-<goarch>/` ship with the app and are found at run time through
`mygo.App.Path(mygo.PathResources)`. `go run ./tools/deps fetch -platform <goos>-<goarch>`
downloads the pinned EasyTier archive, verifies its SHA-256 and unpacks the core and CLI (UPX
removed) into `resources/<goos>-<goarch>/bin`, together with the pinned `upx`
(`third_party/upx.json`; Linux and Windows only) that the core manager uses for downloaded cores.
These directories are build inputs and are not committed.

## Licenses

EasyTier MyGo is GPL-3.0. The bundled EasyTier binaries are LGPL-3.0: every distribution carries
their license text and the source location recorded in `third_party/easytier.json`. The temporary
icon comes from EasyTier-iOS (GPL-3.0-or-later); see `resources/NOTICE`. The bundled `upx` is
GPL-2.0-or-later with the UPX exception; its license ships as `upx-COPYING`.

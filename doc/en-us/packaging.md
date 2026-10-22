# Packaging

`scripts/build.sh [goos/goarch …]` fetches the core, builds the daemon and control CLI into the
platform resources, signs the bundle when `EASYTIER_MYGO_SIGNING_KEY` is set, runs
`go tool mygo build`. `mygo.json` sets `linux.webview` to `false`, so the `.deb` and `install.sh` need GTK and the tray library, not WebKitGTK.
`go tool mygo build` produces the platform artifacts: on Linux a `.tar.gz` (with `install.sh`, a
per-user install) and a `.deb`; on Windows an `.exe` and an NSIS installer; on macOS an `.app` and
a `.dmg` (macOS host required). With `MYGO_UPDATER_PRIVATE_KEY` set it also writes the signed
update files of each platform ([ci-cd.md](ci-cd.md)); their release notes are the version's
section of `doc/en-us/version-history.md`.

## macOS code signature

macOS apps are signed with the maintainer's Apple Development certificate (Team ID `982C6JDNCS`),
the one shared by all the maintainer's macOS apps. `scripts/build.sh` passes `MYGO_SIGN` to
`mygo build -sign`, which signs the app, every executable in its resources and the disk image with
the hardened runtime and a secure timestamp:

```bash
MYGO_SIGN=<certificate SHA-1> scripts/build.sh darwin/arm64
codesign -dvv "build/darwin-arm64/EasyTier MyGo.app"   # TeamIdentifier=982C6JDNCS
```

The identity is named by the certificate's SHA-1 (`security find-identity -p codesigning`); the
signing Mac also needs Apple's WWDR G3 intermediate certificate in its keychain. With a Team ID the
app's designated requirement (identifier, Apple anchor, certificate) is the same for every build,
so macOS keeps its privacy grants across rebuilds and updates; an ad hoc signature (no `MYGO_SIGN`)
is bound to one build's hash. The app needs no entitlements: its executables are Go and Rust
programs that load no libraries outside macOS, so library validation has nothing to refuse.
The app is not notarized, so a downloaded copy is blocked by Gatekeeper until its quarantine is
removed ([platform-notes.md](platform-notes.md)).

The macOS signature rewrites the bundled executables, so the helper's bundle manifest
(`EASYTIER_MYGO_SIGNING_KEY`), made from the files before `mygo build`, does not match them. On
macOS the helper therefore updates itself by code signature instead: each new executable must
satisfy the designated requirement of the installed copy (same identifier and certificate). A
helper installed from an ad hoc build designates its own hash, so updating it asks for
administrator approval once.

## Bundled binaries

Files under `resources/<goos>-<goarch>/` ship with the app and are found at run time through
`mygo.App.Path(mygo.PathResources)`. `go run ./tools/deps fetch -platform <goos>-<goarch>`
downloads the pinned EasyTier archive, verifies its SHA-256 and unpacks the core and CLI (UPX
removed) into `resources/<goos>-<goarch>/bin`, together with the pinned `upx`
(`third_party/upx.json`; Linux and Windows only) that the core manager uses for downloaded cores.
These directories are build inputs and are not committed.

## Licenses

EasyTier MyGo is GPL-3.0. The bundled EasyTier binaries are LGPL-3.0: every distribution carries
their license text and the source location recorded in `third_party/easytier.json`. The icon
comes from EasyTier-iOS (GPL-3.0-or-later); see `resources/NOTICE`. The bundled `upx` is
GPL-2.0-or-later with the UPX exception; its license ships as `upx-COPYING`. The interface icons
are Lucide (ISC). Vendor logos are not GPL-3.0 source; see below.

## Vendor logos

The peer list's vendor logos ([gui.md](gui.md), "Peer rows") are SVG files in
`internal/gui/assets/logos/`, embedded into the GUI at build time; the app never downloads them.
`SOURCES.md` in that directory lists each file's brand, source, license and trademark owner, and
`resources/NOTICE` summarizes it for distributions. Most files come from
[Simple Icons](https://simpleicons.org/) (CC0 1.0), the rest from Wikimedia Commons (public-domain
text or simple-geometry logos) or have no recorded source, which `SOURCES.md` says.

To add or update a logo:

1. Download `icons/<slug>.svg` from a pinned release of the `simple-icons` npm package, for example
   `https://cdn.jsdelivr.net/npm/simple-icons@<version>/icons/<slug>.svg`, or a public-domain file
   from Wikimedia Commons. Prefer Simple Icons: its files are CC0, monochrome, one path in a
   24 × 24 view box.
2. Save it as `internal/gui/assets/logos/<slug>.svg` (lower-case brand name, no separators). It must
   be monochrome shapes only: no text, embedded images or filters (`ui.ParseSVG`); the color comes
   from the row.
3. Add its row to `SOURCES.md` (file, brand, source link, license, trademark owner).
4. Add the brand's hostname words to `vendorKeywords` in `internal/gui/vendorlogo.go` and a case to
   `TestVendorLogoName`. `TestVendorLogosParse` fails for a file that does not parse or a keyword
   whose logo is missing.

A mark is removed if its owner asks.

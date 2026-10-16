# Packaging

`scripts/build.sh [goos/goarch …]` fetches the core, builds the daemon and control CLI into the
platform resources, signs the bundle when `EASYTIER_MYGO_SIGNING_KEY` is set, runs
`go tool mygo build`, and removes MyGo's WebKitGTK dependency from the `.deb`.
`go tool mygo build` produces the platform artifacts: on Linux a `.tar.gz` (with `install.sh`, a
per-user install) and a `.deb`; on Windows an `.exe` and an NSIS installer; on macOS an `.app` and
a `.dmg` (macOS host required). With `MYGO_UPDATER_PRIVATE_KEY` set it also writes the signed
update files of each platform ([ci-cd.md](ci-cd.md)); their release notes are the version's
section of `doc/en-us/version-history.md`.

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

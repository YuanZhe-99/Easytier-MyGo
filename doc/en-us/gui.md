# GUI

The interface is a desktop VPN client, not an engineering console (`internal/gui`). This document
states the rules it follows; `internal/gui/gui_test.go` renders every page and can write
screenshots (`SCREENSHOT_DIR`).

## Structure

- **Sidebar:** the network list (status dot, name, virtual IP; "+" to create or import), then
  Logs and Settings.
- **Network page:** a header card (name, EasyTier version, status badge, Connect/Disconnect);
  while running, the traffic row, the live traffic chart, info tiles (virtual IPv4, NAT type, peer
  count, uptime) and Peers | Events; while stopped, the configuration summary and Edit. Without
  current data from the daemon (no session, or no update for a few seconds) the badge reads
  Unknown and nothing below the header is shown, so a last reported state never looks current.
- **Banner:** above the page when the connection is missing or stale. When the helper is installed
  but launchd or the service manager does not run it, it offers "Start helper", which installs it
  again with one administrator approval.
- **Configuration:** Form | TOML tabs; Save and Save & Apply are separate.
- **Settings:** General, Run mode, Cores, Advanced, About.
- **App updates (About):** a daily check (switchable) and "Check for updates" read the signed
  update of the latest GitHub release; "Install and restart" installs it in place and relaunches.
  In No privileges mode the user daemon stops during the install (Windows cannot replace running
  programs) and starts again with the new version; the helper then updates itself from the signed
  bundle (on macOS by code signature, [packaging.md](packaging.md)). Copies installed by a package manager (`.deb`) and development builds say they cannot
  update themselves.
- **First run:** choose a run mode → install the helper if needed → create or import a network. A
  failed choice (a cancelled prompt, a failed install) is shown on the welcome page under the cards.
- Every page scrolls.

## Peer rows

- **Name** (`friendlyName`): a machine-style hostname (only ASCII letters, digits, `-` and `_`)
  is shown with spaces and the usual capitalization: `macbook-pro` → MacBook Pro,
  `Intel-NUC8i5BEH` → Intel NUC8i5BEH, `Mac-mini-2024` → Mac mini 2024. Words the user already
  capitalized keep their spelling; lower-case words mixing letters and digits (`nuc8i5beh`) are left
  alone. Any other hostname is shown as it is. The raw hostname is the row's tooltip.
- **Avatar** (`vendorLogo`): when a hostname word names a known maker, product line, operating
  system or provider (`mac`, `iphone`, `nuc`, `linksys`, `windows`, `aws`, …), the avatar shows that
  vendor's monochrome logo in the row's color; otherwise the first letter of the name. Only whole
  words match (split at separators, letter–digit and lower–upper boundaries), so `hp` does not match
  `php-dev`. Makers come before operating systems. This device keeps its dot.
- **Copy:** right-clicking a row (Ctrl-click on macOS, Shift+F10) offers Copy IP address, Copy
  MagicDNS name and Copy hostname; a copy button beside the address appears while the row is hovered
  or focused. A toast confirms what was copied.
- **MagicDNS name** (`magicDNSName`): `<hostname>.<zone>` in lower case, offered only when the
  network's saved configuration has `accept_dns = true` and the hostname is a single DNS label. The
  zone is `tld_dns_zone` without its trailing dot, `et.net` by default (see `easytier-upstream.md`).
- Vendor logos are trademarks of their owners, used only to identify a device's maker; their sources
  are listed in `internal/gui/assets/logos/SOURCES.md` (see `packaging.md`, "Vendor logos").

## Wording rules

- Copy is written for the person using the app.
- Never shown in the main interface: reason codes, revision hashes, policy numbers, socket paths,
  profile IDs. Technical detail goes to the diagnostics bundle and to a collapsed "Details" area on
  the log page.

## Visual language

- MyGo light/dark themes plus the desktop accent color; status colors and traffic colors are named
  tokens defined in one place.
- Download is blue and upload is orange; rates use 1024-based units.
- Latency: green below 100 ms, orange below 200 ms, red otherwise. Route tags: `p2p`, `relay N`.
- Body text 14, titles 20–24, nothing below 12.
- Design cues (colors, peer rows, status badge, timeline) follow
  [EasyTier-iOS](https://github.com/EasyTier/EasyTier-iOS); the app icon uses its artwork
  (GPL-3.0-or-later, see `resources/NOTICE`).

## Traffic chart

- Drawn with MyGo `ui/chart`: time axis, area plus monotone line, hover read-out, rounded ticks,
  legend; 1/5/15-minute windows.
- The vertical scale changes only when the data leaves it or stays below a third of it for a while.
- A missing sample is a break in the line, never a dip to zero.
- The view updates only when a new sample arrives.

## Tray

- Menu (Linux AppIndicator offers no click events): status line, one toggle per network, Show,
  Settings, Quit.
- Quit wording follows the run mode: in No privileges and Follow GUI it disconnects every network;
  in Background service the networks keep running, with a separate "Disconnect all and quit".
- The icon reflects connected / disconnected / error. Closing the window hides it to the tray and
  says so the first time; without a tray, closing quits.
- macOS: closing into the tray also hides the Dock icon (Settings → General, on by default; also
  when started hidden at login). Minimizing keeps it; showing the window brings it back.

## Languages

English, Simplified Chinese, Traditional Chinese and Japanese through MyGo's ARB catalogs
(`go tool mygo i18n check`). The app follows the system language and can be switched manually.

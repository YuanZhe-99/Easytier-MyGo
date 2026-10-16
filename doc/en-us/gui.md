# GUI

The interface is a desktop VPN client, not an engineering console (`internal/gui`). This document
states the rules it follows; `internal/gui/gui_test.go` renders every page and can write
screenshots (`SCREENSHOT_DIR`).

## Structure

- **Sidebar:** the network list (status dot, name, virtual IP; "+" to create or import), then
  Logs and Settings.
- **Network page:** a header card (name, EasyTier version, status badge, Connect/Disconnect);
  while running, the traffic row, the live traffic chart, info tiles (virtual IPv4, NAT type, peer
  count, uptime) and Peers | Routes | Events; while stopped, the configuration summary and Edit.
- **Configuration:** Form | TOML tabs; Save and Save & Apply are separate.
- **Settings:** General, Run mode, Cores, Advanced, About.
- **App updates (About):** a daily check (switchable) and "Check for updates" read the signed
  update of the latest GitHub release; "Install and restart" installs it in place and relaunches.
  In No privileges mode the user daemon stops during the install (Windows cannot replace running
  programs) and starts again with the new version; the helper then updates itself from the signed
  bundle. Copies installed by a package manager (`.deb`) and development builds say they cannot
  update themselves.
- **First run:** choose a run mode → install the helper if needed → create or import a network. A
  failed choice (a cancelled prompt, a failed install) is shown on the welcome page under the cards.
- Every page scrolls.

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

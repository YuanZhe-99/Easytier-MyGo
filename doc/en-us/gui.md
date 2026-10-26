# GUI

The interface is a desktop VPN client, not an engineering console (`internal/gui`). This document
states the rules it follows; `internal/gui/gui_test.go` renders every page and can write
screenshots (`SCREENSHOT_DIR`).

## Window

- The window has no system title bar (`TitleBarOptions`): a sidebar on the window's material —
  Mica on Windows 11, the sidebar material on macOS, a shade off the pane elsewhere
  (`windowBase`) — and beside it one pane, a rounded box (14-point corners, a border and a faint
  shadow) 8 points from the window's edges and the sidebar (`App.pane`). The pane's 44-point
  header is the window's title bar (`paneHeader`): the page's icon, title and a line beside it,
  then a status pill or a choice, the page's actions, and on Windows the window's buttons. Its
  title and empty room drag the window; a double click maximizes it.
- **Window buttons:** on Windows the system's would sit on the window's corner, half outside the
  pane, so the window hides them (`HideWindowButtons`) and the header draws minimize, maximize or
  restore, and close after a separator (`windowButtons`), maximize with Windows 11's snap
  layouts; over the Settings sheet or a dialog they stay in place, light on the dimming. On
  macOS the traffic lights sit 9 points from the window's edge, centred on the header, over the
  sidebar's top row, which starts after them. On Linux the desktop's own buttons are centred in
  a title bar as tall as the header and its margin; the header keeps their room
  (`titleRoom`). In full screen no room is kept.
- **Notices:** what needs the user whatever page shows — the helper older than the app (Update
  helper), the helper not installed or not running (Run mode settings, Start helper), the
  background service not attached, data that stopped arriving — is a card in the warning color
  under the header (`noticesView`). A toast shows at the pane's foot.

## Sidebar

- At the top Hide the sidebar and + (Add network), after the traffic lights on macOS; then
  Networks with how many, and a row per network (`sidebarNetwork`): its tile with the state's dot
  on the corner (grey while the state is unknown), its name, and its virtual address while
  connected, else its state in words. The network shown stands out.
- At the foot: Activity, the appearance — follow the system, light and dark in turn (a sun with a
  moon, a sun, a moon) —, Settings, and the run mode as a dot and its name, or what keeps the
  background service away (Helper not installed, Helper not running, Service not running,
  Connecting…), which opens Settings at Run mode (`runChip`).
- Hidden, the sidebar is a rail of icons (`sidebar_rail` in `gui.json`): show the sidebar, +, a
  tile per network named with its state in its tooltip, then the foot's buttons and the run
  mode's dot. On macOS the rail is 72 points wide so that the traffic lights and its icons share
  one centre line.

## Network pane

- **Header:** the network's name, its network name, the core version and "No virtual device"
  when it runs without one, the state as a pill; Edit configuration (the pencil) and Connect
  (primary) or Disconnect.
- **Connected:** why it is not fully connected when it is not (with Try again once automatic
  restarts gave up); saved changes not applied yet as an accent callout with Apply now; the
  traffic card (download, upload, the chart and its window); tiles for the virtual IPv4, the NAT
  type, nodes online and how long it has been connected; then Nodes | Activity, with a hint that
  a node's row copies its address on right-click, and the node list.
- **Disconnected** (`stoppedView`): that it is not connected, the callouts above, and its
  configuration as rows (`summaryOf`, read once per saved revision, never the secret): network
  name, peers, address, virtual device and listening addresses, with Edit configuration; a
  document the summary cannot read says so.
- **Without current data** from the daemon (no session, or no update for a few seconds) the
  pill reads Unknown, Edit and Disconnect are off, and the pane shows only "No current data", so a
  last reported state never looks current (`shownStatus`).
- **No networks:** the pane shows "No networks yet" with Add network and Import file….

## Editing

- Edit configuration opens in the network's pane; its header has Form | TOML, Cancel, Save and
  Save and apply (saving and applying stay separate).
- The form is rows like Settings' (`formView`): Network (name, network name, secret with Show,
  device name, instance ID with Generate new), Address (automatic, virtual IPv4), Connections
  (peers and listening addresses as text areas under their rows, the virtual device), Routing
  (subnets to share, exit nodes), Advanced (folded: the flags, device name, MTU), and Delete
  network last. TOML is MyGo's editor with line numbers; switching carries unsaved edits.
- **Add network** (`createDialogView`) is a dialog over the dimmed window: name, network name,
  secret, first peer and the virtual device as rows, then Import file…, Cancel and Create.
- **Activity** (`logsView`, the sidebar's clock): every network's events, newest 100, by day
  (Today, Yesterday, else the date), each with its time, the state's dot, the sentence with its
  detail and the network; Export diagnostics… in the header.

## Settings

- Settings opens from the sidebar's gear, the run mode at its foot and the tray, never in the
  pane (`settingsSheet`): a sheet over the whole window, sidebar included, 64 points from its
  sides, 48 from its top and 32 from its bottom, over the window dimmed. Its header says Settings
  with ×; Escape, a click outside or × closes it.
- On the left a search field and the sections (`settingsGroups`): Preferences › General; This
  computer › Run mode, EasyTier core; EasyTier MyGo › About & updates. While the search holds
  words, the settings whose title or words hold every one of them, ignoring case, replace the
  sections, titles that match first, each with its page (`settingsHits`, `searchSettings`; every
  hit shows on its page, `TestSettingsHitsShow`); choosing one opens its page scrolled to it and
  tints it for two seconds.
- A page is a title and one sentence, no wider than 720 points, then sections: a muted title,
  with a note or buttons on its right, over a rounded list of rows (`rows`). A row starts with an
  icon in a tile, then its title and one short sentence, and ends with its control.
- **General:** Language and look (language, appearance); Start and close (open at login, start
  in the tray when opened at login, the close button, on macOS hiding the Dock icon);
  Notifications (connection changes).
- **Run mode:** the three run modes as rows, the one in use marked Current and the others with
  Switch, a loading indicator while switching ([run-modes.md](run-modes.md)), then "What is the
  difference between the modes?" with Watch tour ([First run](#first-run)); the privileged
  helper, installed with its version (Update helper when it is older, Remove helper) or not.
- **EasyTier core:** a callout when the daily check found a newer stable release; the cores
  installed (tested or untested, In use, Use, Remove); the daily check; the official releases
  with Show pre-releases and Check for releases, each with Download (size) or Installed.
- **About & updates:** the version with Source code, the license with EasyTier's source code,
  the design credit with EasyTier-iOS; the app's updates — a new version with Install and
  restart, the download's progress, or when it was last checked with Check for updates — and the
  daily check; Diagnostics: the diagnostics file (Export diagnostics…) and the log folder.
- **App updates:** the daily check and "Check for updates" read the signed update of the latest
  GitHub release; "Install and restart" installs it in place and relaunches. In No privileges
  mode the user daemon stops during the install (Windows cannot replace running programs) and
  starts again with the new version; the helper then updates itself from the signed bundle (on
  macOS by code signature, [packaging.md](packaging.md)). Copies installed by a package manager
  (`.deb`) and development builds say they cannot update themselves.

## First run

- The first run is a tour of six steps in one pane, no sidebar (`welcomeShell`, `tourView`); on
  macOS a strip of the window's material as wide as the rail keeps the traffic lights off it. On
  the left the step's illustration; beside it the steps as dots, the language and Skip, the
  step, then Back and Continue. A step slides in from the side it is moved to.
- The steps: **Welcome** (what the app does, what the tour covers); **Virtual device** (with and
  without, a switch to compare: apps on this computer reach the network only with one, other
  devices reach this computer either way); **Run modes** (the three as columns to choose from,
  No privileges at first: virtual device, apps reaching the network, when networks run,
  administrator approval); **Startup and sign-in** (Background service is online from startup
  with nobody signed in; the other two only once you sign in with Open at login on, which the
  step switches); **Staying online** (sleep takes the computer offline in every run mode; where
  this system keeps it awake, with Open power settings on Windows and macOS); **Ready** (the
  choices; finishing installs the helper when the run mode needs it, one administrator prompt).
- Skip asks first, then starts in No privileges, which needs no approval. A failed finish (a
  cancelled prompt, a failed install) is said beside the buttons until the next attempt.
- The illustrations (`tourart.go`) are drawn, not pictures: this computer, seen from the front
  left above, as the system it runs on — a Windows Dev Kit, a Mac mini, a mini PC on Linux —
  with the system's logo on its top (`computerModel`, the logos of `assets/logos`); the network's
  devices with data moving as dashes; the run modes as layers of the computer, the running
  network on the one that runs it; the startup timeline with the stretch that is online; the
  computer awake and asleep in turn. With Reduce Motion nothing moves by itself.
- Watch tour in Settings › Run mode shows the tour again as a sheet over the window
  (`tourSheet`), the run mode in use looked at first. Close, Escape or a click outside go back
  to Settings and change nothing; the last step offers to switch to a run mode looked at.
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

- MyGo light/dark themes plus the desktop accent color; status, traffic and layout values are named
  tokens (`style.go`, `rows.go`). Icons are Lucide line icons (ISC, `resources/NOTICE`) gathered in
  `icons.go`; the window buttons drawn on Windows copy Windows 11's glyphs.
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
- The icon reflects connected / disconnected / error. Its gray and blue are lighter on a dark menu
  bar or taskbar and follow it when it changes, even when the app's own theme differs. Closing the window hides it to the tray and
  says so the first time; without a tray, closing quits.
- macOS: closing into the tray also hides the Dock icon (Settings → General, on by default; also
  when started hidden at login). Minimizing keeps it; showing the window brings it back.

## Languages

English, Simplified Chinese, Traditional Chinese and Japanese through MyGo's ARB catalogs
(`go tool mygo i18n check`). The app follows the system language and can be switched manually.

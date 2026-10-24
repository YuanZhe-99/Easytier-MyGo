# PLAN: the new interface

The interface moves to Kyokai's shell: the sidebar on the window's material, the content as one
floating pane whose header is the window's title bar, and Settings as a sheet over the whole
window, regrouped. The design is the canvas "EasyTier MyGo UI Redesign"
(https://claude.ai/artifact/6CbMfNMqEWWeTgpwuAoEuX); its notes hold the rules per screen.
Not started: the design waits for the user's approval.

Behavior does not change: the same daemon requests, health semantics, save versus apply, the
stale-data rule and the tray. Kyokai is AGPL-3.0, so its code is a reference for MyGo usage only;
nothing is copied.

## M1 Shell

- `TitleBarOptions`: hidden title bar, `Vibrancy` (Mica on Windows, sidebar on macOS),
  `HideWindowButtons` on Windows, traffic lights at x 9 centred on the pane header, Linux
  `TitleBarHeight` 60 so the desktop's buttons centre on it. Remove the 0.1.7 title strip.
- Layout tokens in `style.go` (sidebar 248, rail 56/72, pane margin 8, radius 14, header 44,
  sheet insets 64/48/32, page 720). Lucide icons stay, gathered in one file.
- Sidebar: hide and +, Networks with count, network rows (tile, status badge, name, address or
  state), bottom Activity, appearance cycle, Settings, run-mode chip. Rail, kept in `gui.json`.
- Pane header: icon, name, subtitle, status pill, actions, window buttons on Windows.
- Notice cards above the pane replace the banner; the toast moves to the pane's foot.

## M2 Network pane

- Connected: traffic card, tiles, Peers | Events, peer list (unchanged behavior).
- Disconnected: configuration summary and the pending-changes callout.
- Stale: only the header and "No current data" (regression test `TestShownStatusStale` stays).

## M3 Editor, Add network, Activity

- Editor inside the pane; Form | TOML and Save / Save and apply in the header; form as rows;
  Advanced folded; Delete network last.
- Add network dialog as rows; Import file….
- Activity (was Logs): grouped by day, network tag, Export diagnostics in the header.

## M4 Settings sheet

- Sheet with search and grouped navigation: Preferences › General; This computer › Run mode,
  EasyTier core; EasyTier MyGo › About & updates (Advanced moves into Diagnostics there).
- Rows: tile, title, one sentence, control. Search lists matching settings with their page.
- The tray's Settings and the run-mode chip open the sheet at a page.

## M5 First run and empty state

- Welcome as one pane with selectable run-mode tiles and one Continue button; macOS keeps a
  72-point strip for the traffic lights.
- No networks: centred empty state with Add network and Import file….

## M6 Finish

- ARB strings in en, zh-Hans, zh-Hant, ja; `gui.md` rewritten in both languages; INDEX rows.
- Tests: window buttons clear of content per platform, settings search hits show on their page,
  every page renders in each language and theme; screenshots inspected.
- Real runs on Linux, Windows and macOS.

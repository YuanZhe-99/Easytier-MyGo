package gui

import (
	"runtime"
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/localapi"
	L "github.com/YuanZhe-99/Easytier-MyGo/locales"
)

// view is the whole window (doc/en-us/gui.md, "Window"): the sidebar on
// the window's material and beside it one pane, whose header is the
// window's title bar; on the first run the welcome pane alone. The
// create-network dialog goes over everything.
func (a *App) view(c *ui.Context) {
	t := c.Theme()
	if c.Vibrancy() {
		c.Root().Background(ui.Transparent)
	} else {
		c.Root().Background(windowBase(t))
	}
	a.mu.Lock()
	pg := a.page
	a.mu.Unlock()
	if pg == pageWelcome {
		a.welcomeShell(c)
		return
	}
	ui.Row(c.Key("shell")).Fill().AlignItems(ui.Stretch).Children(func() {
		a.sidebar(c)
		ui.Column(c.Key("main")).Grow(1).MinWidth(0).MinHeight(0).Padding(paneMargin, paneMargin, paneMargin, 0).Children(func() {
			switch pg {
			case pageLogs:
				a.logsView(c)
			case pageEdit:
				a.editView(c)
			default:
				a.networkView(c)
			}
		})
	})
	a.settingsSheet(c)
	a.tourSheet(c)
	a.createDialogView(c)
}

// welcomeShell is the first run: one pane, no sidebar. On macOS a strip
// of the window's material as wide as the rail keeps the traffic lights,
// which MyGo places only as the window opens, off the pane.
func (a *App) welcomeShell(c *ui.Context) {
	a.mu.Lock()
	if a.tour == nil { // the welcome is the tour's first step
		a.tour = &tour{}
	}
	a.mu.Unlock()
	room := a.titleRoom(c)
	ui.Row(c.Key("welcome-shell")).Fill().AlignItems(ui.Stretch).Children(func() {
		left := paneMargin
		if room.Left > 0 {
			ui.Box(c.Key("welcome-lights")).Width(railWidth()).Shrink(0).FillHeight().DragWindow()
			left = 0
		}
		ui.Column(c.Key("welcome-main")).Grow(1).MinWidth(0).Padding(paneMargin, paneMargin, paneMargin, left).Children(func() {
			a.pane(c, paneHead{}, func() { a.tourView(c) })
		})
	})
}

// paneHead is what the pane's header shows: the page's icon, its title and
// one line beside it, then what follows them (a status pill, a choice),
// and the page's actions at the end, before the window buttons.
type paneHead struct {
	icon       *ui.SVG
	title, sub string
	badge      func()
	actions    func()
}

// pane is the floating rounded box beside the sidebar: its header (the
// window's title bar), the notices that need the user, the page's body,
// and the toast at its foot.
func (a *App) pane(c *ui.Context, h paneHead, body func()) {
	t := c.Theme()
	ui.Column(c.Key("pane")).Grow(1).FillWidth().MinHeight(0).Radius(paneRadius).Clip().
		Background(t.Background).Border(1, t.Border).Shadow(0, 6, 18, 0, paneShadow).Children(func() {
		a.paneHeader(c, h)
		a.noticesView(c)
		ui.Column(c.Key("pane-body")).Grow(1).FillWidth().MinHeight(0).Children(body)
		a.toastView(c)
	})
}

// paneHeader is the pane's header, 44 points: its icon, title and line
// drag the window, as does the empty room after them (a double click
// maximizes it); then the actions and, on Windows, the window buttons.
// Notes: on Linux the desktop's buttons sit over its right end
// (titleRoom), which it keeps clear.
func (a *App) paneHeader(c *ui.Context, h paneHead) {
	t := c.Theme()
	room := a.titleRoom(c)
	right := max(8, room.Right-paneMargin)
	ui.Row(c.Key("pane-head")).Height(paneHeaderH).Shrink(0).FillWidth().Padding(0, right, 0, 14).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Row(c.Key("pane-title")).Gap(8).MinWidth(0).AlignItems(ui.Center).DragWindow().Children(func() {
			if h.icon != nil {
				ui.Icon(c, h.icon).Size(navIconSize, navIconSize).TextColor(t.TextMuted)
			}
			if h.title != "" {
				ui.Text(c, h.title).Bold().SingleLine().MaxWidth(paneTitleMax).Ellipsis("…")
			}
			if h.sub != "" {
				ui.Text(c, h.sub).FontSize(sizeSmall).TextColor(t.TextMuted).SingleLine().MinWidth(0).Shrink(1).Ellipsis("…")
			}
		})
		if h.badge != nil {
			h.badge()
		}
		ui.Box(c.Key("pane-drag")).Grow(1).FillHeight().DragWindow()
		if h.actions != nil {
			ui.Row(c.Key("pane-actions")).Gap(6).AlignItems(ui.Center).Children(h.actions)
		}
		a.windowButtons(c, "win", false)
	})
}

// iconButton is a borderless icon button of the pane's header or the
// sidebar, named for assistive technology by its tooltip; on marks the
// page it leads to while that page shows.
// Returns: whether it was clicked.
func iconButton(c *ui.Context, key string, icon *ui.SVG, label string, on bool) bool {
	t := c.Theme()
	b := ui.ButtonBase(c.Key(key)).Size(paneButton, paneButton).Radius(7).Center().Shrink(0).Label(label).Tooltip(label)
	fg := t.TextMuted
	if on || b.Hovered() {
		b.Background(t.SurfaceHover)
		fg = t.Text
	}
	b.Children(func() { ui.Icon(c, icon).Size(navIconSize, navIconSize).TextColor(fg) })
	return b.Clicked()
}

// railWidth is the width of the sidebar folded to its icons: on macOS the
// traffic lights' with their inset on either side, so that they and the
// icons share one centre line.
func railWidth() float32 {
	if runtime.GOOS == "darwin" {
		return railMacW
	}
	return railW
}

// sidebar lists the networks (doc/en-us/gui.md, "Sidebar"): hide the
// sidebar and + at the top, after the traffic lights on macOS; Networks
// and how many; a row per network; and at the foot Activity, the
// appearance, Settings and the run mode. Folded, it is the rail.
func (a *App) sidebar(c *ui.Context) {
	t := c.Theme()
	a.mu.Lock()
	nets := append([]localapi.NetworkStatus(nil), a.networks...)
	sel, pg, stale, rail, sheet := a.selected, a.page, a.stale(), a.prefs.SidebarRail, a.settingsOpen
	a.mu.Unlock()
	if rail {
		a.railView(c, nets, sel, pg, stale, sheet)
		return
	}
	room := a.titleRoom(c)
	ui.Column(c.Key("sidebar")).Width(sidebarW).Shrink(0).FillHeight().Padding(10, 10, 10, 12).Gap(2).Children(func() {
		ui.Row(c.Key("sb-top")).Height(sbTopH).Shrink(0).Gap(2).AlignItems(ui.Center).Padding(0, 0, 0, max(room.Left-12, 0)).Children(func() {
			if iconButton(c, "sb-hide", iconSidebar, L.SidebarHide(), false) {
				a.setRail(true)
			}
			if iconButton(c, "sb-add", iconPlus, L.ActionAddNetwork(), false) {
				a.openCreate()
			}
			ui.Box(c.Key("sb-drag")).Grow(1).FillHeight().DragWindow()
		})
		ui.Row(c.Key("sb-head")).Padding(14, 10, 6, 10).Children(func() {
			ui.Text(c, L.SidebarNetworks()).FontSize(sizeSmall).Bold().TextColor(t.TextMuted).Grow(1)
			ui.Text(c, strconv.Itoa(len(nets))).FontSize(sizeSmall).TextColor(t.TextMuted)
		})
		ui.Scroll(c.Key("sb-list")).Grow(1).FillWidth().Gap(2).Children(func() {
			if len(nets) == 0 {
				ui.Text(c, L.SidebarEmpty()).FontSize(sizeSmall).TextColor(t.TextMuted).Padding(2, 10)
			}
			for _, n := range nets {
				a.sidebarNetwork(c, n, (pg == pageNetwork || pg == pageEdit) && n.ID == sel, stale)
			}
		})
		ui.Row(c.Key("sb-foot")).Shrink(0).Gap(2).AlignItems(ui.Center).Padding(8, 0, 0, 0).Children(func() {
			if iconButton(c, "sb-activity", iconActivity, L.PageLogs(), pg == pageLogs) {
				a.update(func() { a.page = pageLogs })
			}
			a.themeButton(c, "sb-theme")
			if iconButton(c, "sb-settings", iconSettings, L.PageSettings(), sheet) {
				a.openSettings(setGeneral)
			}
			ui.Box(c).Grow(1)
			a.runChip(c)
		})
	})
}

// sidebarNetwork is one network's row: its tile with the state's dot on
// the corner, its name, and its virtual address while connected, else its
// state in words. The network shown stands out.
func (a *App) sidebarNetwork(c *ui.Context, n localapi.NetworkStatus, selected, stale bool) {
	t := c.Theme()
	n = shownStatus(n, stale)
	row := ui.ButtonBase(c.Key("net-"+n.ID)).Height(sbRowH).FillWidth().Radius(9).Padding(0, 10).Label(n.Name)
	switch {
	case selected:
		row.Background(t.Background).Shadow(0, 1, 4, 0, paneShadow)
	case row.Hovered():
		row.Background(t.SurfaceHover)
	}
	sub := stateLabel(n)
	if n.State == localapi.StateConnected && n.VirtualIPv4 != "" {
		sub = shortIP(n.VirtualIPv4)
	}
	row.Children(func() {
		ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
			networkTile(c, n, selected)
			ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
				name := ui.Text(c, n.Name).SingleLine().Ellipsis("…")
				if selected {
					name.Bold()
				}
				ui.Text(c, sub).FontSize(sizeSmall).TextColor(t.TextMuted).SingleLine().Ellipsis("…")
			})
		})
	})
	if row.Clicked() {
		a.selectNetwork(n.ID)
	}
}

// networkTile is a network's tile in the sidebar or the rail: the network
// icon on a rounded face, in the accent's tint while it shows, and the
// state's dot on its corner, ringed in what the tile sits on.
func networkTile(c *ui.Context, n localapi.NetworkStatus, selected bool) {
	t := c.Theme()
	face, fg, ring := tileBackground(t), t.TextMuted, windowBase(t)
	if selected {
		face, fg, ring = t.Accent.Alpha(0.14), t.Accent, t.Background
	}
	// An unknown state is no state: its dot is grey, not a warning.
	badge := stateColor(tok(c), n.State)
	if n.State == localapi.StateUnknown {
		badge = t.TextMuted
	}
	ui.Box(c).Size(netTileSize, netTileSize).Shrink(0).Radius(8).Center().Background(face).Children(func() {
		ui.Icon(c, iconNetwork).Size(navIconSize, navIconSize).TextColor(fg)
		ui.Box(c).Absolute().Right(-2).Bottom(-2).Size(netBadgeSize, netBadgeSize).Radius(netBadgeSize/2).
			Background(badge).Border(2, ring)
	})
}

// tileBackground is a tile's face: a shade off whatever it sits on.
func tileBackground(t *ui.Theme) ui.Color {
	if t.Dark {
		return ui.RGBA(255, 255, 255, 0.06)
	}
	return ui.RGBA(9, 9, 11, 0.045)
}

// railView is the sidebar folded to its icons: show the sidebar and +, a
// tile per network (its name and state in the tooltip), then Activity, the
// appearance, Settings and the run mode's dot.
func (a *App) railView(c *ui.Context, nets []localapi.NetworkStatus, sel string, pg page, stale, sheet bool) {
	t := c.Theme()
	room := a.titleRoom(c)
	ui.Column(c.Key("rail")).Width(max(railWidth(), room.Left)).Shrink(0).FillHeight().Padding(10, 0).Gap(4).AlignItems(ui.Center).Children(func() {
		if room.Left > 0 {
			ui.Box(c.Key("rail-lights")).Height(max(sbTopH, room.Height-10)).Shrink(0).FillWidth().DragWindow()
		}
		if iconButton(c, "rail-show", iconSidebar, L.SidebarShow(), false) {
			a.setRail(false)
		}
		if iconButton(c, "rail-add", iconPlus, L.ActionAddNetwork(), false) {
			a.openCreate()
		}
		ui.Box(c).Width(24).Height(1).Shrink(0).Margin(6, 0).Background(t.Border)
		ui.Scroll(c.Key("rail-list")).Grow(1).Gap(4).AlignItems(ui.Center).Children(func() {
			for _, n := range nets {
				n = shownStatus(n, stale)
				selected := (pg == pageNetwork || pg == pageEdit) && n.ID == sel
				label := n.Name + " — " + stateLabel(n)
				b := ui.ButtonBase(c.Key("rail-net-"+n.ID)).Size(railButton, railButton).Radius(9).Center().Shrink(0).Label(label).Tooltip(label)
				switch {
				case selected:
					b.Background(t.Background).Shadow(0, 1, 4, 0, paneShadow)
				case b.Hovered():
					b.Background(t.SurfaceHover)
				}
				b.Children(func() { networkTile(c, n, selected) })
				if b.Clicked() {
					a.selectNetwork(n.ID)
				}
			}
		})
		if iconButton(c, "rail-activity", iconActivity, L.PageLogs(), pg == pageLogs) {
			a.update(func() { a.page = pageLogs })
		}
		a.themeButton(c, "rail-theme")
		if iconButton(c, "rail-settings", iconSettings, L.PageSettings(), sheet) {
			a.openSettings(setGeneral)
		}
		word, col := a.runState(t)
		b := ui.ButtonBase(c.Key("rail-run")).Size(railButton, railButton).Radius(9).Center().Label(word).Tooltip(word)
		if b.Hovered() {
			b.Background(t.SurfaceHover)
		}
		b.Children(func() { dot(c, col, runDot+1) })
		if b.Clicked() {
			a.openSettings(setRunMode)
		}
	})
}

// setRail folds the sidebar to its icons, or unfolds it, and keeps the
// choice.
func (a *App) setRail(on bool) {
	a.mu.Lock()
	p := a.prefs
	a.mu.Unlock()
	p.SidebarRail = on
	a.setPrefs(p)
}

// themeButton is the sidebar's appearance: follow the system, light and
// dark in turn, its icon saying which is chosen.
func (a *App) themeButton(c *ui.Context, key string) {
	a.mu.Lock()
	theme := a.prefs.Theme
	a.mu.Unlock()
	icon, label, next := iconSunMoon, L.AppearanceSystem(), "light"
	switch theme {
	case "light":
		icon, label, next = iconSun, L.AppearanceLight(), "dark"
	case "dark":
		icon, label, next = iconMoon, L.AppearanceDark(), "system"
	}
	if iconButton(c, key, icon, label, false) {
		a.setTheme(next)
	}
}

// runState is the run mode at the sidebar's foot: its name and the
// success color while the daemon is attached, else what keeps it away —
// the helper missing or stopped, the service not answering, or still
// connecting — in the warning or danger color.
func (a *App) runState(t *ui.Theme) (string, ui.Color) {
	a.mu.Lock()
	connected, connErr, mode, st, choice := a.connected, a.connErr, a.prefs.Mode, a.helperSt, a.currentChoice()
	a.mu.Unlock()
	switch {
	case !connected && mode == ModeHelper && connErr == errHelperMissing.Error():
		return L.RunHelperMissing(), t.Warning
	case !connected && mode == ModeHelper && st.Installed && !st.Active:
		return L.RunHelperStopped(), t.Warning
	case !connected && connErr != "":
		return L.RunServiceDown(), t.Danger
	case !connected:
		return L.RunConnecting(), t.Warning
	}
	switch choice {
	case choiceFollow:
		return L.ModeFollowTitle(), t.Success
	case choiceBackground:
		return L.ModeBackgroundTitle(), t.Success
	}
	return L.ModeUserTitle(), t.Success
}

// runChip is the run mode at the sidebar's foot: a dot and a word, which
// open Settings at the run mode.
func (a *App) runChip(c *ui.Context) {
	t := c.Theme()
	word, col := a.runState(t)
	b := ui.ButtonBase(c.Key("sb-run")).Height(paneButton).Padding(0, 10).Radius(7).Shrink(1).MinWidth(0).Label(word).Tooltip(L.ActionOpenRunMode())
	if b.Hovered() {
		b.Background(t.SurfaceHover)
	}
	b.Children(func() {
		ui.Row(c).Gap(7).AlignItems(ui.Center).MinWidth(0).Children(func() {
			dot(c, col, runDot)
			ui.Text(c, word).FontSize(sizeSmall).TextColor(t.TextMuted).SingleLine().Ellipsis("…").MinWidth(0)
		})
	})
	if b.Clicked() {
		a.openSettings(setRunMode)
	}
}

// selectNetwork shows a network's page.
func (a *App) selectNetwork(id string) {
	a.update(func() {
		a.selected, a.page, a.chartSel = id, pageNetwork, -1
	})
	a.syncWatch()
}

// noticesView is what needs the user whatever page shows, under the
// pane's header: the helper older than the app, the helper missing or
// stopped, the daemon not attached, and data that stopped arriving — each
// a card in the warning color with its action.
func (a *App) noticesView(c *ui.Context) {
	a.mu.Lock()
	connected, connErr, stale, mode := a.connected, a.connErr, a.stale(), a.prefs.Mode
	outdated, busy := a.helperOutdated, a.busy["mode"]
	// An installed helper that launchd does not run (a failed reload)
	// needs an administrator to start it again.
	unloaded := !connected && mode == ModeHelper && a.helperSt.Installed && !a.helperSt.Active
	pg := a.page
	a.mu.Unlock()
	if pg == pageWelcome {
		return
	}
	if outdated && connected {
		noticeCard(c, "notice-outdated", L.BannerHelperOutdated(), func() {
			if ui.Button(c, L.ActionUpdateHelper()).Disabled(busy).Clicked() {
				a.reinstallHelper()
			}
		})
	}
	var msg string
	switch {
	case !connected && mode == ModeHelper && connErr == errHelperMissing.Error():
		msg = L.BannerHelperMissing()
	case unloaded:
		msg = L.BannerHelperStopped()
	case !connected && connErr != "":
		msg = L.BannerDisconnected(connErr)
	case !connected:
		msg = L.BannerConnecting()
	case stale:
		msg = L.BannerStale()
	default:
		return
	}
	noticeCard(c, "notice-conn", msg, func() {
		switch {
		case unloaded:
			if ui.PrimaryButton(c, L.ActionStartHelper()).Disabled(busy).Clicked() {
				a.reinstallHelper()
			}
		case !connected && mode == ModeHelper:
			if ui.Button(c, L.ActionOpenRunMode()).Clicked() {
				a.openSettings(setRunMode)
			}
		}
	})
}

// noticeCard is a card under the pane's header in the warning color: its
// round icon, the message, and its actions on the right.
func noticeCard(c *ui.Context, key, msg string, actions func()) {
	t := c.Theme()
	k := tok(c)
	ui.Column(c.Key(key)).FillWidth().Shrink(0).Padding(0, 12, 8, 12).Children(func() {
		ui.Row(c.Key(key+"-card")).FillWidth().Padding(noticePadY-4, noticePadX-4, noticePadY-4, noticePadX-4).
			Radius(cardRadius).Border(1, k.warn).Background(k.warn.Alpha(0.12)).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(noticeIconSz, noticeIconSz).Shrink(0).Radius(noticeIconSz / 2).Center().Background(t.Background).Children(func() {
				ui.Icon(c, iconAlert).Size(navIconSize+2, navIconSize+2).TextColor(k.warn)
			})
			ui.Text(c, msg).Grow(1).MinWidth(0)
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(actions)
		})
	})
}

// toastView shows the latest message for a few seconds at the pane's foot.
func (a *App) toastView(c *ui.Context) {
	a.mu.Lock()
	msg, isErr, at := a.toast, a.toastErr, a.toastAt
	a.mu.Unlock()
	if msg == "" || time.Since(at) > 5*time.Second {
		return
	}
	c.After(5*time.Second - time.Since(at))
	t := c.Theme()
	k := tok(c)
	// The theme may leave Inverse and InverseText unset (zero is
	// transparent): fall back the way MyGo's own toasts do.
	col, text := t.Inverse, t.InverseText
	if col == (ui.Color{}) {
		col = t.Text
	}
	if text == (ui.Color{}) {
		text = t.Background
	}
	if isErr {
		col, text = k.bad, k.onBad
	}
	ui.Row(c.Key("toast")).Absolute().Bottom(16).Left(16).Right(16).Children(func() {
		ui.Row(c).FillWidth().Padding(10, 14).Radius(10).Background(col).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, msg).TextColor(text).FontSize(sizeSmall).Grow(1)
			if ui.Button(c, L.ActionDismiss()).Clicked() {
				a.update(func() { a.toast = "" })
			}
		})
	})
}

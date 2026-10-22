package gui

import (
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/localapi"
	L "github.com/YuanZhe-99/Easytier-MyGo/locales"
)

// Icons (Lucide, ISC license; see resources/NOTICE).
var (
	iconPlus     = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12h14"/><path d="M12 5v14"/></svg>`))
	iconSettings = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="21" x2="14" y1="4" y2="4"/><line x1="10" x2="3" y1="4" y2="4"/><line x1="21" x2="12" y1="12" y2="12"/><line x1="8" x2="3" y1="12" y2="12"/><line x1="21" x2="16" y1="20" y2="20"/><line x1="12" x2="3" y1="20" y2="20"/><line x1="14" x2="14" y1="2" y2="6"/><line x1="8" x2="8" y1="10" y2="14"/><line x1="16" x2="16" y1="18" y2="22"/></svg>`))
	iconCopy     = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect width="14" height="14" x="8" y="8" rx="2" ry="2"/><path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2"/></svg>`))
	iconLogs     = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 6v6l4 2"/><circle cx="12" cy="12" r="10"/></svg>`))
)

// view is the whole window.
func (a *App) view(c *ui.Context) {
	a.mu.Lock()
	pg := a.page
	a.mu.Unlock()
	t := c.Theme()
	if pg == pageWelcome {
		ui.Column(c).Fill().Background(t.Background).Children(func() {
			a.titleBar(c)
			a.welcomeView(c)
		})
		return
	}
	ui.Column(c).Fill().Background(t.Background).Children(func() {
		a.titleBar(c)
		ui.Row(c).Fill().Grow(1).AlignItems(ui.Stretch).Children(func() {
			a.sidebar(c)
			ui.Column(c).Grow(1).Fill().Children(func() {
				a.banner(c)
				switch pg {
				case pageSettings:
					a.settingsView(c)
				case pageLogs:
					a.logsView(c)
				case pageEdit:
					a.editView(c)
				default:
					a.networkView(c)
				}
			})
		})
		a.toastView(c)
	})
	a.createDialogView(c)
}

// sidebar lists the networks, then Logs and Settings.
func (a *App) sidebar(c *ui.Context) {
	t := c.Theme()
	k := tok(c)
	a.mu.Lock()
	nets := append([]localapi.NetworkStatus(nil), a.networks...)
	sel, pg, stale := a.selected, a.page, a.stale()
	a.mu.Unlock()
	ui.Column(c).Width(sidebarW).FillHeight().Background(t.Surface).Padding(12, 8).Gap(4).Children(func() {
		ui.Row(c).PaddingX(8).PaddingY(4).AlignItems(ui.Center).Children(func() {
			ui.Text(c, L.SidebarNetworks()).FontSize(sizeSmall).TextColor(k.muted).Bold().Grow(1)
			add := ui.Button(c, "").Label(L.ActionAddNetwork()).Tooltip(L.ActionAddNetwork())
			add.Children(func() { ui.Icon(c, iconPlus).Size(16, 16) })
			if add.Clicked() {
				a.openCreate()
			}
		})
		ui.Scroll(c).Grow(1).Gap(2).Children(func() {
			if len(nets) == 0 {
				ui.Text(c, L.SidebarEmpty()).FontSize(sizeSmall).TextColor(k.muted).Padding(8)
			}
			for _, n := range nets {
				a.sidebarNetwork(c, n, pg == pageNetwork && n.ID == sel || pg == pageEdit && n.ID == sel, stale)
			}
		})
		ui.Divider(c)
		a.sidebarLink(c, "logs", iconLogs, L.PageLogs(), pg == pageLogs, func() { a.page = pageLogs })
		a.sidebarLink(c, "settings", iconSettings, L.PageSettings(), pg == pageSettings, func() { a.page = pageSettings })
	})
}

// sidebarNetwork is one network row: status dot, name, virtual IP.
func (a *App) sidebarNetwork(c *ui.Context, n localapi.NetworkStatus, selected, stale bool) {
	t := c.Theme()
	k := tok(c)
	row := ui.ButtonBase(c.Key("net-"+n.ID)).Padding(8, 10).Radius(8).FillWidth().Label(n.Name)
	if selected {
		row.Background(t.Accent.Alpha(0.14))
	}
	row.Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).FillWidth().Children(func() {
			col := stateColor(k, n.State)
			if stale {
				col = k.muted
			}
			dot(c, col, 9)
			ui.Column(c).Grow(1).Gap(2).Children(func() {
				ui.Text(c, n.Name).FontSize(sizeBody).Bold().Ellipsis("…").NoWrap()
				sub := stateLabel(n)
				if n.VirtualIPv4 != "" {
					sub = shortIP(n.VirtualIPv4)
				}
				ui.Text(c, sub).FontSize(sizeSmall).TextColor(k.muted).NoWrap()
			})
		})
	})
	if row.Clicked() {
		a.selectNetwork(n.ID)
	}
}

func (a *App) sidebarLink(c *ui.Context, key string, icon *ui.SVG, label string, selected bool, open func()) {
	t := c.Theme()
	row := ui.ButtonBase(c.Key("link-"+key)).Padding(8, 10).Radius(8).FillWidth().Label(label)
	if selected {
		row.Background(t.Accent.Alpha(0.14))
	}
	row.Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).FillWidth().Children(func() {
			ui.Icon(c, icon).Size(16, 16).TextColor(t.TextMuted)
			ui.Text(c, label).FontSize(sizeBody)
		})
	})
	if row.Clicked() {
		a.update(open)
	}
}

// selectNetwork shows a network's page.
func (a *App) selectNetwork(id string) {
	a.update(func() {
		a.selected, a.page, a.chartSel = id, pageNetwork, -1
	})
	a.syncWatch()
}

// banner explains a missing or stale connection.
func (a *App) banner(c *ui.Context) {
	k := tok(c)
	a.mu.Lock()
	connected, connErr, stale, mode := a.connected, a.connErr, a.stale(), a.prefs.Mode
	outdated, busy := a.helperOutdated, a.busy["mode"]
	// An installed helper that launchd does not run (a failed reload)
	// needs an administrator to start it again.
	unloaded := !connected && mode == ModeHelper && a.helperSt.Installed && !a.helperSt.Active
	a.mu.Unlock()
	if outdated && connected {
		ui.Row(c).FillWidth().Padding(10, 16).Gap(8).AlignItems(ui.Center).Background(k.warn.Alpha(0.14)).Children(func() {
			dot(c, k.warn, 8)
			ui.Text(c, L.BannerHelperOutdated()).FontSize(sizeSmall).Grow(1)
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
	ui.Row(c).FillWidth().Padding(10, 16).Gap(8).AlignItems(ui.Center).Background(k.warn.Alpha(0.14)).Children(func() {
		dot(c, k.warn, 8)
		ui.Text(c, msg).FontSize(sizeSmall).Grow(1)
		if unloaded {
			if ui.PrimaryButton(c, L.ActionStartHelper()).Disabled(busy).Clicked() {
				a.reinstallHelper()
			}
		} else if !connected && mode == ModeHelper {
			if ui.Button(c, L.ActionOpenRunMode()).Clicked() {
				a.update(func() { a.page = pageSettings })
			}
		}
	})
}

// toastView shows the latest message for a few seconds.
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
	ui.Row(c).Absolute().Bottom(16).Left(sidebarW + 16).Right(16).Children(func() {
		ui.Row(c).FillWidth().Padding(10, 14).Radius(10).Background(col).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, msg).TextColor(text).FontSize(sizeSmall).Grow(1)
			if ui.Button(c, L.ActionDismiss()).Clicked() {
				a.update(func() { a.toast = "" })
			}
		})
	})
}

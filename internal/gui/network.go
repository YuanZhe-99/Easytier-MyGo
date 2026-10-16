package gui

import (
	"fmt"
	"slices"

	"github.com/egoist/mygo/ui"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/health"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/localapi"
	L "github.com/YuanZhe-99/Easytier-MyGo/locales"
)

// networkView is the selected network's page.
func (a *App) networkView(c *ui.Context) {
	a.mu.Lock()
	n, ok := a.networkByID(a.selected)
	connected := a.connected
	empty := len(a.networks) == 0
	a.mu.Unlock()
	ui.Scroll(c.Key("network-page")).Grow(1).Padding(24).Gap(16).Children(func() {
		switch {
		case empty && connected:
			a.emptyState(c)
		case !ok:
			ui.Text(c, L.NetworkNoneSelected()).TextColor(c.Theme().TextMuted)
		default:
			a.networkHeader(c, n)
			if n.Run && n.State != localapi.StateStopped {
				a.trafficCard(c, n)
				a.infoTiles(c, n)
				a.networkTabs(c, n)
			} else {
				a.stoppedCard(c, n)
			}
		}
	})
}

// emptyState invites the first network.
func (a *App) emptyState(c *ui.Context) {
	card(c).AlignItems(ui.Center).Padding(40).Gap(14).Children(func() {
		ui.Text(c, L.EmptyTitle()).FontSize(sizeTitle).Bold()
		ui.Text(c, L.EmptyBody()).TextColor(c.Theme().TextMuted).TextAlign(ui.Center).MaxWidth(420)
		if ui.PrimaryButton(c, L.ActionAddNetwork()).Clicked() {
			a.openCreate()
		}
	})
}

// networkHeader: name, network name and core, status, main actions.
func (a *App) networkHeader(c *ui.Context, n localapi.NetworkStatus) {
	k := tok(c)
	a.mu.Lock()
	busy := a.busy["connect-"+n.ID]
	a.mu.Unlock()
	card(c).Children(func() {
		ui.Row(c).Gap(16).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).Gap(4).Children(func() {
				ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
					ui.Text(c, n.Name).FontSize(sizeTitle).Bold().Ellipsis("…").NoWrap()
					statusBadge(c, n)
				})
				var sub []string
				if n.NetworkName != "" {
					sub = append(sub, L.HeaderNetworkName(n.NetworkName))
				}
				if n.CoreVersion != "" {
					sub = append(sub, "EasyTier "+n.CoreVersion)
				}
				if n.NoTUN {
					sub = append(sub, L.HeaderNoTun())
				}
				if len(sub) > 0 {
					ui.Text(c, joinDot(sub)).FontSize(sizeSmall).TextColor(k.muted)
				}
			})
			if ui.Button(c.Key("edit"), L.ActionEdit()).Clicked() {
				a.openEditor(n.ID)
			}
			label, req := L.ActionConnect(), localapi.RequestConnect
			if n.Run {
				label, req = L.ActionDisconnect(), localapi.RequestDisconnect
			}
			var btn ui.Element
			if n.Run {
				btn = ui.Button(c.Key("disconnect"), label)
			} else {
				btn = ui.PrimaryButton(c.Key("connect"), label)
			}
			btn.Disabled(busy).MinWidth(110)
			if btn.Clicked() {
				a.do("connect-"+n.ID, localapi.Request{Type: req, Network: n.ID}, "", nil)
			}
		})
		if d := stateDetail(n); d != "" {
			ui.Text(c, d).FontSize(sizeSmall).TextColor(stateColor(k, n.State))
			if n.State == localapi.StateFailed && n.Recovery == health.RecoveryExhausted {
				if ui.Button(c, L.ActionRetry()).Clicked() {
					a.do("retry-"+n.ID, localapi.Request{Type: localapi.RequestRetry, Network: n.ID}, "", nil)
				}
			}
		}
		if n.Saved != "" && n.Applied != "" && n.Saved != n.Applied {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, L.HeaderPendingChanges()).FontSize(sizeSmall).TextColor(k.warn).Grow(1)
				if ui.Button(c, L.ActionApplyNow()).Clicked() {
					a.do("apply-"+n.ID, localapi.Request{Type: localapi.RequestApply, Network: n.ID}, L.ToastApplied(), nil)
				}
			})
		}
	})
}

func joinDot(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += "  ·  "
		}
		out += p
	}
	return out
}

// stoppedCard explains a disconnected network.
func (a *App) stoppedCard(c *ui.Context, n localapi.NetworkStatus) {
	t := c.Theme()
	a.mu.Lock()
	mode, bg := a.prefs.Mode, a.settings.Background
	a.mu.Unlock()
	card(c).Children(func() {
		msg := L.StoppedBody()
		if n.Run && n.State == localapi.StateStopped {
			msg = L.StoppedWaiting()
		}
		ui.Text(c, msg).TextColor(t.TextMuted)
		if mode == ModeHelper && bg {
			ui.Text(c, L.StoppedBackgroundNote()).FontSize(sizeSmall).TextColor(t.TextMuted)
		}
	})
}

// trafficCard: current rates and the live chart.
func (a *App) trafficCard(c *ui.Context, n localapi.NetworkStatus) {
	k := tok(c)
	a.mu.Lock()
	pts := slices.Clone(a.traffic[n.ID])
	stale := a.stale()
	a.mu.Unlock()
	card(c).Children(func() {
		ui.Row(c).Gap(32).AlignItems(ui.End).Children(func() {
			trafficItem(c, L.TrafficDownload(), n.RxRate, k.download, stale)
			trafficItem(c, L.TrafficUpload(), n.TxRate, k.upload, stale)
			ui.Spacer(c)
			a.mu.Lock()
			ui.Segmented(c.Key("chart-window"), &a.window5m, L.ChartWindow1m(), L.ChartWindow5m(), L.ChartWindow15m()).Label(L.ChartWindowLabel())
			a.mu.Unlock()
		})
		a.trafficChart(c, pts)
	})
}

// trafficItem is one big rate number.
func trafficItem(c *ui.Context, label string, rate float64, col ui.Color, stale bool) {
	t := c.Theme()
	ui.Column(c).Gap(2).Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			dot(c, col, 8)
			ui.Text(c, label).FontSize(sizeSmall).TextColor(t.TextMuted)
		})
		value := formatRate(rate)
		if stale {
			value = "–"
		}
		ui.Text(c, value).FontSize(sizeTitle).Bold().NoWrap()
	})
}

// infoTiles: virtual IP, NAT type, peers, uptime.
func (a *App) infoTiles(c *ui.Context, n localapi.NetworkStatus) {
	ui.Row(c).Gap(gap).FillWidth().Children(func() {
		tile(c, L.TileVirtualIP(), orDash(shortIP(n.VirtualIPv4)))
		tile(c, L.TileNatType(), natLabel(n.NATType))
		tile(c, L.TilePeers(), fmt.Sprint(n.PeerCount))
		tile(c, L.TileUptime(), formatUptime(n.StartedAt))
	})
}

func tile(c *ui.Context, label, value string) {
	t := c.Theme()
	ui.Column(c).Grow(1).Basis(0).MinWidth(0).Background(t.Surface).Radius(cardRadius).Padding(cardPad).Border(1, t.Border).Gap(4).Children(func() {
		ui.Text(c, label).FontSize(sizeSmall).TextColor(c.Theme().TextMuted)
		ui.Text(c, value).FontSize(sizeHeading).Bold().Selectable().Ellipsis("…").NoWrap()
	})
}

func orDash(s string) string {
	if s == "" {
		return "–"
	}
	return s
}

// networkTabs: peers and events of the network.
func (a *App) networkTabs(c *ui.Context, n localapi.NetworkStatus) {
	a.mu.Lock()
	ui.Segmented(c.Key("net-tab"), &a.netTab, L.TabPeers(), L.TabEvents()).AlignSelf(ui.Start)
	tab := a.netTab
	detail := a.details[n.ID]
	var evs []health.Event
	for _, e := range a.events {
		if e.Component.ID == n.ID {
			evs = append(evs, e)
		}
	}
	a.mu.Unlock()
	if tab == 1 {
		eventTimeline(c, evs, false)
		return
	}
	a.peerList(c, detail)
}

// peerList shows the nodes of the network (local node first).
func (a *App) peerList(c *ui.Context, d *localapi.NetworkDetail) {
	t := c.Theme()
	k := tok(c)
	if d == nil || len(d.Peers) == 0 {
		ui.Text(c, L.PeersLoading()).TextColor(t.TextMuted)
		return
	}
	card(c).Padding(4).Gap(0).Children(func() {
		for i, p := range d.Peers {
			if i > 0 {
				ui.Divider(c)
			}
			ui.Row(c.Key(fmt.Sprintf("peer-%d", i))).Padding(10, 12).Gap(12).AlignItems(ui.Center).Children(func() {
				col, label := k.peer, "P"
				switch {
				case p.Local:
					col, label = k.local, L.PeerThisDevice()
				case p.Version == "" && p.IPv4 == "":
					col = k.server
				}
				ui.Box(c).Width(36).Height(36).Radius(18).Background(col.Alpha(0.14)).Center().Children(func() {
					initial := label
					if !p.Local && p.Hostname != "" {
						initial = string([]rune(p.Hostname)[:1])
					}
					if p.Local {
						initial = "●"
					}
					ui.Text(c, initial).TextColor(col).Bold()
				})
				ui.Column(c).Grow(1).Gap(2).Children(func() {
					name := p.Hostname
					if p.Local {
						name = p.Hostname + "  (" + L.PeerThisDevice() + ")"
					}
					ui.Text(c, name).Bold().Ellipsis("…").NoWrap()
					ui.Text(c, joinDot(nonEmpty(shortIP(p.IPv4), p.Tunnel, natLabel(p.NATType)))).FontSize(sizeSmall).TextColor(t.TextMuted).NoWrap()
				})
				if p.Local {
					return
				}
				if p.Direct {
					capsule(c, L.PeerDirect(), k.peer)
				} else {
					capsule(c, L.PeerRelay(p.Cost), k.relay)
				}
				if p.LossRate >= 0.02 {
					capsule(c, fmt.Sprintf("%.0f%%", p.LossRate*100), lossColor(k, p.LossRate))
				}
				ui.Text(c, formatLatency(p.LatencyMS)).TextColor(latencyColor(k, p.LatencyMS)).Bold().NoWrap().MinWidth(70).TextAlign(ui.End)
			})
		}
	})
}

func nonEmpty(xs ...string) []string {
	var out []string
	for _, x := range xs {
		if x != "" && x != "–" {
			out = append(out, x)
		}
	}
	return out
}

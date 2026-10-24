package gui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/configdoc"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/health"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/localapi"
	L "github.com/YuanZhe-99/Easytier-MyGo/locales"
)

// netContentW is the network page's column, at most.
const netContentW float32 = 1120

// networkView is the selected network's pane (doc/en-us/gui.md, "Network
// pane"): connected, its traffic, tiles and peers; disconnected, a summary
// of its configuration; without current data from the daemon, only its
// header and "No current data".
func (a *App) networkView(c *ui.Context) {
	a.mu.Lock()
	n, ok := a.networkByID(a.selected)
	connected, stale := a.connected, a.stale()
	empty := len(a.networks) == 0
	a.mu.Unlock()
	n = shownStatus(n, stale)
	switch {
	case empty && connected:
		a.pane(c, paneHead{}, func() { a.emptyState(c) })
		return
	case !ok:
		a.pane(c, paneHead{}, func() {
			centered(c, "none", iconNetwork, L.NetworkNoneSelected(), "", nil)
		})
		return
	}
	a.pane(c, a.networkHead(c, n, stale), func() {
		if stale {
			centered(c, "nodata", iconUnplug, L.NoDataTitle(), L.NoDataBody(), nil)
			return
		}
		running := n.Run && n.State != localapi.StateStopped
		if !running {
			a.stoppedView(c, n)
			return
		}
		ui.Scroll(c.Key("network-page")).Grow(1).FillWidth().Padding(4, 24, 24, 24).AlignItems(ui.Center).Children(func() {
			ui.Column(c.Key("network-col")).FillWidth().MaxWidth(netContentW).Gap(gap).Children(func() {
				a.networkNotes(c, n)
				a.trafficCard(c, n)
				a.infoTiles(c, n)
				a.networkTabs(c, n)
			})
		})
	})
}

// shownStatus is the status the network page shows: without current data
// from the daemon (no session, or no frame for staleAfter) the network's
// state is unknown, never its last reported one (regression: the
// page kept showing "Connected" and its uptime after the helper had gone).
func shownStatus(n localapi.NetworkStatus, stale bool) localapi.NetworkStatus {
	if stale {
		n.State, n.Reason, n.Error = localapi.StateUnknown, "", ""
	}
	return n
}

// networkHead is the pane header of a network: its name, its network
// name, core and whether it runs without a virtual device, the state as a
// pill; Edit and Connect or Disconnect. Without current data the actions
// are off.
func (a *App) networkHead(c *ui.Context, n localapi.NetworkStatus, stale bool) paneHead {
	a.mu.Lock()
	busy := a.busy["connect-"+n.ID]
	a.mu.Unlock()
	var sub []string
	if n.NetworkName != "" {
		sub = append(sub, n.NetworkName)
	}
	if n.CoreVersion != "" && !stale {
		sub = append(sub, "EasyTier "+n.CoreVersion)
	}
	if n.NoTUN {
		sub = append(sub, L.HeaderNoTun())
	}
	return paneHead{
		icon: iconNetwork, title: n.Name, sub: joinDot(sub),
		badge: func() { statusBadge(c, n) },
		actions: func() {
			if iconButton(c, "edit", iconPencil, L.EditConfiguration(), false) && !stale {
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
			btn.Disabled(busy || stale).MinWidth(104)
			if btn.Clicked() {
				a.do("connect-"+n.ID, localapi.Request{Type: req, Network: n.ID}, "", nil)
			}
		},
	}
}

// networkNotes are the lines at the top of a network's page: why it is not
// fully connected (with Try again once automatic restarts gave up), and
// saved changes that are not applied yet, with Apply now.
func (a *App) networkNotes(c *ui.Context, n localapi.NetworkStatus) {
	k := tok(c)
	if d := stateDetail(n); d != "" {
		ui.Row(c.Key("detail")).FillWidth().Gap(8).AlignItems(ui.Center).Padding(0, 2).Children(func() {
			ui.Text(c, d).TextColor(stateColor(k, n.State)).Grow(1).MinWidth(0)
			if n.State == localapi.StateFailed && n.Recovery == health.RecoveryExhausted {
				if ui.Button(c, L.ActionRetry()).Clicked() {
					a.do("retry-"+n.ID, localapi.Request{Type: localapi.RequestRetry, Network: n.ID}, "", nil)
				}
			}
		})
	}
	if n.Saved != "" && n.Applied != "" && n.Saved != n.Applied {
		callout(c, "pending", c.Theme().Accent, iconInfo, L.HeaderPendingChanges(), func() {
			if ui.Button(c, L.ActionApplyNow()).Clicked() {
				a.do("apply-"+n.ID, localapi.Request{Type: localapi.RequestApply, Network: n.ID}, L.ToastApplied(), nil)
			}
		})
	}
}

func joinDot(parts []string) string {
	return strings.Join(parts, " · ")
}

// centered is what a pane shows in place of its content: a round icon, a
// title, a sentence and actions, centred near its top.
func centered(c *ui.Context, key string, icon *ui.SVG, title, about string, actions func()) {
	t := c.Theme()
	ui.Column(c.Key("centered-"+key)).Grow(1).FillWidth().AlignItems(ui.Center).Padding(emptyTop, 24, 24, 24).Gap(8).Children(func() {
		ui.Box(c).Size(emptyMark, emptyMark).Radius(emptyMark/2).Center().Background(t.Accent.Alpha(0.12)).Margin(0, 0, 10, 0).Children(func() {
			ui.Icon(c, icon).Size(emptyMark/2.3, emptyMark/2.3).TextColor(t.Accent)
		})
		ui.Text(c, title).FontSize(sizeTitle).Bold().TextAlign(ui.Center)
		if about != "" {
			ui.Text(c, about).TextColor(t.TextMuted).TextAlign(ui.Center).MaxWidth(emptyW)
		}
		if actions != nil {
			ui.Row(c).Gap(10).Margin(14, 0, 0, 0).Children(actions)
		}
	})
}

// emptyState invites the first network.
func (a *App) emptyState(c *ui.Context) {
	centered(c, "empty", iconNetwork, L.EmptyTitle(), L.EmptyBody(), func() {
		if ui.PrimaryButton(c, L.ActionAddNetwork()).Clicked() {
			a.openCreate()
		}
		if ui.Button(c, L.ActionImport()).Clicked() {
			a.openCreate()
			a.importNetwork()
		}
	})
}

// netSummary is what a disconnected network's page shows of its saved
// configuration: never its secret. rev is the saved revision it was read
// from.
type netSummary struct {
	rev              string
	peers, listeners []string
	dhcp             bool
	ipv4             string
	failed           bool
}

// summaryOf reads a configuration's summary.
// Returns: failed set when the document cannot be read as a form.
func summaryOf(text, rev string) netSummary {
	v, err := configdoc.Extract(text)
	if err != nil {
		return netSummary{rev: rev, failed: true}
	}
	s := netSummary{rev: rev, dhcp: v.DHCP != nil && *v.DHCP, ipv4: str(v.IPv4)}
	if v.Peers != nil {
		for _, p := range *v.Peers {
			s.peers = append(s.peers, p.URI)
		}
	}
	if v.Listeners != nil {
		s.listeners = *v.Listeners
	}
	return s
}

// loadSummary reads a network's saved configuration for its summary, once
// per saved revision.
// Side effects: a daemon request in the background; the summary lands in
// a.summaries.
func (a *App) loadSummary(id, rev string) {
	c := a.client()
	key := "summary-" + id
	a.mu.Lock()
	if c == nil || a.busy[key] {
		a.mu.Unlock()
		return
	}
	a.busy[key] = true
	a.mu.Unlock()
	go func() {
		doc, err := c.Do(a.ctx, localapi.Request{Type: localapi.RequestConfigGet, Network: id})
		a.update(func() {
			delete(a.busy, key)
			if err != nil || doc.Config == nil {
				a.summaries[id] = netSummary{rev: rev, failed: true}
				return
			}
			a.summaries[id] = summaryOf(doc.Config.Text, rev)
		})
	}()
}

// stoppedView is a disconnected network's page: that it is not connected,
// the saved changes not applied yet, and a summary of its configuration
// with Edit configuration.
func (a *App) stoppedView(c *ui.Context, n localapi.NetworkStatus) {
	t := c.Theme()
	a.mu.Lock()
	s, have := a.summaries[n.ID]
	mode, bg := a.prefs.Mode, a.settings.Background
	a.mu.Unlock()
	if !have || s.rev != n.Saved {
		a.loadSummary(n.ID, n.Saved)
	}
	pageColumn(c, "stopped", pageColumnW, func() {
		ui.Row(c.Key("stopped-head")).FillWidth().Gap(16).AlignItems(ui.Center).Margin(14, 0, 0, 0).Children(func() {
			ui.Box(c).Size(48, 48).Shrink(0).Radius(24).Center().Background(tileBackground(t)).Children(func() {
				ui.Icon(c, iconNetwork).Size(22, 22).TextColor(t.TextMuted)
			})
			ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
				ui.Text(c, L.StoppedTitle(n.Name)).FontSize(sizePageTitle).Bold()
				msg := L.StoppedBody()
				if n.Run && n.State == localapi.StateStopped {
					msg = L.StoppedWaiting()
				}
				ui.Text(c, msg).TextColor(t.TextMuted)
			})
		})
		ui.Column(c.Key("stopped-notes")).FillWidth().Gap(8).Margin(14, 0, 0, 0).Children(func() { a.networkNotes(c, n) })
		var r rows
		r.section(c, "summary", L.SummaryTitle(), func() {
			if ui.Button(c.Key("summary-edit"), L.EditConfiguration()).Clicked() {
				a.openEditor(n.ID)
			}
		}, func(r *rows) {
			r.row(c, "sum-name", iconTag, L.FieldNetworkName(), L.FieldSecretHint(), func() {
				ui.Text(c, orDash(n.NetworkName)).Selectable()
			})
			if !have || s.failed {
				r.row(c, "sum-more", iconInfo, L.SummaryUnavailable(), "", nil)
				return
			}
			r.row(c, "sum-peers", iconServer, L.FieldPeers(), L.SummaryPeersAbout(), func() {
				ui.Text(c, orDash(strings.Join(s.peers, "\n"))).FontSize(sizeSmall).Selectable().MaxWidth(rowControlW + 80).TextAlign(ui.End)
			})
			addr := L.SummaryAddressAuto()
			if !s.dhcp {
				addr = orDash(s.ipv4)
			}
			r.row(c, "sum-addr", iconPin, L.SummaryAddress(), L.SummaryAddressAbout(), func() { ui.Text(c, addr).Selectable() })
			device, about := L.StateOn(), L.SummaryDeviceOn()
			if n.NoTUN {
				device, about = L.StateOff(), L.FieldNoTunHint()
			}
			r.row(c, "sum-device", iconDevice, L.SummaryDevice(), about, func() { ui.Text(c, device) })
			r.row(c, "sum-listen", iconEar, L.FieldListeners(), L.SummaryListenersAbout(), func() {
				ui.Text(c, orDash(strings.Join(s.listeners, "\n"))).FontSize(sizeSmall).Selectable().MaxWidth(rowControlW + 80).TextAlign(ui.End)
			})
		})
		if mode == ModeHelper && bg {
			ui.Text(c, L.StoppedBackgroundNote()).FontSize(sizeSmall).TextColor(t.TextMuted).Margin(12, 4, 0, 4)
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
	ui.Row(c.Key("net-tabs")).FillWidth().Gap(gap).AlignItems(ui.Center).Margin(4, 0, 0, 0).Children(func() {
		ui.Segmented(c.Key("net-tab"), &a.netTab, L.TabPeers(), L.TabEvents())
		ui.Box(c).Grow(1)
		if a.netTab == 0 {
			ui.Text(c, L.PeersHint()).FontSize(sizeSmall).TextColor(c.Theme().TextMuted)
		}
	})
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
	a.peerList(c, n.MagicDNSZone, detail)
}

// peerList shows the nodes of the network (local node first).
// Inputs: zone is the network's MagicDNS zone, "" when MagicDNS is off.
// Side effects: a row's context menu and copy button write to the clipboard
// and show a toast; clicking a peer with connections expands it.
func (a *App) peerList(c *ui.Context, zone string, d *localapi.NetworkDetail) {
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
			// Keyed by identity, not position, so expansion survives
			// reordering.
			key := d.Status.ID + "/" + p.Hostname + "/" + p.IPv4
			a.mu.Lock()
			open := a.expanded[key]
			a.mu.Unlock()
			name := friendlyName(p.Hostname)
			ip, dns := shortIP(p.IPv4), magicDNSName(p.Hostname, zone)
			copyText := func(s string) {
				c.WriteClipboard(s)
				a.update(func() { a.setToast(L.Copied(s), false) })
			}
			row := ui.ButtonBase(c.Key("peer-" + key)).FillWidth().Label(name)
			if name != p.Hostname {
				row.Tooltip(p.Hostname)
			}
			if row.Clicked() && !p.Local && len(p.Conns) > 0 {
				a.update(func() { a.expanded[key] = !open })
			}
			row.ContextMenu(func(m *ui.Menu) {
				if m.Item(L.PeerCopyIP()).Disabled(ip == "").Chosen() {
					copyText(ip)
				}
				if dns != "" && m.Item(L.PeerCopyDNS()).Chosen() {
					copyText(dns)
				}
				if m.Item(L.PeerCopyHostname()).Chosen() {
					copyText(p.Hostname)
				}
			})
			showCopy := row.Hovered() || row.FocusWithin()
			row.Children(func() {
				ui.Row(c).FillWidth().Padding(10, 12).Gap(12).AlignItems(ui.Center).Children(func() {
					col := k.peer
					switch {
					case p.Local:
						col = k.local
					case p.PublicServer:
						col = k.server
					}
					ui.Box(c).Width(avatarSize).Height(avatarSize).Radius(avatarSize / 2).Background(col.Alpha(0.14)).Center().Children(func() {
						switch logo := vendorLogo(p.Hostname); {
						case p.Local:
							ui.Text(c, "●").TextColor(col).Bold()
						case logo != nil:
							w, h := logo.Size()
							lw := logoSize
							if h > w {
								lw = logoSize * w / h
							}
							ui.Icon(c, logo).Width(lw).TextColor(col)
						default:
							ui.Text(c, avatarInitial(name)).TextColor(col).Bold()
						}
					})
					ui.Column(c).Grow(1).Gap(2).Children(func() {
						title := name
						if p.Local {
							title = name + "  (" + L.PeerThisDevice() + ")"
						}
						ui.Text(c, title).Bold().Ellipsis("…").NoWrap()
						sub := nonEmpty(ip, p.Tunnel, natLabel(p.NATType))
						if p.PublicServer {
							sub = append([]string{L.PeerPublicServer()}, sub...)
						}
						ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
							ui.Text(c, joinDot(sub)).FontSize(sizeSmall).TextColor(t.TextMuted).NoWrap()
							if ip == "" {
								return
							}
							b := ui.ButtonBase(c.Key("copy-" + key)).Label(L.PeerCopyIP()).Tooltip(L.PeerCopyIP()).Padding(2).Radius(4)
							if !showCopy {
								b.Opacity(0)
							}
							if b.Clicked() {
								copyText(ip)
							}
							b.Children(func() { ui.Icon(c, iconCopy).Width(copyIconSz).TextColor(t.TextMuted) })
						})
						if len(p.Subnets) > 0 {
							ui.Text(c, L.PeerSubnets(strings.Join(p.Subnets, ", "))).FontSize(sizeSmall).TextColor(t.TextMuted).Selectable()
						}
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
			})
			if open {
				connDetails(c, p)
			}
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

// connDetails lists a peer's tunnel connections (shown when its row is
// expanded).
func connDetails(c *ui.Context, p localapi.PeerInfo) {
	t := c.Theme()
	k := tok(c)
	ui.Column(c).PaddingX(60).PaddingY(4).Gap(6).FillWidth().Children(func() {
		ui.Text(c, L.PeerConnections(len(p.Conns))).FontSize(sizeSmall).Bold().TextColor(t.TextMuted)
		for i, cn := range p.Conns {
			ui.Row(c.Key(fmt.Sprint("conn-", i))).Gap(10).AlignItems(ui.Center).Children(func() {
				capsule(c, cn.Tunnel, k.peer)
				ui.Text(c, cn.Local+"  →  "+cn.Remote).FontSize(sizeSmall).Selectable().Grow(1).Ellipsis("…").NoWrap()
				ui.Text(c, "↓ "+formatBytes(float64(cn.RxBytes))+"   ↑ "+formatBytes(float64(cn.TxBytes))).FontSize(sizeSmall).TextColor(t.TextMuted).NoWrap()
				ui.Text(c, fmt.Sprintf("%.0f%%", cn.LossRate*100)).FontSize(sizeSmall).TextColor(lossColor(k, cn.LossRate)).NoWrap()
				ui.Text(c, formatLatency(cn.LatencyMS)).FontSize(sizeSmall).TextColor(latencyColor(k, cn.LatencyMS)).NoWrap().MinWidth(64).TextAlign(ui.End)
			})
		}
	})
}

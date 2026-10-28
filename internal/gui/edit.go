package gui

import (
	"errors"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/configdoc"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/localapi"
	L "github.com/YuanZhe-99/Easytier-MyGo/locales"
)

// configEditor edits one network's document as a form or as raw TOML.
// The form is a projection: saving rewrites only the changed spans
// (configdoc.Changes/ApplyValues), so comments and unknown fields survive.
type configEditor struct {
	id       string
	loaded   bool
	baseText string
	baseRev  string
	base     configdoc.Values
	tab      int // 0 form, 1 TOML
	raw      editor.State
	err      string
	advanced bool

	// mini and release describe the core the network runs on (EasyTier
	// mini; its EasyTier release, "" when unknown). miniIgnored and
	// conflicts are what that core leaves out of or refuses in the saved
	// document (configdoc feature IDs).
	mini                   bool
	release                string
	miniIgnored, conflicts []string

	// Form fields.
	name, netName, secret, ipv4, hostname string
	instanceID                            string
	dhcp, noTun, showSecret               bool
	peers, listeners, proxies, exits      string
	mtu, devName                          string
	flags                                 map[string]bool
}

// advancedFlags are the flags the form offers, in order, with labels.
var advancedFlags = []string{
	"latency_first", "enable_exit_node", "accept_dns", "disable_p2p", "p2p_only", "lazy_p2p",
	"enable_kcp_proxy", "enable_quic_proxy", "private_mode", "enable_encryption", "enable_ipv6", "multi_thread",
	"prefer_peer_relay", // EasyTier 2.7+: offered on those cores, or when the document sets it
}

func flagLabel(key string) (string, string) {
	switch key {
	case "latency_first":
		return L.FlagLatencyFirst(), L.FlagLatencyFirstHint()
	case "enable_exit_node":
		return L.FlagExitNode(), L.FlagExitNodeHint()
	case "accept_dns":
		return L.FlagMagicDNS(), L.FlagMagicDNSHint()
	case "disable_p2p":
		return L.FlagDisableP2P(), L.FlagDisableP2PHint()
	case "p2p_only":
		return L.FlagP2POnly(), L.FlagP2POnlyHint()
	case "lazy_p2p":
		return L.FlagLazyP2P(), L.FlagLazyP2PHint()
	case "enable_kcp_proxy":
		return L.FlagKCP(), L.FlagKCPHint()
	case "enable_quic_proxy":
		return L.FlagQUIC(), L.FlagQUICHint()
	case "private_mode":
		return L.FlagPrivate(), L.FlagPrivateHint()
	case "enable_encryption":
		return L.FlagEncryption(), L.FlagEncryptionHint()
	case "enable_ipv6":
		return L.FlagIPv6(), L.FlagIPv6Hint()
	case "multi_thread":
		return L.FlagMultiThread(), L.FlagMultiThreadHint()
	case "prefer_peer_relay":
		return L.FlagPreferRelay(), L.FlagPreferRelayHint()
	}
	return key, ""
}

// openEditor loads a network's document and shows the editor.
func (a *App) openEditor(id string) {
	c := a.client()
	if c == nil {
		a.showToast(msgNotConnected(), true)
		return
	}
	a.update(func() {
		a.page, a.selected = pageEdit, id
		a.editor = &configEditor{id: id}
	})
	go func() {
		doc, err := c.Do(a.ctx, localapi.Request{Type: localapi.RequestConfigGet, Network: id})
		a.update(func() {
			e := a.editor
			if e == nil || e.id != id {
				return
			}
			if err != nil {
				e.err = describeError(err)
				return
			}
			n, _ := a.networkByID(id)
			e.name, e.mini, e.release = n.Name, n.Mini, n.CoreRelease
			e.load(doc.Config.Text, doc.Config.Revision)
		})
	}()
}

// load fills the editor from a document.
func (e *configEditor) load(text, rev string) {
	e.baseText, e.baseRev, e.loaded, e.err = text, rev, true, ""
	b := editor.NewBuffer(text)
	b.SetLanguage(editor.LanguageFor("network.toml"))
	e.raw = editor.State{Buffer: b, Label: L.EditTomlLabel(), LineNumbers: editor.LineNumbersOn}
	e.miniIgnored = nil
	if e.mini {
		e.miniIgnored, _ = configdoc.MiniUnsupported(text)
	}
	e.conflicts, _ = configdoc.VersionConflicts(text, configdoc.AtLeast27(e.release))
	v, err := configdoc.Extract(text)
	if err != nil {
		// The form cannot represent this document; edit it as TOML.
		e.tab = 1
		e.err = L.EditFormUnavailable()
		return
	}
	e.base = v
	e.fromValues(v)
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func lines(p *[]string) string {
	if p == nil {
		return ""
	}
	return strings.Join(*p, "\n")
}

// fromValues copies a projection into the form fields.
func (e *configEditor) fromValues(v configdoc.Values) {
	e.netName, e.secret, e.ipv4, e.hostname = str(v.NetworkName), str(v.NetworkSecret), str(v.IPv4), str(v.Hostname)
	// An empty or missing instance_id gets a fresh UUID, shown in the form
	// and written by the next save.
	if e.instanceID = strings.TrimSpace(str(v.InstanceID)); e.instanceID == "" {
		e.instanceID = configdoc.NewUUID()
	}
	e.dhcp = v.DHCP != nil && *v.DHCP
	e.listeners, e.exits = lines(v.Listeners), lines(v.ExitNodes)
	var peers []string
	if v.Peers != nil {
		for _, p := range *v.Peers {
			peers = append(peers, p.URI)
		}
	}
	e.peers = strings.Join(peers, "\n")
	var proxies []string
	if v.ProxyNetworks != nil {
		for _, p := range *v.ProxyNetworks {
			proxies = append(proxies, p.CIDR)
		}
	}
	e.proxies = strings.Join(proxies, "\n")
	e.flags = configdoc.DefaultFlags()
	for k, b := range v.Flags {
		e.flags[k] = b
	}
	e.noTun = e.flags["no_tun"]
	e.mtu, e.devName = "", str(v.DevName)
	if v.MTU != nil {
		e.mtu = strconv.FormatUint(uint64(*v.MTU), 10)
	}
}

func splitLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// formText applies the form's changes to the loaded document.
func (e *configEditor) formText() (string, error) {
	cur := e.base
	set := func(dst **string, v string) { *dst = &v }
	set(&cur.NetworkName, e.netName)
	set(&cur.NetworkSecret, e.secret)
	set(&cur.Hostname, e.hostname)
	if strings.TrimSpace(e.instanceID) == "" {
		e.instanceID = configdoc.NewUUID()
	}
	set(&cur.InstanceID, strings.TrimSpace(e.instanceID))
	dhcp := e.dhcp
	cur.DHCP = &dhcp
	if !e.dhcp {
		set(&cur.IPv4, e.ipv4)
	}
	ls, ex := splitLines(e.listeners), splitLines(e.exits)
	cur.Listeners, cur.ExitNodes = &ls, &ex
	var peers []configdoc.Peer
	for _, uri := range splitLines(e.peers) {
		p := configdoc.Peer{URI: uri}
		if e.base.Peers != nil { // keep a known peer's public key
			if i := slices.IndexFunc(*e.base.Peers, func(o configdoc.Peer) bool { return o.URI == uri }); i >= 0 {
				p = (*e.base.Peers)[i]
			}
		}
		peers = append(peers, p)
	}
	cur.Peers = &peers
	var proxies []configdoc.ProxyNetwork
	for _, cidr := range splitLines(e.proxies) {
		p := configdoc.ProxyNetwork{CIDR: cidr}
		if e.base.ProxyNetworks != nil {
			if i := slices.IndexFunc(*e.base.ProxyNetworks, func(o configdoc.ProxyNetwork) bool { return o.CIDR == cidr }); i >= 0 {
				p = (*e.base.ProxyNetworks)[i]
			}
		}
		proxies = append(proxies, p)
	}
	cur.ProxyNetworks = &proxies
	flags := map[string]bool{}
	for k, b := range e.flags {
		flags[k] = b
	}
	flags["no_tun"] = e.noTun
	cur.Flags = flags
	if e.devName != "" || e.base.DevName != nil {
		set(&cur.DevName, e.devName)
	}
	if e.mtu != "" {
		v, err := strconv.ParseUint(e.mtu, 10, 32)
		if err != nil {
			return "", errors.New(L.EditBadMTU())
		}
		m := uint32(v)
		cur.MTU = &m
	}
	return configdoc.ApplyValues(e.baseText, configdoc.Changes(e.base, cur))
}

// text is what saving writes.
func (e *configEditor) text() (string, error) {
	if e.tab == 1 {
		return e.raw.Buffer.String(), nil
	}
	return e.formText()
}

// switchTab moves between the form and TOML, carrying the edits along.
func (e *configEditor) switchTab(to int) {
	if to == e.tab {
		return
	}
	if to == 1 {
		text, err := e.formText()
		if err != nil {
			e.err = err.Error()
			return
		}
		e.raw.Buffer.Set(text)
		e.tab, e.err = 1, ""
		return
	}
	text := e.raw.Buffer.String()
	v, err := configdoc.Extract(text)
	if err != nil {
		e.err = L.EditFormUnavailable()
		return
	}
	e.baseText, e.base = text, v
	e.fromValues(v)
	e.tab, e.err = 0, ""
}

// editView is the editor in the network's pane (doc/en-us/gui.md,
// "Editing"): its header has Form | TOML, Cancel, Save and Save and apply;
// the form is rows like Settings', the TOML MyGo's editor.
func (a *App) editView(c *ui.Context) {
	t := c.Theme()
	a.mu.Lock()
	e := a.editor
	busy := e != nil && a.busy["save-"+e.id]
	var title string
	if e != nil {
		n, _ := a.networkByID(e.id)
		title = n.Name
	}
	a.mu.Unlock()
	if e == nil {
		return
	}
	head := paneHead{icon: iconPencil, title: title, sub: L.EditConfiguration(),
		badge: func() {
			if !e.loaded {
				return
			}
			tab := e.tab
			ui.Segmented(c.Key("edit-tab"), &tab, L.EditTabForm(), L.EditTabToml()).Label(L.EditConfiguration())
			if tab != e.tab {
				a.mu.Lock()
				e.switchTab(tab)
				a.mu.Unlock()
			}
		},
		actions: func() {
			if ui.Button(c.Key("edit-cancel"), L.ActionCancel()).Clicked() {
				a.update(func() { a.page, a.editor = pageNetwork, nil })
			}
			if ui.Button(c.Key("edit-save"), L.ActionSave()).Disabled(busy || !e.loaded).Clicked() {
				a.saveEditor(false)
			}
			if ui.PrimaryButton(c.Key("edit-apply"), L.ActionSaveApply()).Disabled(busy || !e.loaded).Clicked() {
				a.saveEditor(true)
			}
		},
	}
	a.pane(c, head, func() {
		if e.err != "" {
			ui.Text(c, e.err).FontSize(sizeSmall).TextColor(tok(c).bad).Padding(4, 24, 8, 24)
		}
		if !e.loaded {
			ui.Column(c).Grow(1).FillWidth().Center().Children(func() { ui.Spinner(c) })
			return
		}
		if e.tab == 1 {
			ui.Column(c.Key("edit-toml")).Grow(1).FillWidth().MinHeight(0).Padding(4, 24, 24, 24).Gap(10).Children(func() {
				ui.Text(c, L.EditTomlHint()).FontSize(sizeSmall).TextColor(t.TextMuted)
				editor.View(c, &e.raw).Grow(1).FillWidth().Border(1, t.Border).Radius(cardRadius)
			})
			return
		}
		pageColumn(c, "edit-form", pageColumnW, func() { a.formView(c, e) })
	})
}

// formView is the projection of the document as rows: the network, its
// address, its connections, routing, the advanced flags (folded), and
// Delete network last. Lists of addresses take a text area under their
// row.
func (a *App) formView(c *ui.Context, e *configEditor) {
	t := c.Theme()
	a.mu.Lock()
	busy := a.busy["save-"+e.id]
	a.mu.Unlock()
	var r rows
	field := func(key string, v *string, placeholder string) ui.Element {
		return ui.TextInput(c.Key(key), v).Placeholder(placeholder).Width(rowControlW)
	}
	area := func(key string, v *string, placeholder string) {
		ui.TextArea(c.Key(key), v).Placeholder(placeholder).FillWidth().MinHeight(64)
	}
	// On EasyTier mini, rows it does not run say so in the warning color.
	hint := func(about, onMini string) (string, ui.Color) {
		if e.mini {
			return onMini, tok(c).warn
		}
		return about, ui.Color{}
	}
	a.coreNotes(c, e)
	r.section(c, "f-basic", L.EditSectionBasic(), nil, func(r *rows) {
		r.row(c, "f-name", iconTag, L.FieldDisplayName(), L.EditNameAbout(), func() { field("f-name-in", &e.name, "") })
		r.row(c, "f-netname", iconNetwork, L.FieldNetworkName(), L.FieldSecretHint(), func() { field("f-netname-in", &e.netName, "") })
		r.row(c, "f-secret", iconKey, L.FieldNetworkSecret(), L.EditSecretAbout(), func() {
			in := ui.TextInput(c.Key("f-secret-in"), &e.secret).Width(rowControlW - paneButton - 4)
			if !e.showSecret {
				in.Password()
			}
			icon, label := iconEye, L.FieldShowSecret()
			if e.showSecret {
				icon, label = iconEyeOff, L.ActionHide()
			}
			if iconButton(c, "f-secret-show", icon, label, false) {
				e.showSecret = !e.showSecret
			}
		})
		r.row(c, "f-host", iconDevice, L.FieldHostname(), L.EditHostnameAbout(), func() {
			field("f-host-in", &e.hostname, L.FieldHostnamePlaceholder())
		})
		r.row(c, "f-instance", iconHash, L.FieldInstanceID(), L.FieldInstanceIDHint(), func() {
			field("f-instance-in", &e.instanceID, "").Width(rowControlW - paneButton - 4)
			if iconButton(c, "f-instance-new", iconRefresh, L.ActionNewInstanceID(), false) {
				e.instanceID = configdoc.NewUUID()
			}
		})
	})
	r.section(c, "f-address", L.EditSectionAddress(), nil, func(r *rows) {
		r.row(c, "f-dhcp", iconPin, L.FieldDHCP(), L.EditDHCPAbout(), func() {
			ui.Switch(c.Key("f-dhcp-sw"), &e.dhcp).Label(L.FieldDHCP())
		})
		r.row(c, "f-ipv4", iconPin, L.FieldIPv4(), L.EditIPv4About(), func() {
			field("f-ipv4-in", &e.ipv4, "10.144.144.1/24").Disabled(e.dhcp)
		})
	})
	r.section(c, "f-connect", L.EditSectionConnect(), nil, func(r *rows) {
		about, col := hint(L.FieldPeersHint(), L.EditMiniSchemes())
		r.rowIn(c, "f-peers", iconServer, L.FieldPeers(), about, col, nil)
		r.block(c, "f-peers-area", func() { area("f-peers-in", &e.peers, "tcp://public.easytier.cn:11010") })
		about, col = hint(L.FieldListenersHint(), L.EditMiniSchemes())
		r.rowIn(c, "f-listeners", iconEar, L.FieldListeners(), about, col, nil)
		r.block(c, "f-listeners-area", func() { area("f-listeners-in", &e.listeners, "tcp://0.0.0.0:11010\nudp://0.0.0.0:11010") })
		device := !e.noTun
		r.row(c, "f-device", iconDevice, L.SummaryDevice(), L.EditDeviceAbout(), func() {
			if ui.Switch(c.Key("f-device-sw"), &device).Label(L.SummaryDevice()).Changed() {
				e.noTun = !device
			}
		})
	})
	r.section(c, "f-routing", L.EditSectionRouting(), nil, func(r *rows) {
		about, col := hint(L.FieldProxiesHint(), L.EditMiniNotRun())
		r.rowIn(c, "f-proxies", iconRoute, L.FieldProxies(), about, col, nil)
		r.block(c, "f-proxies-area", func() { area("f-proxies-in", &e.proxies, "192.168.1.0/24") })
		about, col = hint(L.FieldExitNodesHint(), L.EditMiniNotRun())
		r.rowIn(c, "f-exits", iconExit, L.FieldExitNodes(), about, col, nil)
		r.block(c, "f-exits-area", func() { area("f-exits-in", &e.exits, "10.144.144.2") })
	})
	r.section(c, "f-advanced", L.EditSectionAdvanced(), func() {
		label := L.ActionShow()
		if e.advanced {
			label = L.ActionHide()
		}
		if ui.Button(c.Key("adv-toggle"), label).Clicked() {
			e.advanced = !e.advanced
		}
	}, func(r *rows) {
		if !e.advanced {
			r.row(c, "f-adv-folded", iconFlag, L.EditAdvancedHint(), "", nil)
			return
		}
		for _, key := range advancedFlags {
			if _, set := e.base.Flags[key]; key == "prefer_peer_relay" && !set && !configdoc.AtLeast27(e.release) {
				continue // the 2.6 core does not have it
			}
			on := e.flags[key]
			label, h := flagLabel(key)
			col := ui.Color{}
			if miniFlags[key] {
				h, col = hint(h, L.EditMiniNotRun())
			}
			r.rowIn(c, "flag-"+key, iconFlag, label, h, col, func() {
				ui.Switch(c.Key("flag-sw-"+key), &on).Label(label)
			})
			e.flags[key] = on
		}
		r.row(c, "f-dev", iconDevice, L.FieldDevName(), "", func() { field("f-dev-in", &e.devName, "et0") })
		r.row(c, "f-mtu", iconRuler, L.FieldMTU(), "", func() { field("f-mtu-in", &e.mtu, "1380") })
	})
	a.coreSection(c, &r, e)
	r.section(c, "f-delete", L.EditSectionDelete(), nil, func(r *rows) {
		r.rowIn(c, "f-delete-row", iconTrash, L.ActionDeleteNetwork(), L.EditDeleteAbout(), t.TextMuted, func() {
			if ui.Button(c.Key("f-delete-btn"), L.ActionDeleteNetwork()).Disabled(busy).Clicked() {
				a.confirmDelete(e.id, e.name)
			}
		})
	})
}

// saveEditor saves the document (conflict-checked), renames when needed
// and, when asked, applies it.
func (a *App) saveEditor(apply bool) {
	a.mu.Lock()
	e := a.editor
	text, err := e.text()
	id, rev, name := e.id, e.baseRev, strings.TrimSpace(e.name)
	n, _ := a.networkByID(id)
	a.mu.Unlock()
	if err != nil {
		a.update(func() { e.err = err.Error() })
		return
	}
	c := a.client()
	if c == nil {
		a.showToast(msgNotConnected(), true)
		return
	}
	a.update(func() { a.busy["save-"+id] = true })
	go func() {
		defer a.update(func() { delete(a.busy, "save-"+id) })
		if name != "" && name != n.Name {
			if _, err := c.Do(a.ctx, localapi.Request{Type: localapi.RequestNetworkRename, Network: id, Name: name}); err != nil {
				a.update(func() { e.err = describeError(err) })
				return
			}
		}
		resp, err := c.Do(a.ctx, localapi.Request{Type: localapi.RequestConfigSave, Network: id, Text: text, Expected: rev})
		if err != nil {
			a.update(func() {
				if localapi.CodeOf(err) == localapi.CodeConflict {
					e.err = L.EditConflict()
				} else {
					e.err = describeError(err)
				}
			})
			return
		}
		a.update(func() { e.load(text, resp.Config.Revision) })
		if apply {
			if _, err := c.Do(a.ctx, localapi.Request{Type: localapi.RequestApply, Network: id}); err != nil {
				a.update(func() { e.err = describeError(err) })
				return
			}
		}
		a.update(func() {
			a.page, a.editor = pageNetwork, nil
			if apply {
				a.setToast(L.ToastApplied(), false)
			} else {
				a.setToast(L.ToastSaved(), false)
			}
		})
	}()
}

// createDialog adds a network: from a few fields, or by importing a file.
type createDialog struct {
	name, netName, secret, peer string
	noTun                       bool
	err                         string
}

func (a *App) openCreate() {
	a.update(func() { a.createDlg = &createDialog{} })
}

// createDialogView is Add network (doc/en-us/gui.md, "Editing"): a dialog
// over the dimmed window, its fields as rows, then Import file…, Cancel
// and Create. The window's buttons stay over the dimming on Windows.
func (a *App) createDialogView(c *ui.Context) {
	a.mu.Lock()
	d := a.createDlg
	busy := a.busy["create"]
	a.mu.Unlock()
	if d == nil {
		return
	}
	t := c.Theme()
	open := true
	defer func() {
		if !open {
			a.update(func() { a.createDlg = nil })
		}
	}()
	ui.DialogBase(c, &open, func(back, panel ui.Element) {
		back.Background(sheetScrim).Padding(dialogTop, sheetX, sheetBottom, sheetX).AlignItems(ui.Center).Justify(ui.Start)
		panel.Radius(paneRadius).Background(t.Background).Shadow(0, 10, 30, 0, sheetShadow).Clip()
		ui.Column(c.Key("create")).Width(dialogW).Children(func() {
			ui.Row(c.Key("create-head")).Height(sheetHeaderH).Shrink(0).Padding(0, 12, 0, 20).AlignItems(ui.Center).Children(func() {
				ui.Text(c, L.CreateTitle()).FontSize(sizeHeading).Bold().Grow(1)
				if iconButton(c, "create-close", iconClose, L.ActionClose(), false) {
					open = false
				}
			})
			ui.Divider(c)
			ui.Column(c.Key("create-body")).FillWidth().Padding(16, 20, 20, 20).Children(func() {
				ui.Text(c, L.CreateBody()).TextColor(t.TextMuted)
				var r rows
				field := func(key string, v *string, placeholder string) ui.Element {
					return ui.TextInput(c.Key(key), v).Placeholder(placeholder).Width(rowControlW - 20)
				}
				r.section(c, "create-fields", L.EditSectionBasic(), nil, func(r *rows) {
					r.row(c, "c-name", nil, L.FieldDisplayName(), L.EditNameAbout(), func() {
						field("c-name-in", &d.name, L.CreateNamePlaceholder()).AutoFocus()
					})
					r.row(c, "c-net", nil, L.FieldNetworkName(), L.FieldSecretHint(), func() { field("c-net-in", &d.netName, "") })
					r.row(c, "c-secret", nil, L.FieldNetworkSecret(), L.EditSecretAbout(), func() { field("c-secret-in", &d.secret, "").Password() })
					r.row(c, "c-peer", nil, L.CreatePeer(), L.CreatePeerAbout(), func() {
						field("c-peer-in", &d.peer, "tcp://public.easytier.cn:11010")
					})
					device := !d.noTun
					r.row(c, "c-device", nil, L.SummaryDevice(), L.EditDeviceAbout(), func() {
						if ui.Switch(c.Key("c-device-sw"), &device).Label(L.SummaryDevice()).Changed() {
							d.noTun = !device
						}
					})
				})
				if d.err != "" {
					ui.Text(c, d.err).FontSize(sizeSmall).TextColor(tok(c).bad).Margin(10, 4, 0, 4)
				}
				ui.Row(c.Key("create-foot")).FillWidth().Gap(8).AlignItems(ui.Center).Margin(18, 0, 0, 0).Children(func() {
					if ui.Button(c.Key("c-import"), L.ActionImport()).Disabled(busy).Clicked() {
						a.importNetwork()
					}
					ui.Spacer(c)
					if ui.Button(c.Key("c-cancel"), L.ActionCancel()).Clicked() {
						open = false
					}
					if ui.PrimaryButton(c.Key("c-create"), L.ActionCreate()).Disabled(busy).MinWidth(88).Clicked() {
						a.createNetwork()
					}
				})
			})
		})
	})
	if ownWindowButtons {
		ui.Overlay(c, func() {
			ui.Row(c.Key("create-win")).Absolute().Top(paneMargin + (paneHeaderH-winButtonH)/2).Right(2 * paneMargin).Gap(8).Children(func() {
				a.windowButtons(c, "create-win", true)
			})
		})
	}
}

// createNetwork builds a document from the template and the fields.
func (a *App) createNetwork() {
	a.mu.Lock()
	d := *a.createDlg
	a.mu.Unlock()
	if strings.TrimSpace(d.name) == "" || strings.TrimSpace(d.netName) == "" {
		a.update(func() { a.createDlg.err = L.CreateMissing() })
		return
	}
	id := configdoc.NewUUID()
	v := configdoc.Values{NetworkName: &d.netName, NetworkSecret: &d.secret, InstanceName: &d.name, InstanceID: &id}
	if d.peer != "" {
		v.Peers = &[]configdoc.Peer{{URI: strings.TrimSpace(d.peer)}}
	}
	if d.noTun {
		v.Flags = map[string]bool{"no_tun": true}
	}
	text, err := configdoc.ApplyValues(configdoc.Template(), v)
	if err != nil {
		a.update(func() { a.createDlg.err = err.Error() })
		return
	}
	a.submitCreate(d.name, text)
}

// importNetwork reads a TOML file chosen by the user.
func (a *App) importNetwork() {
	a.mu.Lock()
	w := a.window
	name := a.createDlg.name
	a.mu.Unlock()
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: w, Title: L.ImportTitle(),
			Filters: []mygo.FileFilter{{Name: "EasyTier TOML", Extensions: []string{"toml"}}}})
		if err != nil || len(paths) == 0 {
			return
		}
		data, err := os.ReadFile(paths[0])
		if err != nil {
			a.update(func() { a.createDlg.err = err.Error() })
			return
		}
		if name == "" {
			name = strings.TrimSuffix(paths[0][strings.LastIndexAny(paths[0], `/\`)+1:], ".toml")
		}
		a.submitCreate(name, string(data))
	}()
}

func (a *App) submitCreate(name, text string) {
	a.do("create", localapi.Request{Type: localapi.RequestNetworkCreate, Name: name, Text: text}, L.ToastCreated(), func(r *localapi.Response) {
		a.createDlg = nil
		if len(r.Networks) > 0 {
			a.selected, a.page = r.Networks[len(r.Networks)-1].ID, pageNetwork
		}
	})
}

// confirmDelete asks before deleting a network and its configuration.
func (a *App) confirmDelete(id, name string) {
	a.mu.Lock()
	w := a.window
	a.mu.Unlock()
	go func() {
		res, err := mygo.Dialog.Message(mygo.MessageOptions{Parent: w, Type: mygo.MessageWarning,
			Message: L.DeleteConfirmTitle(name), Detail: L.DeleteConfirmBody(),
			Buttons: []string{L.ActionDeleteNetwork(), L.ActionCancel()}, DefaultButton: 1, CancelButton: 1})
		if err != nil || res.Button != 0 {
			return
		}
		a.do("save-"+id, localapi.Request{Type: localapi.RequestNetworkDelete, Network: id}, L.ToastDeleted(), func(*localapi.Response) {
			a.page, a.editor, a.selected = pageNetwork, nil, ""
		})
	}()
}

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

	// Form fields.
	name, netName, secret, ipv4, hostname string
	dhcp, noTun, showSecret                bool
	peers, listeners, proxies, exits      string
	mtu, devName                          string
	flags                                 map[string]bool
}

// advancedFlags are the flags the form offers, in order, with labels.
var advancedFlags = []string{
	"latency_first", "enable_exit_node", "accept_dns", "disable_p2p", "p2p_only", "lazy_p2p",
	"enable_kcp_proxy", "enable_quic_proxy", "private_mode", "enable_encryption", "enable_ipv6", "multi_thread",
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
			e.name = n.Name
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

// editView is the editor page.
func (a *App) editView(c *ui.Context) {
	t := c.Theme()
	a.mu.Lock()
	e := a.editor
	busy := e != nil && a.busy["save-"+e.id]
	a.mu.Unlock()
	if e == nil {
		return
	}
	ui.Column(c).Grow(1).Fill().Padding(24).Gap(16).Children(func() {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Text(c, L.EditTitle(e.name)).FontSize(sizeTitle).Bold().Grow(1).Ellipsis("…").NoWrap()
			if ui.Button(c, L.ActionCancel()).Clicked() {
				a.update(func() { a.page, a.editor = pageNetwork, nil })
			}
			if ui.Button(c, L.ActionSave()).Disabled(busy || !e.loaded).Clicked() {
				a.saveEditor(false)
			}
			if ui.PrimaryButton(c, L.ActionSaveApply()).Disabled(busy || !e.loaded).Clicked() {
				a.saveEditor(true)
			}
		})
		if e.err != "" {
			ui.Text(c, e.err).FontSize(sizeSmall).TextColor(tok(c).bad)
		}
		if !e.loaded {
			ui.Spinner(c)
			return
		}
		tab := e.tab
		ui.Segmented(c.Key("edit-tab"), &tab, L.EditTabForm(), L.EditTabToml()).AlignSelf(ui.Start)
		if tab != e.tab {
			a.mu.Lock()
			e.switchTab(tab)
			a.mu.Unlock()
		}
		if e.tab == 1 {
			ui.Text(c, L.EditTomlHint()).FontSize(sizeSmall).TextColor(t.TextMuted)
			editor.View(c, &e.raw).Grow(1).FillWidth().Border(1, t.Border).Radius(8)
			return
		}
		ui.Scroll(c.Key("edit-form")).Grow(1).Gap(16).Children(func() { a.formView(c, e) })
	})
}

// formView is the projection of the document as fields.
func (a *App) formView(c *ui.Context, e *configEditor) {
	t := c.Theme()
	hint := func(s string) { ui.Text(c, s).FontSize(sizeSmall).TextColor(t.TextMuted) }
	section := func(title string, fn func()) {
		card(c).Children(func() {
			ui.Text(c, title).FontSize(sizeHeading).Bold()
			fn()
		})
	}
	section(L.EditSectionBasic(), func() {
		ui.Field(c, L.FieldDisplayName(), func() { ui.TextInput(c.Key("f-name"), &e.name).FillWidth() })
		ui.Field(c, L.FieldNetworkName(), func() { ui.TextInput(c.Key("f-netname"), &e.netName).FillWidth() })
		ui.Field(c, L.FieldNetworkSecret(), func() {
			ui.Row(c).Gap(8).Children(func() {
				in := ui.TextInput(c.Key("f-secret"), &e.secret).Grow(1)
				if !e.showSecret {
					in.Password()
				}
				ui.Checkbox(c.Key("f-show"), &e.showSecret, L.FieldShowSecret())
			})
		})
		hint(L.FieldSecretHint())
		ui.Field(c, L.FieldHostname(), func() { ui.TextInput(c.Key("f-host"), &e.hostname).Placeholder(L.FieldHostnamePlaceholder()).FillWidth() })
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Switch(c.Key("f-dhcp"), &e.dhcp).Label(L.FieldDHCP())
			ui.Text(c, L.FieldDHCP())
		})
		if !e.dhcp {
			ui.Field(c, L.FieldIPv4(), func() { ui.TextInput(c.Key("f-ipv4"), &e.ipv4).Placeholder("10.144.144.1/24").FillWidth() })
		}
	})
	section(L.EditSectionConnect(), func() {
		ui.Field(c, L.FieldPeers(), func() {
			ui.TextArea(c.Key("f-peers"), &e.peers).Placeholder("tcp://public.easytier.cn:11010").FillWidth().MinHeight(70)
		})
		hint(L.FieldPeersHint())
		ui.Field(c, L.FieldListeners(), func() {
			ui.TextArea(c.Key("f-listeners"), &e.listeners).Placeholder("tcp://0.0.0.0:11010\nudp://0.0.0.0:11010").FillWidth().MinHeight(70)
		})
		hint(L.FieldListenersHint())
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Switch(c.Key("f-notun"), &e.noTun).Label(L.FieldNoTun())
			ui.Column(c).Grow(1).Gap(2).Children(func() {
				ui.Text(c, L.FieldNoTun())
				hint(L.FieldNoTunHint())
			})
		})
	})
	section(L.EditSectionRouting(), func() {
		ui.Field(c, L.FieldProxies(), func() {
			ui.TextArea(c.Key("f-proxies"), &e.proxies).Placeholder("192.168.1.0/24").FillWidth().MinHeight(56)
		})
		hint(L.FieldProxiesHint())
		ui.Field(c, L.FieldExitNodes(), func() {
			ui.TextArea(c.Key("f-exits"), &e.exits).Placeholder("10.144.144.2").FillWidth().MinHeight(40)
		})
		hint(L.FieldExitNodesHint())
	})
	card(c).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			ui.Text(c, L.EditSectionAdvanced()).FontSize(sizeHeading).Bold().Grow(1)
			label := L.ActionShow()
			if e.advanced {
				label = L.ActionHide()
			}
			if ui.Button(c.Key("adv-toggle"), label).Clicked() {
				e.advanced = !e.advanced
			}
		})
		if !e.advanced {
			hint(L.EditAdvancedHint())
			return
		}
		for _, key := range advancedFlags {
			on := e.flags[key]
			label, h := flagLabel(key)
			ui.Row(c.Key("flag-"+key)).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Switch(c, &on).Label(label)
				ui.Column(c).Grow(1).Gap(2).Children(func() {
					ui.Text(c, label)
					hint(h)
				})
			})
			e.flags[key] = on
		}
		ui.Field(c, L.FieldDevName(), func() { ui.TextInput(c.Key("f-dev"), &e.devName).Placeholder("et0").FillWidth() })
		ui.Field(c, L.FieldMTU(), func() { ui.TextInput(c.Key("f-mtu"), &e.mtu).Placeholder("1380").FillWidth() })
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
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(460).Padding(24).Gap(12).Background(t.Background).Radius(cardRadius).Children(func() {
			ui.Text(c, L.CreateTitle()).FontSize(sizeTitle).Bold()
			ui.Text(c, L.CreateBody()).FontSize(sizeSmall).TextColor(t.TextMuted)
			ui.Field(c, L.FieldDisplayName(), func() { ui.TextInput(c.Key("c-name"), &d.name).Placeholder(L.CreateNamePlaceholder()).FillWidth().AutoFocus() })
			ui.Field(c, L.FieldNetworkName(), func() { ui.TextInput(c.Key("c-net"), &d.netName).FillWidth() })
			ui.Field(c, L.FieldNetworkSecret(), func() { ui.TextInput(c.Key("c-secret"), &d.secret).Password().FillWidth() })
			ui.Field(c, L.CreatePeer(), func() {
				ui.TextInput(c.Key("c-peer"), &d.peer).Placeholder("tcp://public.easytier.cn:11010").FillWidth()
			})
			ui.Checkbox(c.Key("c-notun"), &d.noTun, L.FieldNoTun())
			if d.err != "" {
				ui.Text(c, d.err).FontSize(sizeSmall).TextColor(tok(c).bad)
			}
			ui.Row(c).Gap(8).Children(func() {
				if ui.Button(c, L.ActionImport()).Disabled(busy).Clicked() {
					a.importNetwork()
				}
				ui.Spacer(c)
				if ui.Button(c, L.ActionCancel()).Clicked() {
					a.update(func() { a.createDlg = nil })
				}
				if ui.PrimaryButton(c, L.ActionCreate()).Disabled(busy).Clicked() {
					a.createNetwork()
				}
			})
		})
	})
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
	v := configdoc.Values{NetworkName: &d.netName, NetworkSecret: &d.secret, InstanceName: &d.name}
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

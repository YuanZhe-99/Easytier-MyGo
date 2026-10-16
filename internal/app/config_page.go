package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/configdoc"
)

// errInvalidNewName is shown when the inline "new config" name is empty.
var errInvalidNewName = errors.New("新配置名称不能为空")

// Row models for the block-oriented parts of the form.
type peerRow struct{ URI, Key string }
type proxyRow struct{ CIDR, Mapped, Allow string }
type forwardRow struct{ Proto, Bind, Dst string }

// flagFields is the covered set of flags.* booleans, with Chinese labels.
// It mirrors the official GUI's advanced switches (in TOML semantics).
var flagFields = []struct{ Key, Label, Hint string }{
	{"no_tun", "不创建 TUN", "仅作转发/测试，不接管本机流量"},
	{"latency_first", "延迟优先", "优先选择延迟更低的路径"},
	{"use_smoltcp", "用户态协议栈", "use_smoltcp：由 smoltcp 处理 TUN 流量"},
	{"enable_ipv6", "启用 IPv6", "enable_ipv6"},
	{"enable_encryption", "启用加密", "关闭后流量不加密（仅限测试）"},
	{"enable_kcp_proxy", "KCP 代理", "把 TCP 流量转换为 KCP"},
	{"disable_kcp_input", "禁止 KCP 入站", "不接受对端以 KCP 连入"},
	{"enable_quic_proxy", "QUIC 代理", "把 TCP 流量转换为 QUIC"},
	{"disable_quic_input", "禁止 QUIC 入站", "不接受对端以 QUIC 连入"},
	{"disable_p2p", "禁用 P2P", "只经中继转发"},
	{"p2p_only", "仅 P2P", "不中继其它节点的流量"},
	{"lazy_p2p", "延迟建立 P2P", "lazy_p2p"},
	{"need_p2p", "需要 P2P", "need_p2p"},
	{"bind_device", "绑定网卡", "套接字绑定到本机网卡"},
	{"multi_thread", "多线程", "多线程运行时"},
	{"enable_exit_node", "作为出口节点", "允许其它节点经本机出网"},
	{"relay_all_peer_rpc", "中继全部对端 RPC", "relay_all_peer_rpc"},
	{"proxy_forward_by_system", "系统转发子网代理", "用系统转发代替用户态转发"},
	{"accept_dns", "接受 Magic DNS", "accept_dns：使用 EasyTier 域名解析"},
	{"private_mode", "私有模式", "private_mode"},
	{"disable_tcp_hole_punching", "禁用 TCP 打洞", ""},
	{"disable_udp_hole_punching", "禁用 UDP 打洞", ""},
	{"disable_sym_hole_punching", "禁用对称 NAT 打洞", ""},
	{"enable_udp_broadcast_relay", "UDP 广播中继", ""},
	{"disable_upnp", "禁用 UPnP", ""},
	{"disable_relay_kcp", "禁止中继 KCP", "disable_relay_kcp"},
	{"enable_relay_foreign_network_kcp", "允许跨网络 KCP 中继", "enable_relay_foreign_network_kcp"},
	{"disable_relay_quic", "禁止中继 QUIC", "disable_relay_quic"},
	{"enable_relay_foreign_network_quic", "允许跨网络 QUIC 中继", "enable_relay_foreign_network_quic"},
	{"disable_relay_data", "禁止中继数据", "disable_relay_data"},
}

// configPage manages stored network configs: a form and the raw TOML
// editor over the same document. Saving and activating are separate
// actions — saving never restarts the core.
func (a *App) configPage(c *ui.Context) {
	s := a.configCopy()
	t := c.Theme()
	contentColumn(c).Children(func() {
		sectionTitle(c, "网络", "TOML 原文是事实来源；表单只改动对应字段，保存与应用是两步")
		// A read-only connection cannot fetch config text at all: say why
		// instead of showing an empty editor and failing buttons.
		if s.haveStatus && s.access == "read" {
			ui.Column(c).Background(t.Surface).Radius(8).Padding(12).Gap(4).FillWidth().Children(func() {
				ui.Text(c, "只读访问").Bold().TextColor(t.Warning)
				ui.Text(c, "你不在管理服务的授权组（easytier-mygo）中，因此只能查看状态，不能读取配置原文或更改网络。").
					FontSize(12).TextColor(t.TextMuted)
				ui.Text(c, "让管理员把你加入该组：sudo gpasswd -a $USER easytier-mygo（重新登录后生效）。").
					FontSize(12).TextColor(t.TextMuted)
			})
			return
		}
		switch {
		case !s.haveStatus:
			ui.Text(c, "尚未收到管理服务的状态。").TextColor(t.TextMuted)
			return
		case len(s.configs) == 0:
			ui.Text(c, "还没有配置。用左侧“创建”或“导入…”开始；配置保存在："+s.configDir).
				TextColor(t.TextMuted)
		}

		if s.pendingSelect != "" {
			for i, e := range s.configs {
				if e.Name == s.pendingSelect {
					a.configSel = i
					a.configSelectPending = ""
					break
				}
			}
		}
		a.ensureConfigLoaded(s)

		ui.Row(c).Fill().Grow(1).Gap(12).AlignItems(ui.Stretch).Children(func() {
			a.configListColumn(c, s)
			a.editorColumn(c, s)
		})
	})
}

// ensureConfigLoaded selects a config and loads it when nothing unsaved is
// in the way. It runs during rendering; the load itself is asynchronous.
func (a *App) ensureConfigLoaded(s configCopy) {
	if !a.pollingEnabled() || s.editorLoading || len(s.configs) == 0 {
		return
	}
	sel := s.selected
	if sel < 0 || sel >= len(s.configs) {
		sel = 0
		a.configSel = 0
	}
	name := s.configs[sel].Name
	if name != s.editorName && !a.hasUnsaved() {
		a.loadConfig(name)
	}
}

func (a *App) pollingEnabled() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.started
}

func (a *App) configListColumn(c *ui.Context, s configCopy) {
	t := c.Theme()
	ui.Column(c).Width(230).Gap(8).Children(func() {
		a.configList.Selected = &a.configSel
		if ui.List(c, &a.configList, len(s.configs), func(i int) {
			e := s.configs[i]
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				dot, dotColor := "○", t.TextMuted
				if e.Name == s.activeConfig {
					dot, dotColor = "●", t.Success
				}
				ui.Text(c, dot).FontSize(9).TextColor(dotColor)
				ui.Text(c, e.Name).SingleLine()
				ui.Spacer(c)
				ui.Text(c, revisionShort(e.Revision)).FontSize(10).TextColor(t.TextMuted)
			})
		}).Grow(1).Changed() {
			a.selectConfigRow(a.configSel, s)
		}
		if s.activeConfig == "" {
			ui.Text(c, "没有活动配置").FontSize(11).TextColor(t.TextMuted)
		} else {
			ui.Text(c, "当前："+s.activeConfig).FontSize(11).TextColor(t.TextMuted)
		}
		a.coreControl(c, a.snapshotCopy())
		ui.TextInput(c, &a.newConfigName).Placeholder("新配置名称").Width(220)
		ui.Row(c).Gap(8).Children(func() {
			ui.Button(c, "创建").OnClick(func() { a.createConfig(strings.TrimSpace(a.newConfigName)) })
			ui.Button(c, "导入…").OnClick(func() { a.importConfig() })
		})
	})
}

// selectConfigRow switches the editor to row i, asking first when there
// are unsaved changes.
func (a *App) selectConfigRow(i int, s configCopy) {
	if i < 0 || i >= len(s.configs) {
		return
	}
	name := s.configs[i].Name
	if name == a.editorName {
		return
	}
	if a.hasUnsaved() {
		prev := -1
		for j, e := range s.configs {
			if e.Name == s.editorName {
				prev = j
				break
			}
		}
		a.configSel = prev
		a.confirmThen("放弃未保存的修改？",
			"切换配置会丢弃编辑器里尚未保存的内容（原文件不受影响）。",
			func(context.Context) error {
				a.editorDirty = false
				a.loadConfig(name)
				return nil
			})
		return
	}
	a.loadConfig(name)
}

// hasUnsaved reports raw-editor or form changes that are not saved yet.
func (a *App) hasUnsaved() bool {
	return (a.editorBuf != nil && a.editorBuf.Modified()) || a.editorDirty || a.formDirty() || a.probeSettingsDirty()
}

// formDirty reports whether the form differs from the loaded document.
func (a *App) formDirty() bool {
	if a.editorBuf == nil || a.formExtractErr != "" {
		return false
	}
	cur, err := a.formCurrent()
	if err != nil {
		return true // invalid input counts as unsaved
	}
	return !cur.IsEmpty()
}

// loadConfig fetches one config's raw text into the editor.
func (a *App) loadConfig(name string) {
	client := a.currentClient()
	if a.editorLoading {
		return
	}
	a.editorLoading = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		payload, err := client.ConfigGet(ctx, name)
		a.publishFor(client, func() {
			a.editorLoading = false
			if err != nil {
				a.setConfigMsg(err, "")
				return
			}
			a.adoptConfig(name, payload.Entry.Revision, payload.Content, payload.Valid, payload.ParseError)
		})
		a.poll()
	}()
}

// adoptConfig makes the editor (both tabs) show one document.
func (a *App) adoptConfig(name, revision, content string, valid bool, parseErr string) {
	a.editorBuf = newConfigEditor(content)
	a.editorName = name
	a.editorRev = revision
	a.editorDirty = false
	a.configValid = valid
	a.configParseErr = parseErr
	a.configCoreNote = ""
	a.configMsg = ""
	a.configMsgErr = false
	a.populateForm(content)
}

// populateForm extracts the form fields from a document.
func (a *App) populateForm(text string) {
	v, err := configdoc.Extract(text)
	if err != nil {
		a.formExtractErr = err.Error()
		a.formBase = configdoc.Values{}
		a.formName, a.formHost, a.formIPv4, a.formListeners = "", "", "", ""
		a.formNetName, a.formSecret = "", ""
		a.formDHCP, a.formIPv6Auto, a.formVpnOn = false, false, false
		a.formPeers = nil
		a.formFlagVals = nil
		a.formDevName, a.formMTU, a.formBps, a.formRelayWL = "", "", "", ""
		a.formSocks5, a.formRoutes, a.formExitNodes, a.formMapped = "", "", "", ""
		a.formStun, a.formStunV6, a.formTCPWhitelist, a.formUDPWhitelist = "", "", "", ""
		a.formCredential, a.formNetNS, a.formIPv6Prefix = "", "", ""
		a.formSecureMode = false
		a.formSecurePrivate = ""
		a.formSecurePublic = ""
		a.formProxyNets, a.formPortFwds = nil, nil
		a.formVpnCIDR, a.formVpnListen = "", ""
		return
	}
	a.formExtractErr = ""
	a.formBase = v
	a.formName = derefString(v.InstanceName)
	a.formHost = derefString(v.Hostname)
	a.formIPv4 = derefString(v.IPv4)
	a.formListeners = strings.Join(derefStrings(v.Listeners), ", ")
	a.formNetName = derefString(v.NetworkName)
	a.formSecret = derefString(v.NetworkSecret)
	a.formDHCP = derefBool(v.DHCP)
	a.formIPv6Auto = derefBool(v.IPv6PublicAddrAuto)

	a.formPeers = nil
	if v.Peers != nil {
		for _, p := range *v.Peers {
			a.formPeers = append(a.formPeers, peerRow{URI: p.URI, Key: p.PublicKey})
		}
	}

	// Flags: show the document's value, or the core default when unset.
	a.formFlagVals = make([]bool, len(flagFields))
	defaults := configdoc.DefaultFlags()
	for i, f := range flagFields {
		if b, ok := v.Flags[f.Key]; ok {
			a.formFlagVals[i] = b
		} else {
			a.formFlagVals[i] = defaults[f.Key]
		}
	}
	a.formDevName = derefString(v.DevName)
	a.formMTU = uintText32(v.MTU)
	a.formBps = uintText64(v.InstanceRecvBpsLimit)
	a.formRelayWL = derefString(v.RelayNetworkWhitelist)
	a.formSocks5 = derefString(v.Socks5Proxy)
	a.formRoutes = strings.Join(derefStrings(v.Routes), ", ")
	a.formExitNodes = strings.Join(derefStrings(v.ExitNodes), ", ")
	a.formMapped = strings.Join(derefStrings(v.MappedListeners), ", ")
	a.formStun = strings.Join(derefStrings(v.StunServers), ", ")
	a.formStunV6 = strings.Join(derefStrings(v.StunServersV6), ", ")
	a.formTCPWhitelist = strings.Join(derefStrings(v.TCPWhitelist), ", ")
	a.formUDPWhitelist = strings.Join(derefStrings(v.UDPWhitelist), ", ")
	a.formCredential = derefString(v.CredentialFile)
	a.formNetNS = derefString(v.NetNS)
	a.formSecureMode = v.SecureModeEnabled != nil && *v.SecureModeEnabled
	a.formSecurePrivate = derefString(v.SecurePrivateKey)
	a.formSecurePublic = derefString(v.SecurePublicKey)
	a.formIPv6Prefix = derefString(v.IPv6PublicAddrPrefix)

	a.formProxyNets = nil
	if v.ProxyNetworks != nil {
		for _, n := range *v.ProxyNetworks {
			a.formProxyNets = append(a.formProxyNets, proxyRow{
				CIDR: n.CIDR, Mapped: n.MappedCIDR, Allow: strings.Join(n.Allow, ", "),
			})
		}
	}
	a.formPortFwds = nil
	if v.PortForwards != nil {
		for _, f := range *v.PortForwards {
			a.formPortFwds = append(a.formPortFwds, forwardRow{Proto: f.Proto, Bind: f.BindAddr, Dst: f.DstAddr})
		}
	}
	if v.VpnPortal != nil {
		a.formVpnOn = true
		a.formVpnCIDR = v.VpnPortal.ClientCIDR
		a.formVpnListen = v.VpnPortal.WireguardListen
	} else {
		a.formVpnOn = false
		a.formVpnCIDR, a.formVpnListen = "", ""
	}
}

// formCurrent builds a Values with only the fields the form changed. It
// returns an error for input the engine cannot encode (non-numeric MTU,
// an enabled VPN portal without its required fields).
func (a *App) formCurrent() (configdoc.Values, error) {
	var v configdoc.Values
	if s := strIfChanged(a.formBase.InstanceName, a.formName); s != nil {
		v.InstanceName = s
	}
	if s := strIfChanged(a.formBase.Hostname, a.formHost); s != nil {
		v.Hostname = s
	}
	if s := strIfChanged(a.formBase.IPv4, a.formIPv4); s != nil {
		v.IPv4 = s
	}
	if b := boolIfChanged(a.formBase.DHCP, a.formDHCP); b != nil {
		v.DHCP = b
	}
	listeners := splitList(a.formListeners)
	if !slices.Equal(listeners, derefStrings(a.formBase.Listeners)) {
		v.Listeners = &listeners
	}
	if s := strIfChanged(a.formBase.NetworkName, a.formNetName); s != nil {
		v.NetworkName = s
	}
	if s := strIfChanged(a.formBase.NetworkSecret, a.formSecret); s != nil {
		v.NetworkSecret = s
	}

	// Peers: drop empty rows, compare as blocks.
	peers := make([]configdoc.Peer, 0, len(a.formPeers))
	for _, p := range a.formPeers {
		if strings.TrimSpace(p.URI) == "" {
			continue
		}
		peers = append(peers, configdoc.Peer{URI: strings.TrimSpace(p.URI), PublicKey: strings.TrimSpace(p.Key)})
	}
	if !equalBlocks(peers, derefPeers2(a.formBase.Peers)) {
		v.Peers = &peers
	}

	// Flags: a difference against the document is a change; when the
	// document does not set the flag, the core's default is the baseline.
	defaults := configdoc.DefaultFlags()
	flags := map[string]bool{}
	for i, f := range flagFields {
		if i >= len(a.formFlagVals) {
			break
		}
		shown := a.formFlagVals[i]
		baseV, inBase := a.formBase.Flags[f.Key]
		if inBase {
			if shown != baseV {
				flags[f.Key] = shown
			}
		} else if shown != defaults[f.Key] {
			flags[f.Key] = shown
		}
	}
	if len(flags) > 0 {
		v.Flags = flags
	}

	if s := strIfChanged(a.formBase.DevName, a.formDevName); s != nil {
		v.DevName = s
	}
	// Numbers: empty means "remove the key" when the document had it.
	baseMTU := uintText32(a.formBase.MTU)
	if form := strings.TrimSpace(a.formMTU); form != baseMTU {
		if form == "" {
			v.Remove = append(v.Remove, "flags.mtu")
		} else {
			n, err := strconv.ParseUint(form, 10, 32)
			if err != nil {
				return v, fmt.Errorf("MTU 需要整数：%q", a.formMTU)
			}
			u := uint32(n)
			v.MTU = &u
		}
	}
	baseBps := uintText64(a.formBase.InstanceRecvBpsLimit)
	if form := strings.TrimSpace(a.formBps); form != baseBps {
		if form == "" {
			v.Remove = append(v.Remove, "flags.instance_recv_bps_limit")
		} else {
			n, err := strconv.ParseUint(form, 10, 64)
			if err != nil {
				return v, fmt.Errorf("接收限速需要整数（字节/秒）：%q", a.formBps)
			}
			u := n
			v.InstanceRecvBpsLimit = &u
		}
	}
	if s := strIfChanged(a.formBase.RelayNetworkWhitelist, a.formRelayWL); s != nil {
		v.RelayNetworkWhitelist = s
	}
	if b := boolIfChanged(a.formBase.IPv6PublicAddrAuto, a.formIPv6Auto); b != nil {
		v.IPv6PublicAddrAuto = b
	}
	baseSocks := derefString(a.formBase.Socks5Proxy)
	if s := strings.TrimSpace(a.formSocks5); s != baseSocks {
		if s == "" {
			v.Remove = append(v.Remove, "socks5_proxy")
		} else {
			v.Socks5Proxy = &s
		}
	}
	routes := splitList(a.formRoutes)
	if !slices.Equal(routes, derefStrings(a.formBase.Routes)) {
		v.Routes = &routes
	}
	exits := splitList(a.formExitNodes)
	if !slices.Equal(exits, derefStrings(a.formBase.ExitNodes)) {
		v.ExitNodes = &exits
	}
	mapped := splitList(a.formMapped)
	if !slices.Equal(mapped, derefStrings(a.formBase.MappedListeners)) {
		v.MappedListeners = &mapped
	}
	for _, field := range []struct {
		base  *[]string
		value string
		out   **[]string
	}{
		{a.formBase.StunServers, a.formStun, &v.StunServers}, {a.formBase.StunServersV6, a.formStunV6, &v.StunServersV6},
		{a.formBase.TCPWhitelist, a.formTCPWhitelist, &v.TCPWhitelist}, {a.formBase.UDPWhitelist, a.formUDPWhitelist, &v.UDPWhitelist},
	} {
		list := splitList(field.value)
		if !slices.Equal(list, derefStrings(field.base)) {
			*field.out = &list
		}
	}
	for _, field := range []struct {
		base  *string
		value string
		out   **string
	}{
		{a.formBase.CredentialFile, a.formCredential, &v.CredentialFile}, {a.formBase.NetNS, a.formNetNS, &v.NetNS}, {a.formBase.IPv6PublicAddrPrefix, a.formIPv6Prefix, &v.IPv6PublicAddrPrefix},
		{a.formBase.SecurePrivateKey, a.formSecurePrivate, &v.SecurePrivateKey}, {a.formBase.SecurePublicKey, a.formSecurePublic, &v.SecurePublicKey},
	} {
		if changed := strIfChanged(field.base, field.value); changed != nil {
			*field.out = changed
		}
	}
	baseSecure := a.formBase.SecureModeEnabled != nil && *a.formBase.SecureModeEnabled
	if a.formSecureMode != baseSecure {
		value := a.formSecureMode
		v.SecureModeEnabled = &value
	}

	nets := make([]configdoc.ProxyNetwork, 0, len(a.formProxyNets))
	for _, n := range a.formProxyNets {
		if strings.TrimSpace(n.CIDR) == "" {
			continue
		}
		allow := splitList(n.Allow)
		if len(allow) == 0 {
			allow = nil
		}
		nets = append(nets, configdoc.ProxyNetwork{
			CIDR:       strings.TrimSpace(n.CIDR),
			MappedCIDR: strings.TrimSpace(n.Mapped),
			Allow:      allow,
		})
	}
	if !equalBlocks(nets, derefProxy2(a.formBase.ProxyNetworks)) {
		v.ProxyNetworks = &nets
	}

	fwds := make([]configdoc.PortForward, 0, len(a.formPortFwds))
	for _, f := range a.formPortFwds {
		if strings.TrimSpace(f.Bind) == "" && strings.TrimSpace(f.Dst) == "" {
			continue
		}
		proto := "tcp"
		if strings.TrimSpace(f.Proto) == "udp" {
			proto = "udp"
		}
		fwds = append(fwds, configdoc.PortForward{
			Proto: proto, BindAddr: strings.TrimSpace(f.Bind), DstAddr: strings.TrimSpace(f.Dst),
		})
	}
	if !equalBlocks(fwds, derefForwards2(a.formBase.PortForwards)) {
		v.PortForwards = &fwds
	}

	switch {
	case !a.formVpnOn && a.formBase.VpnPortal != nil:
		v.Remove = append(v.Remove, "vpn_portal_config")
	case a.formVpnOn:
		cidr := strings.TrimSpace(a.formVpnCIDR)
		listen := strings.TrimSpace(a.formVpnListen)
		if cidr == "" || listen == "" {
			return v, errors.New("VPN 门户需要客户端网段与监听地址；不使用时请关闭开关")
		}
		cur := configdoc.VpnPortalConfig{ClientCIDR: cidr, WireguardListen: listen}
		if a.formBase.VpnPortal == nil || *a.formBase.VpnPortal != cur {
			v.VpnPortal = &cur
		}
	}
	return v, nil
}

// effectiveText is what a save or export would write: the raw buffer, or
// the buffer with the form's changes applied in place.
func (a *App) effectiveText() (string, error) {
	if a.editorBuf == nil {
		return "", errors.New("还没有加载配置")
	}
	text := a.editorBuf.String()
	if a.configTab != 0 {
		return text, nil
	}
	if a.formExtractErr != "" {
		return "", errors.New("该配置无法用表单编辑：" + a.formExtractErr)
	}
	changes, err := a.formCurrent()
	if err != nil {
		return "", err
	}
	if changes.IsEmpty() {
		return text, nil
	}
	out, err := configdoc.ApplyValues(text, changes)
	if err != nil {
		return "", fmt.Errorf("表单无法应用：%w", err)
	}
	return out, nil
}

// configDo runs one config operation off the main thread.
func (a *App) configDo(run func(ctx context.Context) error) {
	client := a.currentClient()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := run(ctx); err != nil {
			a.publishFor(client, func() { a.setConfigMsg(err, "") })
		}
		a.poll()
	}()
}

func (a *App) saveConfig() {
	client := a.currentClient()
	buf, name, rev := a.editorBuf, a.editorName, a.editorRev
	if buf == nil || name == "" {
		return
	}
	content, err := a.effectiveText()
	if err != nil {
		a.setConfigMsg(err, "")
		return
	}
	a.configDo(func(ctx context.Context) error {
		payload, err := client.ConfigSave(ctx, name, content, rev)
		if err != nil {
			return err
		}
		a.publishFor(client, func() {
			// A completed save must not replace edits made while the request
			// was in flight, or a document selected since it began.
			if a.editorBuf != buf || a.editorName != name {
				return
			}
			current, currentErr := a.effectiveText()
			if currentErr != nil || current != content {
				a.editorRev = payload.Entry.Revision
				a.setConfigMsg(nil, "已保存提交的修订；后续修改仍未保存")
				return
			}
			a.editorBuf = newConfigEditor(content)
			a.editorRev = payload.Entry.Revision
			a.editorDirty = false
			a.configValid = payload.Valid
			a.configParseErr = payload.ParseError
			a.populateForm(content)
			if payload.Valid {
				a.setConfigMsg(nil, "已保存："+name)
			} else {
				a.setConfigMsg(nil, "已保存，但解析失败："+payload.ParseError)
			}
		})
		return nil
	})
}

func (a *App) validateConfig() {
	client := a.currentClient()
	buf, name := a.editorBuf, a.editorName
	if buf == nil || name == "" {
		return
	}
	content, err := a.effectiveText()
	if err != nil {
		a.setConfigMsg(err, "")
		return
	}
	a.configDo(func(ctx context.Context) error {
		payload, err := client.ConfigValidate(ctx, name, content, true)
		if err != nil {
			return err
		}
		a.publishFor(client, func() {
			// Validation describes the submitted bytes, not a newer edit or
			// another document opened while the upstream check was running.
			if !a.editorMatches(buf, name, content) {
				return
			}
			a.configValid = payload.Valid
			a.configParseErr = payload.ParseError
			a.configCoreNote = payload.CoreCheckNote
			switch {
			case !payload.Valid:
				a.setConfigMsg(nil, "解析失败（未保存）")
			case payload.CoreCheck && !payload.CoreCheckOK:
				a.setConfigMsg(nil, "解析通过，但上游核心拒绝该配置")
			default:
				a.setConfigMsg(nil, "校验通过（解析 + 上游检查）")
			}
		})
		return nil
	})
}

func (a *App) editorMatches(buf *configEditor, name, content string) bool {
	if a.editorBuf != buf || a.editorName != name {
		return false
	}
	current, err := a.effectiveText()
	return err == nil && current == content
}

func (a *App) activateConfig() {
	client := a.currentClient()
	name := a.editorName
	revision := a.editorRev
	if name == "" {
		return
	}
	a.confirmThen("应用配置并重启核心？",
		"核心会用该配置重启，当前网络连接会短暂中断。手动停止状态也会被这次明确的应用操作恢复为运行。",
		func(ctx context.Context) error {
			if _, err := client.ConfigActivate(ctx, name, revision); err != nil {
				return err
			}
			a.publishFor(client, func() {
				a.setConfigMsg(nil, "已应用："+name+"（核心已重启，进入启动宽限）")
			})
			return nil
		})
}

func (a *App) createConfig(name string) {
	client := a.currentClient()
	if name == "" {
		a.setConfigMsg(errInvalidNewName, "")
		return
	}
	a.configSelectPending = name
	a.configDo(func(ctx context.Context) error {
		if _, err := client.ConfigCreate(ctx, name, ""); err != nil {
			a.publishFor(client, func() { a.configSelectPending = "" })
			return err
		}
		a.publishFor(client, func() { a.setConfigMsg(nil, "已创建："+name+"（模板，编辑后保存）") })
		return nil
	})
}

func (a *App) deleteConfig() {
	client := a.currentClient()
	name := a.editorName
	if name == "" {
		return
	}
	a.confirmThen("删除配置 "+name+"？",
		"删除前会备份到配置目录的 backups/ 下；活动配置不能删除。",
		func(ctx context.Context) error {
			if err := client.ConfigDelete(ctx, name); err != nil {
				return err
			}
			a.publishFor(client, func() {
				if a.editorName == name {
					a.editorBuf = nil
					a.editorName = ""
					a.editorRev = ""
					a.editorDirty = false
					a.populateForm("")
				}
				a.setConfigMsg(nil, "已删除："+name+"（已备份）")
			})
			return nil
		})
}

// importConfig copies a local .toml file into the store (raw text kept
// as-is).
func (a *App) importConfig() {
	client := a.currentClient()
	a.mu.Lock()
	w := a.window
	a.mu.Unlock()
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:   w,
			Title:    "导入网络配置",
			Multiple: false,
			Filters:  []mygo.FileFilter{{Name: "TOML 配置", Extensions: []string{"toml"}}},
		})
		if err != nil || len(paths) == 0 {
			return
		}
		data, err := os.ReadFile(paths[0])
		if err != nil {
			a.publishFor(client, func() { a.setConfigMsg(err, "") })
			return
		}
		name := sanitizeConfigName(strings.TrimSuffix(filepath.Base(paths[0]), filepath.Ext(paths[0])))
		a.publishFor(client, func() { a.configSelectPending = name })
		a.configDo(func(ctx context.Context) error {
			if _, err := client.ConfigCreate(ctx, name, string(data)); err != nil {
				a.publishFor(client, func() { a.configSelectPending = "" })
				return err
			}
			a.publishFor(client, func() { a.setConfigMsg(nil, "已导入："+name) })
			return nil
		})
	}()
}

// exportConfig writes the effective text (raw, including secrets) to a
// local file the user chooses.
func (a *App) exportConfig() {
	if a.editorBuf == nil || a.editorName == "" {
		return
	}
	content, err := a.effectiveText()
	if err != nil {
		a.setConfigMsg(err, "")
		return
	}
	name := a.editorName
	a.mu.Lock()
	w := a.window
	a.mu.Unlock()
	go func() {
		path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
			Parent:      w,
			Title:       "导出网络配置",
			DefaultPath: name + ".toml",
			Filters:     []mygo.FileFilter{{Name: "TOML 配置", Extensions: []string{"toml"}}},
		})
		if err != nil || path == "" {
			return
		}
		if !strings.HasSuffix(path, ".toml") {
			path += ".toml"
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			a.publish(func() { a.setConfigMsg(err, "") })
			return
		}
		a.publish(func() { a.setConfigMsg(nil, "已导出："+path) })
	}()
}

// sanitizeConfigName turns a file name into something the store accepts.
func sanitizeConfigName(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		switch {
		case r == '/' || r == '\\' || r == 0:
			b.WriteRune('-')
		case r < 0x20:
			// drop control characters
		default:
			b.WriteRune(r)
		}
	}
	name := strings.TrimSpace(b.String())
	if name == "" || strings.HasPrefix(name, ".") {
		name = "imported"
	}
	if len([]rune(name)) > 64 {
		name = string([]rune(name)[:64])
	}
	return name
}

// editorColumn is the right half: header, tabs, toolbar, the active view.
func (a *App) editorColumn(c *ui.Context, s configCopy) {
	t := c.Theme()
	ui.Column(c).Fill().Grow(1).Gap(8).Children(func() {
		if s.editorBuf == nil {
			ui.Text(c, "在左侧选择一个配置，或新建/导入。").TextColor(t.TextMuted)
			return
		}

		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, s.editorName).Bold().FontSize(15).SingleLine()
			if s.editorName == s.activeConfig {
				ui.Text(c, "当前网络").FontSize(11).TextColor(t.Success)
				if s.appliedRevision != "" && s.appliedRevision != s.editorRev {
					ui.Text(c, "已保存未应用").FontSize(11).TextColor(t.Warning)
				}
			}
			if s.editorLoading {
				ui.Text(c, "读取中…").FontSize(11).TextColor(t.TextMuted)
			}
			if a.hasUnsaved() {
				ui.Text(c, "● 未保存").FontSize(11).TextColor(t.Warning)
			}
			ui.Text(c, "修订 "+revisionShort(s.editorRev)).FontSize(11).TextColor(t.TextMuted)
			ui.Spacer(c)
			if s.valid && s.parseErr == "" {
				line := "解析通过"
				if s.coreNote != "" {
					line = s.coreNote
				}
				ui.Text(c, line).FontSize(11).TextColor(t.TextMuted).SingleLine()
			}
		})
		if s.activationState != "" {
			ui.Text(c, "应用状态："+activationLabel(s.activationState)).FontSize(11).TextColor(t.TextMuted)
		}
		if s.activationError != "" {
			ui.Text(c, s.activationError).FontSize(11).TextColor(t.Danger)
		}

		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Tabs(c, &a.configTab, "表单", "原文")
			ui.Spacer(c)
			ui.Button(c, "校验").OnClick(func() { a.validateConfig() })
			ui.Button(c, "导出…").OnClick(func() { a.exportConfig() })
			if a.hasUnsaved() {
				ui.PrimaryButton(c, "保存").OnClick(func() { a.saveConfig() })
			} else {
				ui.Button(c, "保存").Disabled(true)
			}
			if s.editorName != s.activeConfig {
				ui.PrimaryButton(c, "应用").OnClick(func() { a.activateConfig() })
			}
			ui.Button(c, "删除…").OnClick(func() { a.deleteConfig() })
		})

		if s.parseErr != "" && (a.configTab != 0 || a.formExtractErr != "" || a.editorDirty) {
			ui.Text(c, "解析失败："+s.parseErr).FontSize(11).TextColor(t.Danger).Wrap()
		}

		if a.configTab == 0 {
			a.formView(c)
		} else {
			a.rawView(c)
		}

		ui.Row(c).Gap(12).Children(func() {
			if s.msg != "" {
				if s.msgErr {
					ui.Text(c, s.msg).FontSize(11).TextColor(t.Danger).SingleLine()
				} else {
					ui.Text(c, s.msg).FontSize(11).TextColor(t.Success).SingleLine()
				}
			}
			ui.Spacer(c)
			ui.Text(c, "原文与注释保持不变；导出的文件包含网络密钥，注意保管。").
				FontSize(11).TextColor(t.TextMuted)
		})
	})
}

func activationLabel(state string) string {
	switch state {
	case "preflight":
		return "预检中"
	case "starting":
		return "启动候选网络"
	case "rollback":
		return "回滚中"
	case "rolled-back":
		return "应用失败，已恢复上一配置"
	case "rollback-failed":
		return "回滚失败，需人工处理"
	case "cancelled":
		return "已取消，保持停止"
	case "committed", "applied":
		return "已应用"
	case "unconfirmed":
		return "无法确认，需人工处理"
	case "failed":
		return "应用失败"
	default:
		return "状态未知"
	}
}

// formView renders the form. It scrolls as one tall list row so a long
// field list does not push the buttons off screen.
func (a *App) formView(c *ui.Context) {
	t := c.Theme()
	if a.formExtractErr != "" {
		ui.Text(c, "该配置无法用表单编辑："+a.formExtractErr).TextColor(t.Danger).Wrap()
		ui.Text(c, "请切到“原文”页编辑；保存与校验仍可在原文页进行。").
			FontSize(12).TextColor(t.TextMuted)
		return
	}
	ui.List(c, &a.formScroll, 1, func(int) {
		ui.Column(c).Gap(10).ClipX().Children(func() {
			a.formSectionHeader(c, "基础")
			a.formRow(c, "实例名称", "显示名，可留空", func() {
				ui.TextInput(c, &a.formName).Placeholder("instance_name").Grow(1)
			})
			a.formRow(c, "主机名", "默认使用系统主机名", func() {
				ui.TextInput(c, &a.formHost).Placeholder("hostname").Grow(1)
			})
			a.formRow(c, "虚拟 IPv4", "留空由 DHCP/对端分配", func() {
				ui.TextInput(c, &a.formIPv4).Placeholder("10.144.44.1").Grow(1)
			})
			a.formRow(c, "自动分配地址", "DHCP：虚拟地址由网络自动分配", func() {
				ui.Switch(c, &a.formDHCP)
			})
			a.formRow(c, "监听端口", "逗号分隔；与其它 EasyTier 共存时务必显式指定", func() {
				ui.TextInput(c, &a.formListeners).Placeholder("tcp://0.0.0.0:11010, udp://0.0.0.0:11010").Grow(1)
			})
			a.formRow(c, "网络名称", "同一网络的所有节点必须一致", func() {
				ui.TextInput(c, &a.formNetName).Placeholder("network_name").Grow(1)
			})
			a.formRow(c, "网络密钥", "同一网络的共享密钥；保存后请妥善保管", func() {
				ui.TextInput(c, &a.formSecret).Placeholder("network_secret").Grow(1)
			})

			ui.Text(c, "对端（peer）").Bold()
			if len(a.formPeers) == 0 {
				ui.Text(c, "还没有配置对端；只有与同一网络中的对端建立连接才能组网。").
					FontSize(12).TextColor(t.TextMuted)
			}
			for i := range a.formPeers {
				i := i
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.TextInput(c, &a.formPeers[i].URI).Placeholder("tcp://host:11010").Grow(1)
					ui.TextInput(c, &a.formPeers[i].Key).Placeholder("公钥（可选）").Width(160)
					ui.Button(c, "删除").OnClick(func() {
						a.formPeers = append(a.formPeers[:i], a.formPeers[i+1:]...)
					})
				})
			}
			ui.Row(c).Gap(8).Children(func() {
				ui.Button(c, "添加对端").OnClick(func() {
					a.formPeers = append(a.formPeers, peerRow{})
				})
			})

			a.formSectionHeader(c, "高级")
			c.Key("secure-mode")
			ui.Checkbox(c, &a.formSecureMode, "启用安全模式")
			ui.Text(c, "显式填写匹配的 base64 X25519 本地公私钥；不会自动生成身份。对端公钥在 peer 字段配置，保存后显式应用。").FontSize(11).TextColor(t.TextMuted)
			c.Key("secure-private-key")
			ui.TextInput(c, &a.formSecurePrivate).Label("本地私钥（敏感）").Password().FillWidth()
			c.Key("secure-public-key")
			ui.TextInput(c, &a.formSecurePublic).Label("本地公钥").FillWidth()
			a.formRow(c, "开发名 / 网卡", "dev_name：绑定指定网卡（留空自动）", func() {
				ui.TextInput(c, &a.formDevName).Placeholder("eth0 / wlan0").Grow(1)
			})
			a.formRow(c, "MTU", "默认 1380；范围约 400–1380", func() {
				ui.TextInput(c, &a.formMTU).Placeholder("1380").Width(130)
			})
			a.formRow(c, "接收限速", "instance_recv_bps_limit（字节/秒，留空不限）", func() {
				ui.TextInput(c, &a.formBps).Placeholder("不限").Width(160)
			})
			a.formRow(c, "中继网络白名单", "relay_network_whitelist，逗号分隔网段，默认 *", func() {
				ui.TextInput(c, &a.formRelayWL).Placeholder("*").Grow(1)
			})
			a.formRow(c, "IPv6 公网地址自动", "ipv6_public_addr_auto", func() {
				ui.Switch(c, &a.formIPv6Auto)
			})
			a.formRow(c, "SOCKS5 代理", "socks5_proxy：留空关闭，例如 socks5://0.0.0.0:1080", func() {
				ui.TextInput(c, &a.formSocks5).Placeholder("socks5://0.0.0.0:1080").Grow(1)
			})
			a.formRow(c, "路由", "routes：手动通告的网段，逗号分隔", func() {
				ui.TextInput(c, &a.formRoutes).Placeholder("192.168.0.0/16").Grow(1)
			})
			a.formRow(c, "出口节点", "exit_nodes：父节点虚拟 IP 列表，逗号分隔", func() {
				ui.TextInput(c, &a.formExitNodes).Placeholder("10.144.44.9").Grow(1)
			})
			a.formRow(c, "映射监听器", "mapped_listeners：公网映射地址，逗号分隔", func() {
				ui.TextInput(c, &a.formMapped).Placeholder("tcp://public.example:11010").Grow(1)
			})

			ui.Text(c, "标志开关").Bold()
			for _, field := range []struct {
				key, label, hint string
				value            *string
			}{
				{"stun", "STUN IPv4", "服务器列表，逗号分隔", &a.formStun},
				{"stun-v6", "STUN IPv6", "服务器列表，逗号分隔", &a.formStunV6},
				{"tcp-whitelist", "TCP 白名单", "条目列表，逗号分隔", &a.formTCPWhitelist},
				{"udp-whitelist", "UDP 白名单", "条目列表，逗号分隔", &a.formUDPWhitelist},
				{"credential", "凭据文件", "只引用路径，不读取文件内容", &a.formCredential},
				{"netns", "网络命名空间", "Linux netns 名称", &a.formNetNS},
				{"ipv6-prefix", "IPv6 公网前缀", "例如 2001:db8::/64", &a.formIPv6Prefix},
			} {
				c.Key(field.key)
				a.formRow(c, field.label, field.hint, func() { ui.TextInput(c, field.value).Grow(1) })
			}
			for i := range flagFields {
				if i >= len(a.formFlagVals) {
					break
				}
				i := i
				f := flagFields[i]
				ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
					ui.Switch(c, &a.formFlagVals[i])
					ui.Text(c, f.Label).FontSize(13).Width(170)
					if f.Hint != "" {
						ui.Text(c, f.Hint).FontSize(11).TextColor(t.TextMuted).SingleLine()
					}
				})
			}

			a.formSectionHeader(c, "子网代理（proxy_network）")
			if len(a.formProxyNets) == 0 {
				ui.Text(c, "还没有子网代理；把对端子网经本机转发时添加。").
					FontSize(12).TextColor(t.TextMuted)
			}
			for i := range a.formProxyNets {
				i := i
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.TextInput(c, &a.formProxyNets[i].CIDR).Placeholder("10.1.1.0/24").Grow(1)
					ui.TextInput(c, &a.formProxyNets[i].Mapped).Placeholder("映射（可选）").Width(150)
					ui.TextInput(c, &a.formProxyNets[i].Allow).Placeholder("allow：tcp,udp,icmp").Width(170)
					ui.Button(c, "删除").OnClick(func() {
						a.formProxyNets = append(a.formProxyNets[:i], a.formProxyNets[i+1:]...)
					})
				})
			}
			ui.Row(c).Gap(8).Children(func() {
				ui.Button(c, "添加子网代理").OnClick(func() {
					a.formProxyNets = append(a.formProxyNets, proxyRow{})
				})
			})

			a.formSectionHeader(c, "端口转发（port_forward）")
			if len(a.formPortFwds) == 0 {
				ui.Text(c, "还没有端口转发；把本机端口映射到虚拟网络内地址时添加。").
					FontSize(12).TextColor(t.TextMuted)
			}
			for i := range a.formPortFwds {
				i := i
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					if a.formPortFwds[i].Proto == "udp" {
						ui.Button(c, "UDP").OnClick(func() { a.formPortFwds[i].Proto = "tcp" })
					} else {
						ui.Button(c, "TCP").OnClick(func() { a.formPortFwds[i].Proto = "udp" })
					}
					ui.TextInput(c, &a.formPortFwds[i].Bind).Placeholder("0.0.0.0:8080").Grow(1)
					ui.Text(c, "→").TextColor(t.TextMuted)
					ui.TextInput(c, &a.formPortFwds[i].Dst).Placeholder("10.144.44.9:80").Grow(1)
					ui.Button(c, "删除").OnClick(func() {
						a.formPortFwds = append(a.formPortFwds[:i], a.formPortFwds[i+1:]...)
					})
				})
			}
			ui.Row(c).Gap(8).Children(func() {
				ui.Button(c, "添加端口转发").OnClick(func() {
					a.formPortFwds = append(a.formPortFwds, forwardRow{Proto: "tcp"})
				})
			})

			a.formSectionHeader(c, "VPN 门户（WireGuard）")
			a.formRow(c, "启用 VPN 门户", "vpn_portal_config：允许 WireGuard 客户端接入", func() {
				ui.Switch(c, &a.formVpnOn)
			})
			if a.formVpnOn {
				a.formRow(c, "客户端网段", "client_cidr，例如 10.14.14.0/24", func() {
					ui.TextInput(c, &a.formVpnCIDR).Placeholder("10.14.14.0/24").Grow(1)
				})
				a.formRow(c, "WireGuard 监听", "wireguard_listen，例如 0.0.0.0:11013", func() {
					ui.TextInput(c, &a.formVpnListen).Placeholder("0.0.0.0:11013").Grow(1)
				})
			}
		})
	}).Grow(1).ClipX()
}

// formSectionHeader labels a block of the form.
func (a *App) formSectionHeader(c *ui.Context, title string) {
	t := c.Theme()
	ui.Row(c).Gap(8).AlignItems(ui.Center).PaddingY(4).Children(func() {
		ui.Text(c, title).Bold().FontSize(14)
		ui.Divider(c).Grow(1)
	})
	_ = t
}

// formRow lays out one labelled field.
func (a *App) formRow(c *ui.Context, label, hint string, field func()) {
	t := c.Theme()
	ui.Column(c).Gap(3).Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Text(c, label).Bold().FontSize(13).Width(110)
			field()
		})
		ui.Text(c, hint).FontSize(11).TextColor(t.TextMuted).PaddingX(120)
	})
}

// rawView is the raw TOML editor.
func (a *App) rawView(c *ui.Context) {
	c.Key("config-raw-editor-" + a.editorName)
	editor.View(c, &a.editorBuf.state).Fill().Grow(1)
	a.editorDirty = a.editorBuf.Modified()
}

// --- small helpers ------------------------------------------------------

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func derefBool(b *bool) bool {
	if b == nil {
		return false
	}
	return *b
}

func derefStrings(l *[]string) []string {
	if l == nil {
		return nil
	}
	return *l
}

func derefPeers2(l *[]configdoc.Peer) []configdoc.Peer {
	if l == nil {
		return nil
	}
	return *l
}

func derefProxy2(l *[]configdoc.ProxyNetwork) []configdoc.ProxyNetwork {
	if l == nil {
		return nil
	}
	return *l
}

func derefForwards2(l *[]configdoc.PortForward) []configdoc.PortForward {
	if l == nil {
		return nil
	}
	return *l
}

// equalBlocks compares two block lists, treating nil and empty as equal.
func equalBlocks[T any](a, b []T) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return reflect.DeepEqual(a, b)
}

func strIfChanged(base *string, form string) *string {
	if (base == nil && form == "") || (base != nil && *base == form) {
		return nil
	}
	v := form
	return &v
}

func boolIfChanged(base *bool, form bool) *bool {
	if (base == nil && !form) || (base != nil && *base == form) {
		return nil
	}
	v := form
	return &v
}

func uintText32(v *uint32) string {
	if v == nil {
		return ""
	}
	return strconv.FormatUint(uint64(*v), 10)
}

func uintText64(v *uint64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatUint(*v, 10)
}

// flagValue reports the shown value of one covered flag (used by tests
// and by the form's own consistency checks).
func (a *App) flagValue(key string) bool {
	for i, f := range flagFields {
		if f.Key == key && i < len(a.formFlagVals) {
			return a.formFlagVals[i]
		}
	}
	return false
}

// splitList parses a comma-separated list field.
func splitList(s string) []string {
	out := []string{}
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// revisionShort is defined here so both the list and the header share it.
func revisionShort(rev string) string {
	if len(rev) > 8 {
		return rev[:8]
	}
	return rev
}

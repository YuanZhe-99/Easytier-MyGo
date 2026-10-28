package gui

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/configdoc"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/localapi"
	L "github.com/YuanZhe-99/Easytier-MyGo/locales"
)

// featureName is a configdoc feature ID in words: the settings EasyTier
// mini leaves out, and those tied to an EasyTier version.
func featureName(id string) string {
	switch id {
	case configdoc.MiniListeners:
		return L.FeatureListeners()
	case configdoc.MiniMappedListeners:
		return L.FeatureMappedListeners()
	case configdoc.MiniPeers:
		return L.FeaturePeers()
	case configdoc.MiniSubnetProxy:
		return L.FieldProxies()
	case configdoc.MiniExitNodes:
		return L.FieldExitNodes()
	case configdoc.MiniSocks5:
		return L.FeatureSocks5()
	case configdoc.MiniPortForward:
		return L.FeaturePortForward()
	case configdoc.MiniVpnPortal:
		return L.FeatureVpnPortal()
	case configdoc.MiniIPv6Public:
		return L.FeatureIPv6Public()
	case configdoc.MiniMagicDNS:
		return L.FlagMagicDNS()
	case configdoc.MiniKCP:
		return L.FlagKCP()
	case configdoc.MiniQUIC:
		return L.FlagQUIC()
	case configdoc.MiniUDPBroadcast:
		return L.FeatureUDPBroadcast()
	case configdoc.MiniCompression:
		return L.FeatureCompression()
	case configdoc.MiniEncryption:
		return L.FeatureEncryption()
	case configdoc.NewPortal:
		return L.FeatureNewPortal()
	case configdoc.LegacyPortal:
		return L.FeatureVpnPortal()
	case configdoc.TCPStunServers:
		return L.FeatureTCPStun()
	case configdoc.SocketMark:
		return L.FeatureSocketMark()
	case configdoc.PreferRelay:
		return L.FlagPreferRelay()
	case configdoc.ManagedCreds:
		return L.FeatureManagedCredentials()
	}
	return id
}

// featureList joins feature IDs in words.
func featureList(ids []string) string {
	names := make([]string, len(ids))
	for i, id := range ids {
		names[i] = featureName(id)
	}
	return strings.Join(names, L.ListSeparator())
}

// miniFlags are the form's flags EasyTier mini does not run.
var miniFlags = map[string]bool{"enable_exit_node": true, "accept_dns": true, "enable_kcp_proxy": true, "enable_quic_proxy": true}

// coreNotes are the notices at the top of the form: what EasyTier mini
// leaves out of the saved document, and settings the network's core
// version refuses or ignores.
func (a *App) coreNotes(c *ui.Context, e *configEditor) {
	if !e.mini && len(e.conflicts) == 0 {
		return
	}
	t, k := c.Theme(), tok(c)
	ui.Column(c.Key("f-notes")).FillWidth().Gap(8).Margin(14, 0, 0, 0).Children(func() {
		if e.mini {
			callout(c, "f-mini-note", t.Accent, iconCPU, L.EditMiniNote(), nil)
			if len(e.miniIgnored) > 0 {
				callout(c, "f-mini-ignored", k.warn, iconAlert, L.EditMiniIgnored(featureList(e.miniIgnored)), nil)
			}
		}
		switch {
		case len(e.conflicts) == 1 && e.conflicts[0] == configdoc.LegacyPortal:
			callout(c, "f-version", k.warn, iconAlert, L.EditLegacyPortal(), nil)
		case len(e.conflicts) > 0:
			callout(c, "f-version", k.warn, iconAlert, L.EditNeedsNewer(e.release, featureList(e.conflicts)), nil)
		}
	})
}

// coreSection is the form's Core section: a network on the full core can
// be copied for EasyTier mini; a mini network says where it runs.
func (a *App) coreSection(c *ui.Context, r *rows, e *configEditor) {
	a.mu.Lock()
	busy := a.busy["mini-copy"]
	a.mu.Unlock()
	r.section(c, "f-core", L.EditSectionCore(), nil, func(r *rows) {
		if e.mini {
			r.row(c, "f-core-mini", iconCPU, L.CoreMini(), L.EditMiniRuns(), nil)
			return
		}
		r.row(c, "f-core-copy", iconCopy, L.ActionMiniCopy(), L.EditMiniCopyAbout(), func() {
			if ui.Button(c.Key("f-mini-copy"), L.ActionMiniCopy()).Disabled(busy).Clicked() {
				a.miniCopy(e.id)
			}
		})
	})
}

// miniCopy asks the daemon for a copy of a network made for EasyTier mini
// and opens it; the original is not touched.
func (a *App) miniCopy(id string) {
	a.do("mini-copy", localapi.Request{Type: localapi.RequestMiniCopy, Network: id}, "", func(r *localapi.Response) {
		name := r.Created
		for _, n := range r.Networks {
			if n.ID == r.Created {
				name = n.Name
			}
		}
		msg := L.ToastMiniCopied(name)
		if len(r.Removed) > 0 {
			msg = L.ToastMiniCopiedWithout(name, featureList(r.Removed))
		}
		a.setToast(msg, false)
		a.page, a.editor, a.selected = pageNetwork, nil, r.Created
	})
}

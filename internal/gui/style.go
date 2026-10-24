package gui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/health"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/localapi"
	L "github.com/YuanZhe-99/Easytier-MyGo/locales"
)

// Layout and type scale (doc/en-us/gui.md: body 14, titles 20–24, nothing
// below 12).
const (
	sizeSmall   float32 = 12
	sizeBody    float32 = 14
	sizeTitle   float32 = 22
	sizeHeading float32 = 16
	cardRadius  float32 = 12
	cardPad     float32 = 16
	gap         float32 = 12
	avatarSize  float32 = 36 // a peer's round avatar
	logoSize    float32 = 22 // a vendor logo inside it (about 64%)
	copyIconSz  float32 = 13 // the copy button beside a peer's address
	navIconSize float32 = 16 // the sidebar's and the pane header's icons

	sidebarW     float32 = 248 // the sidebar beside the pane
	railW        float32 = 56  // the sidebar folded to its icons (railWidth)
	railMacW     float32 = 72  // on macOS: the traffic lights and 9 on either side
	sbTopH       float32 = 40  // the sidebar's top row: hide the sidebar and +
	sbRowH       float32 = 46  // a network's row in the sidebar
	netTileSize  float32 = 28  // the tile a network's row starts with
	netBadgeSize float32 = 10  // its status dot, on the tile's corner
	railButton   float32 = 36  // a button of the rail
	paneMargin   float32 = 8   // around the pane, from the window's edges and the sidebar
	paneRadius   float32 = 14  // the pane's corners
	paneHeaderH  float32 = 44  // the pane's header, which is the window's title bar
	paneButton   float32 = 28  // an icon button in the header or the sidebar
	paneTitleMax float32 = 240 // the header's title, at most
	winButtonW   float32 = 40  // a window button the view draws on Windows
	winButtonH   float32 = 30
	winGlyph     float32 = 10 // its glyph
	noticePadX   float32 = 16 // a notice card above the pane, from its sides
	noticePadY   float32 = 14 // and from its top and bottom
	noticeIconSz float32 = 36 // the round icon it starts with
	runDot       float32 = 8  // the run mode's dot at the sidebar's foot
)

// Where the macOS traffic lights sit: the close button's top-left corner at
// the system's own inset from the window's edge, the 14-point buttons
// centred on the pane's header.
const (
	trafficX = 9
	trafficY = int(paneMargin + (paneHeaderH-14)/2)
)

// paneShadow is the pane's shadow over the window's base.
var paneShadow = ui.RGBA(16, 24, 40, 0.07)

// closeHover is the window's close button under the pointer, white on
// Windows' red.
var (
	closeHover     = ui.RGB(196, 43, 28)
	closeHoverText = ui.RGB(255, 255, 255)
)

// The Settings sheet's and dialogs' dimming over the window, their shadow,
// and the window's buttons on the dimming: their glyphs and their face
// under the pointer.
var (
	sheetScrim  = ui.RGBA(0, 0, 0, 0.4)
	sheetShadow = ui.RGBA(0, 0, 0, 0.3)
	scrimGlyph  = ui.RGB(244, 245, 247)
	scrimHover  = ui.RGBA(255, 255, 255, 0.14)
)

// The Settings sheet's layout.
const (
	sheetX       float32 = 64  // from the window's sides
	sheetTop     float32 = 48  // from its top
	sheetBottom  float32 = 32  // from its bottom
	sheetHeaderH float32 = 52  // its header
	settingsNavW float32 = 240 // its sections
	searchH      float32 = 34  // the search field over them
	navRowH      float32 = 32  // a section
	dialogW      float32 = 560 // the Add network dialog
	dialogTop    float32 = 104 // from the window's top
)

// windowBase is the window's own color where it has no material (Linux):
// a shade off the pane, lighter in the light appearance and darker in the
// dark one, so the pane stands out from it in both.
func windowBase(t *ui.Theme) ui.Color {
	if t.Dark {
		return ui.Hex("#111113")
	}
	return t.Surface
}

// tokens are the app's named colors, derived from the theme so they work
// in light and dark mode.
type tokens struct {
	download, upload ui.Color
	good, warn, bad  ui.Color
	onBad            ui.Color // text on a bad (red) fill
	local, server    ui.Color
	peer, relay      ui.Color
	card, muted      ui.Color
	hairline         ui.Color
}

func tok(c *ui.Context) tokens {
	t := c.Theme()
	k := tokens{
		download: ui.Hex("#2f7bf5"), upload: ui.Hex("#f2930d"),
		good: t.Success, warn: t.Warning, bad: t.Danger, onBad: ui.Hex("#ffffff"),
		local: ui.Hex("#22a559"), server: ui.Hex("#e64980"), peer: ui.Hex("#2f7bf5"), relay: ui.Hex("#8e5cf0"),
		card: t.Surface, muted: t.TextMuted, hairline: t.Border,
	}
	if t.Dark {
		k.download, k.upload = ui.Hex("#5b9bff"), ui.Hex("#ffb547")
		k.local, k.server, k.peer, k.relay = ui.Hex("#3dcc7a"), ui.Hex("#ff6b9e"), ui.Hex("#5b9bff"), ui.Hex("#a985ff")
	}
	return k
}

// card is a rounded panel.
func card(c *ui.Context) ui.Element {
	t := c.Theme()
	return ui.Column(c).Background(t.Surface).Radius(cardRadius).Padding(cardPad).Gap(gap).FillWidth().Border(1, t.Border)
}

// stateColor is the color of a network state.
func stateColor(k tokens, s localapi.State) ui.Color {
	switch s {
	case localapi.StateConnected:
		return k.good
	case localapi.StateStarting, localapi.StateConnecting:
		return k.warn
	case localapi.StateFailed:
		return k.bad
	case localapi.StateDegraded, localapi.StateUnknown:
		return k.warn
	}
	return k.muted
}

// stateLabel names a network state for people.
func stateLabel(n localapi.NetworkStatus) string {
	switch n.State {
	case localapi.StateConnected:
		return L.StateConnected()
	case localapi.StateStarting:
		return L.StateStarting()
	case localapi.StateConnecting:
		if n.Reason == "no-peers" {
			return L.StateWaitingPeers()
		}
		return L.StateConnecting()
	case localapi.StateDegraded:
		return L.StateDegraded()
	case localapi.StateFailed:
		return L.StateFailed()
	case localapi.StateUnknown:
		return L.StateUnknown()
	}
	return L.StateStopped()
}

// stateDetail explains a non-connected state in a sentence, or "".
func stateDetail(n localapi.NetworkStatus) string {
	if n.Error != "" {
		return n.Error
	}
	switch {
	case n.State == localapi.StateFailed && n.Recovery == health.RecoveryExhausted:
		return L.DetailRecoveryExhausted()
	case n.State == localapi.StateFailed:
		return L.DetailFailed()
	case n.Reason == "no-peers":
		return L.DetailNoPeers()
	case n.Reason == "awaiting-address":
		return L.DetailAwaitingAddress()
	case n.State == localapi.StateUnknown:
		return L.DetailCollector()
	}
	return ""
}

// statusBadge is a capsule with a dot and the state's name.
func statusBadge(c *ui.Context, n localapi.NetworkStatus) {
	k := tok(c)
	col := stateColor(k, n.State)
	ui.Row(c).Gap(6).AlignItems(ui.Center).Padding(4, 10).Radius(999).Background(col.Alpha(0.12)).Children(func() {
		dot(c, col, 8)
		ui.Text(c, stateLabel(n)).FontSize(sizeSmall).TextColor(col).Bold().NoWrap()
	})
}

// dot is a filled circle.
func dot(c *ui.Context, col ui.Color, d float32) {
	ui.Box(c).Width(d).Height(d).Radius(d / 2).Background(col)
}

// capsule is a small colored label.
func capsule(c *ui.Context, text string, col ui.Color) {
	ui.Box(c).Padding(2, 8).Radius(999).Background(col.Alpha(0.14)).Children(func() {
		ui.Text(c, text).FontSize(sizeSmall).TextColor(col).Bold().NoWrap()
	})
}

// formatRate renders bytes per second in 1024-based units.
func formatRate(bps float64) string {
	return formatBytes(bps) + "/s"
}

// formatBytes renders a byte count in 1024-based units.
func formatBytes(b float64) string {
	units := []string{"B", "KB", "MB", "GB", "TB"}
	i := 0
	for b >= 1024 && i < len(units)-1 {
		b /= 1024
		i++
	}
	switch {
	case i == 0:
		return fmt.Sprintf("%.0f %s", b, units[i])
	case b < 10:
		return fmt.Sprintf("%.1f %s", b, units[i])
	}
	return fmt.Sprintf("%.0f %s", b, units[i])
}

// formatLatency renders milliseconds with one decimal.
func formatLatency(ms float64) string {
	if ms <= 0 || math.IsNaN(ms) {
		return "–"
	}
	return fmt.Sprintf("%.1f ms", ms)
}

// latencyColor follows EasyTier-iOS: green below 100 ms, orange below 200.
func latencyColor(k tokens, ms float64) ui.Color {
	switch {
	case ms <= 0:
		return k.muted
	case ms < 100:
		return k.good
	case ms < 200:
		return k.warn
	}
	return k.bad
}

// lossColor: muted below 2 %, orange below 10 %, red above.
func lossColor(k tokens, loss float64) ui.Color {
	switch {
	case loss < 0.02:
		return k.muted
	case loss < 0.10:
		return k.warn
	}
	return k.bad
}

// formatUptime renders how long a network has run.
func formatUptime(since time.Time) string {
	if since.IsZero() {
		return "–"
	}
	d := time.Since(since).Round(time.Second)
	switch {
	case d < time.Minute:
		return L.DurationSeconds(int(d.Seconds()))
	case d < time.Hour:
		return L.DurationMinutes(int(d.Minutes()))
	case d < 24*time.Hour:
		return L.DurationHours(int(d.Hours()), int(d.Minutes())%60)
	}
	return L.DurationDays(int(d.Hours() / 24))
}

// shortIP drops the prefix length from "10.0.0.1/24".
func shortIP(cidr string) string {
	if i := strings.IndexByte(cidr, '/'); i > 0 {
		return cidr[:i]
	}
	return cidr
}

// natLabel names EasyTier's NAT types for people.
func natLabel(n string) string {
	switch n {
	case "", "Unknown":
		return L.NatUnknown()
	case "OpenInternet":
		return L.NatOpen()
	case "NoPAT":
		return L.NatNoPAT()
	case "FullCone":
		return L.NatFullCone()
	case "Restricted":
		return L.NatRestricted()
	case "PortRestricted":
		return L.NatPortRestricted()
	case "Symmetric", "SymUdpFirewall", "SymmetricEasyInc", "SymmetricEasyDec":
		return L.NatSymmetric()
	}
	return n
}

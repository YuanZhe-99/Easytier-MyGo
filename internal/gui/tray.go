package gui

import (
	"bytes"
	_ "embed"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"strings"

	"github.com/egoist/mygo"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/localapi"
	L "github.com/YuanZhe-99/Easytier-MyGo/locales"
)

//go:embed assets/tray.png
var trayGlyph []byte

// trayState is the aggregate shown by the tray icon.
type trayState int

const (
	trayOff trayState = iota
	trayOn
	trayWarn
)

// trayIcons renders the glyph in three states: gray when nothing runs,
// blue when connected, with a red dot when a network has a problem.
// Inputs: dark, whether the bar the icon shows on is dark; the gray and
// blue are lighter there so the glyph keeps its contrast.
func trayIcons(dark bool) map[trayState][]byte {
	src, err := png.Decode(bytes.NewReader(trayGlyph))
	if err != nil {
		return nil
	}
	b := src.Bounds()
	tint := func(c color.NRGBA, badge bool) []byte {
		dst := image.NewNRGBA(b)
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				_, _, _, a := src.At(x, y).RGBA()
				dst.SetNRGBA(x, y, color.NRGBA{c.R, c.G, c.B, uint8(a >> 8)})
			}
		}
		if badge {
			r := b.Dx() / 3
			cx, cy := b.Max.X-r, b.Max.Y-r
			for y := cy - r; y < cy+r; y++ {
				for x := cx - r; x < cx+r; x++ {
					if (x-cx)*(x-cx)+(y-cy)*(y-cy) <= r*r {
						dst.SetNRGBA(x, y, color.NRGBA{0xe5, 0x3e, 0x3e, 0xff})
					}
				}
			}
		}
		var out bytes.Buffer
		_ = png.Encode(&out, dst)
		return out.Bytes()
	}
	_ = draw.Src
	off, on := color.NRGBA{0x6b, 0x70, 0x79, 0xff}, color.NRGBA{0x2f, 0x7b, 0xf5, 0xff}
	if dark {
		off, on = color.NRGBA{0xb4, 0xb9, 0xc1, 0xff}, color.NRGBA{0x5b, 0x9b, 0xff, 0xff}
	}
	return map[trayState][]byte{
		trayOff:  tint(off, false),
		trayOn:   tint(on, false),
		trayWarn: tint(on, true),
	}
}

// trayModel is what the tray shows; the menu is rebuilt when it changes.
type trayModel struct {
	state    trayState
	status   string
	networks []localapi.NetworkStatus
	mode     runChoice
}

func (m trayModel) key() string {
	var b strings.Builder
	b.WriteString(m.status)
	for _, n := range m.networks {
		b.WriteString("|" + n.ID + n.Name + string(n.State))
		if n.Run {
			b.WriteString("+")
		}
	}
	b.WriteByte(byte('0' + m.mode))
	return b.String()
}

// Tray owns the status icon.
type Tray struct {
	app   *App
	tray  *mygo.Tray
	icons map[trayState][]byte
	last  string
	state trayState
}

// NewTray creates the status icon (main thread). It returns nil when the
// desktop has no tray; closing the window then quits. The glyph follows the
// bar it shows on, which on Windows may be dark while the app is light.
func (a *App) NewTray() *Tray {
	t, err := mygo.NewTray(mygo.TrayOptions{Icon: trayIcons(false)[trayOff], ToolTip: "EasyTier MyGo"})
	if err != nil {
		return nil
	}
	tr := &Tray{app: a, tray: t, icons: trayIcons(t.Dark()), last: "-"}
	t.OnAppearanceChange(func() {
		tr.icons = trayIcons(t.Dark())
		_ = t.SetIcon(tr.icons[tr.state], false)
	})
	tr.Refresh()
	return tr
}

// model summarizes the app's state for the tray.
func (a *App) trayModel() trayModel {
	a.mu.Lock()
	defer a.mu.Unlock()
	m := trayModel{networks: append([]localapi.NetworkStatus(nil), a.networks...), mode: a.currentChoice()}
	on, bad := 0, false
	for _, n := range a.networks {
		if n.State == localapi.StateConnected {
			on++
		}
		if n.State == localapi.StateFailed || n.Error != "" {
			bad = true
		}
	}
	switch {
	case !a.connected:
		m.status = L.TrayDisconnected()
	case on == 0:
		m.status = L.TrayNoneConnected()
	default:
		m.status = L.TrayConnected(on)
	}
	switch {
	case bad:
		m.state = trayWarn
	case on > 0:
		m.state = trayOn
	}
	return m
}

// Refresh updates the icon and rebuilds the menu when something changed
// (main thread).
func (t *Tray) Refresh() {
	if t == nil {
		return
	}
	m := t.app.trayModel()
	if m.state != t.state || t.last == "-" {
		_ = t.tray.SetIcon(t.icons[m.state], false)
		t.state = m.state
	}
	if k := m.key(); k != t.last {
		t.last = k
		t.tray.SetToolTip("EasyTier MyGo — " + m.status)
		t.tray.SetMenu(t.menu(m))
	}
}

// menu: status, one toggle per network, show, settings, quit.
func (t *Tray) menu(m trayModel) *mygo.Menu {
	a := t.app
	items := []*mygo.MenuItem{{Label: m.status, Disabled: true}, mygo.Separator()}
	for _, n := range m.networks {
		id := n.ID
		label := n.Name
		if n.VirtualIPv4 != "" {
			label += "  " + shortIP(n.VirtualIPv4)
		}
		items = append(items, &mygo.MenuItem{Label: label, Type: mygo.MenuItemCheckbox, Checked: n.Run,
			Click: func(it *mygo.MenuItem, _ *mygo.Window) {
				req := localapi.RequestDisconnect
				if it.IsChecked() {
					req = localapi.RequestConnect
				}
				a.do("connect-"+id, localapi.Request{Type: req, Network: id}, "", nil)
			}})
	}
	if len(m.networks) > 0 {
		items = append(items, mygo.Separator())
	}
	items = append(items,
		&mygo.MenuItem{Label: L.TrayShow(), Click: func(*mygo.MenuItem, *mygo.Window) { a.Focus() }},
		&mygo.MenuItem{Label: L.PageSettings(), Click: func(*mygo.MenuItem, *mygo.Window) {
			a.update(func() { a.page = pageSettings })
			a.Focus()
		}},
		mygo.Separator(),
	)
	if m.mode == choiceBackground {
		items = append(items,
			&mygo.MenuItem{Label: L.TrayQuitKeep(), Click: func(*mygo.MenuItem, *mygo.Window) { a.Quit(false) }},
			&mygo.MenuItem{Label: L.TrayQuitDisconnect(), Click: func(*mygo.MenuItem, *mygo.Window) { a.Quit(true) }},
		)
	} else {
		items = append(items, &mygo.MenuItem{Label: L.TrayQuit(), Click: func(*mygo.MenuItem, *mygo.Window) { a.Quit(false) }})
	}
	return mygo.NewMenu(items)
}

// Destroy removes the icon.
func (t *Tray) Destroy() {
	if t != nil {
		t.tray.Destroy()
	}
}

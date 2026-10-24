package gui

import (
	"runtime"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	L "github.com/YuanZhe-99/Easytier-MyGo/locales"
)

// ownWindowButtons tells whether the view draws the window's buttons
// (windowButtons): on Windows, where the system's would sit on the window's
// corner rather than on the pane. macOS keeps its traffic lights and Linux
// the desktop's own buttons.
var ownWindowButtons = runtime.GOOS == "windows"

// TitleBarOptions hides the system title bar: the pane's header takes its
// place (doc/en-us/gui.md, "Window"). On macOS the traffic lights sit over
// the sidebar's top row, centred on the pane's header; on Windows the view
// draws minimize, maximize and close at the end of the header; on Linux the
// desktop's buttons are centred in a title bar as tall as the header and
// its margin. The window keeps its resizing edges, shadow and corners, and
// its material shows around the pane.
// Inputs: o, the window's other options.
// Returns: o with the title-bar fields set.
func TitleBarOptions(o mygo.WindowOptions) mygo.WindowOptions {
	o.TitleBarStyle = mygo.TitleBarHidden
	o.TitleBarHeight = int(2*paneMargin + paneHeaderH)
	o.HideWindowButtons = ownWindowButtons
	o.TrafficLightPosition = &mygo.Point{X: trafficX, Y: trafficY}
	o.Vibrancy = windowMaterial()
	return o
}

// windowMaterial is the material behind the sidebar and around the pane:
// Mica on Windows 11, the sidebar's on macOS, none on Linux (the view then
// paints windowBase itself).
func windowMaterial() mygo.Vibrancy {
	switch runtime.GOOS {
	case "windows":
		return mygo.VibrancyMica
	case "darwin":
		return mygo.VibrancySidebar
	}
	return mygo.VibrancyNone
}

// WatchFullScreen records whether w is in full screen, where the system
// shows no traffic lights, so the sidebar need not keep their room.
// Side effects: subscribes to w's full-screen events.
func (a *App) WatchFullScreen(w *mygo.Window) {
	set := func(on bool) func() {
		return func() { a.update(func() { a.fullScreen = on }) }
	}
	w.OnEnterFullScreen(set(true))
	w.OnLeaveFullScreen(set(false))
}

// titleRoom is the room the system's window buttons take over the view
// (traffic lights on the left on macOS, the desktop's layout on Linux):
// none in full screen, and none on Windows, where the view draws its own.
func (a *App) titleRoom(c *ui.Context) ui.TitleBar {
	a.mu.Lock()
	full := a.fullScreen
	a.mu.Unlock()
	if full {
		return ui.TitleBar{}
	}
	return c.TitleBar()
}

// windowButtons are the window's minimize, maximize or restore, and close
// on Windows (ownWindowButtons), after a separator at the end of the pane's
// header. While the Settings sheet covers the pane they stay in that
// place, over the dimmed window and apart from the sheet's × (onScrim: no
// separator, light glyphs). Maximize brings Windows 11's snap layouts, as
// the system's does; close is the system's close (HandleClose decides).
// The window is nil in the view tests.
func (a *App) windowButtons(c *ui.Context, key string, onScrim bool) {
	if !ownWindowButtons {
		return
	}
	t := c.Theme()
	a.mu.Lock()
	win := a.window
	a.mu.Unlock()
	button := func(name string, glyph *ui.SVG, label string) ui.Element {
		b := ui.ButtonBase(c.Key(key+"-"+name)).Size(winButtonW, winButtonH).Radius(6).Center().Shrink(0).Label(label)
		fg, hover := t.TextMuted, t.SurfaceHover
		if onScrim {
			fg, hover = scrimGlyph, scrimHover
		}
		switch {
		case b.Hovered() && name == "close":
			b.Background(closeHover)
			fg = closeHoverText
		case b.Hovered():
			b.Background(hover)
			if !onScrim {
				fg = t.Text
			}
		}
		b.Children(func() { ui.Icon(c, glyph).Size(winGlyph, winGlyph).TextColor(fg) })
		return b
	}
	if !onScrim {
		ui.Box(c).Size(1, 18).Shrink(0).Margin(0, 6).Background(t.Border)
	}
	if button("min", winMinimize, L.WindowMinimize()).Tooltip(L.WindowMinimize()).Clicked() && win != nil {
		win.Minimize()
	}
	maximized := win != nil && win.IsMaximized()
	glyph, label := winMaximize, L.WindowMaximize()
	if maximized {
		glyph, label = winRestore, L.WindowRestore()
	}
	if button("max", glyph, label).MaximizeButton().Clicked() && win != nil {
		if maximized {
			win.Unmaximize()
		} else {
			win.Maximize()
		}
	}
	if button("close", winClose, L.WindowClose()).Tooltip(L.WindowClose()).Clicked() && win != nil {
		win.Close()
	}
}

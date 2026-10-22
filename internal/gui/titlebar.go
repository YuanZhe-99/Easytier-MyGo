package gui

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// appName is the product name on the title-bar chip; it is a brand and is
// not translated.
const appName = "EasyTier MyGo"

// TitleBarOptions hides the system title bar so the strip drawn by
// titleBar takes its place; the system's window buttons stay, drawn over
// the strip's ends.
// Inputs: o, the window's other options.
// Returns: o with the title-bar fields set.
func TitleBarOptions(o mygo.WindowOptions) mygo.WindowOptions {
	o.TitleBarStyle = mygo.TitleBarHidden
	o.TitleBarHeight = int(titleBarH)
	o.TrafficLightPosition = &mygo.Point{X: int(trafficX), Y: int(trafficY)}
	return o
}

// WatchFullScreen hides the title-bar strip while w is in full screen,
// where the system shows no window buttons and nothing drags the window.
// Side effects: subscribes to w's full-screen events.
func (a *App) WatchFullScreen(w *mygo.Window) {
	set := func(on bool) func() {
		return func() { a.update(func() { a.fullScreen = on }) }
	}
	w.OnEnterFullScreen(set(true))
	w.OnLeaveFullScreen(set(false))
}

// titleBar is the strip across the top of the window that serves as its
// title bar: the app chip, then empty room that drags the window (a
// double-click maximizes it).
// Notes: c.TitleBar() reports the room the system's buttons take (traffic
// lights on the left on macOS, caption buttons on the right on Windows,
// the desktop's choice on Linux); the strip pads past them.
func (a *App) titleBar(c *ui.Context) {
	a.mu.Lock()
	full := a.fullScreen
	a.mu.Unlock()
	if full {
		return
	}
	t := c.Theme()
	room := c.TitleBar()
	ui.Row(c.Key("titlebar")).FillWidth().Shrink(0).Height(max(room.Height, titleBarH)).
		Padding(0, room.Right+8, 0, room.Left+8).AlignItems(ui.Center).Background(t.Background).DragWindow().
		Children(func() {
			ui.Row(c.Key("titlebar-chip")).Gap(6).AlignItems(ui.Center).Padding(4, 10).Radius(6).
				Background(t.Surface).DragWindow().Children(func() {
				if appIcon != nil {
					ui.Image(c, appIcon).Width(chipIconSz).Height(chipIconSz)
				}
				ui.Text(c, appName).FontSize(sizeSmall).NoWrap()
			})
			ui.Box(c.Key("titlebar-drag")).Grow(1).FillHeight().DragWindow()
		})
}

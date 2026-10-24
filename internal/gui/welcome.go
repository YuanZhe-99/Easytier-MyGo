package gui

import (
	"strconv"

	"github.com/egoist/mygo/ui"

	L "github.com/YuanZhe-99/Easytier-MyGo/locales"
)

// Layout of the welcome page.
const (
	welcomeW     float32 = 900 // its column, at most
	welcomeIcon  float32 = 64  // the app's icon over it
	modeTileH    float32 = 196 // a run mode's tile
	modeTileIcon float32 = 36  // the icon's face on it
)

// welcomeView is the first run (doc/en-us/gui.md, "First run"): the app's
// icon and welcome, the three run modes as tiles to choose from, No
// privileges chosen at first, and Continue with the one chosen. A failed
// choice (a cancelled prompt, a failed install) is said above Continue
// until the next attempt.
func (a *App) welcomeView(c *ui.Context) {
	t := c.Theme()
	k := tok(c)
	a.mu.Lock()
	busy, chosen := a.busy["mode"], a.welcomeChoice
	// The welcome page has no toast: a failed choice stays here.
	failure := ""
	if a.toastErr {
		failure = a.toast
	}
	a.mu.Unlock()
	modes := runModes()
	ui.Scroll(c.Key("welcome")).Grow(1).FillWidth().Padding(20, 40, 32, 40).AlignItems(ui.Center).Children(func() {
		ui.Column(c.Key("welcome-col")).FillWidth().MaxWidth(welcomeW).Children(func() {
			if appIcon != nil {
				ui.Image(c, appIcon).Width(welcomeIcon).Height(welcomeIcon)
			}
			ui.Text(c, L.WelcomeTitle()).FontSize(28).Bold().Margin(20, 0, 0, 0)
			ui.Text(c, L.WelcomeBody()).TextColor(t.TextMuted).Margin(8, 0, 0, 0)
			ui.Grid(c.Key("welcome-modes")).Columns(3).Gap(14).FillWidth().Margin(28, 0, 0, 0).Children(func() {
				for _, m := range modes {
					on := m.choice == chosen
					b := ui.ToggleBase(c.Key("welcome-mode-"+strconv.Itoa(int(m.choice))), &on).MinHeight(modeTileH).Radius(cardRadius).Padding(18).Label(m.title).Disabled(busy)
					edge, face := t.Border, t.Background
					if t.Dark {
						face = t.Surface
					}
					switch {
					case m.choice == chosen:
						edge, face = t.Accent, t.Accent.Alpha(0.1)
					case b.Hovered():
						edge = t.TextMuted
					}
					b.Border(1, edge).Background(face)
					if b.Changed() {
						a.update(func() { a.welcomeChoice = m.choice })
					}
					b.Children(func() {
						ui.Column(c).Fill().Gap(10).Children(func() {
							ui.Row(c).FillWidth().AlignItems(ui.Start).Children(func() {
								fg, tile := t.TextMuted, tileBackground(t)
								if m.choice == chosen {
									fg, tile = t.Accent, t.Background
								}
								ui.Box(c).Size(modeTileIcon, modeTileIcon).Radius(9).Center().Background(tile).Children(func() {
									ui.Icon(c, m.icon).Size(18, 18).TextColor(fg)
								})
								ui.Box(c).Grow(1)
								if m.choice == chosen {
									ui.Box(c).Size(20, 20).Radius(10).Center().Background(t.Accent).Children(func() {
										ui.Icon(c, iconCheck).Size(12, 12).TextColor(t.AccentText)
									})
								}
							})
							ui.Text(c, m.title).FontSize(sizeHeading).Bold()
							ui.Text(c, m.body).FontSize(sizeSmall + 1)
							ui.Box(c).Grow(1)
							ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
								ui.Icon(c, m.needIcon).Size(14, 14).TextColor(t.TextMuted)
								ui.Text(c, m.need).FontSize(sizeSmall).TextColor(t.TextMuted)
							})
						})
					})
				}
			})
			if failure != "" && !busy {
				ui.Text(c, failure).FontSize(sizeSmall).TextColor(k.bad).Margin(20, 0, 0, 0)
			}
			ui.Box(c).Height(1).FillWidth().Background(t.Border).Margin(20, 0, 18, 0)
			ui.Row(c.Key("welcome-foot")).FillWidth().Gap(16).AlignItems(ui.Center).Children(func() {
				ui.Text(c, L.WelcomeLater()).FontSize(sizeSmall).TextColor(t.TextMuted).Grow(1).MinWidth(0)
				if busy {
					ui.Spinner(c)
					ui.Text(c, L.ModeSwitching()).FontSize(sizeSmall).TextColor(t.TextMuted)
				}
				title := modes[0].title
				for _, m := range modes {
					if m.choice == chosen {
						title = m.title
					}
				}
				if ui.PrimaryButton(c.Key("welcome-go"), L.WelcomeContinue(title)).Disabled(busy).Clicked() {
					a.chooseMode(chosen)
				}
			})
		})
	})
}

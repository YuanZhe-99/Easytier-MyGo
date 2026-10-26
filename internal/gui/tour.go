package gui

import (
	"strconv"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	L "github.com/YuanZhe-99/Easytier-MyGo/locales"
)

// tourStep is a step of the tour (doc/en-us/gui.md, "First run").
type tourStep int

const (
	tourWelcome tourStep = iota
	tourDevice           // the virtual device, with and without
	tourModes            // the three run modes side by side
	tourStartup          // when networks come up after the computer starts
	tourAwake            // sleep takes the computer offline
	tourReady            // what was chosen; finishing installs the helper if needed
	tourSteps            // how many steps there are
)

// Layout of the tour.
const (
	tourArtW    float32 = 340  // the illustration's column
	tourArtPad  float32 = 16   // around the illustration in it
	tourBodyW   float32 = 540  // the step's column, at most
	tourPadX    float32 = 44   // the steps' side margins
	tourTopH    float32 = 60   // the row of dots, language and Skip
	tourFootH   float32 = 72   // the row of Back and Continue
	tourDot     float32 = 6    // a step's dot
	tourDotOn   float32 = 20   // the current step's dot
	tourIcon    float32 = 56   // the app's icon on the welcome
	tourTitle   float32 = 26   // a step's title
	tourLead    float32 = 15   // the welcome's sentence
	tourSlide   float32 = 28   // how far a step slides in
	tourSkipW   float32 = 320  // the card Skip opens
	tourLabelW  float32 = 132  // the run modes' row labels
	tourMark    float32 = 20   // a fact's round mark
	tourLangW   float32 = 170  // the language picker
	tourSheetW  float32 = 1040 // the tour over the window (from Settings), at most
	tourSheetH  float32 = 680
	tourSheetMW float32 = 720 // and at least
	tourSheetMH float32 = 520
)

// tourPopIn is how the ready step's check appears.
var tourPopIn = ui.CubicBezier(0.3, 1.6, 0.5, 1)

// tour is the tour's state while it shows (App.tour, under App.mu).
type tour struct {
	step    tourStep
	dir     int       // the last move: 1 forward, -1 back
	mode    runChoice // the run mode looked at; the first run's choice
	noTUN   bool      // the device step shows a network without a virtual device
	replay  bool      // opened from Settings: closing it changes nothing
	askSkip bool      // Skip asks before it skips
}

// tourEdit changes the tour's state, if it still shows.
func (a *App) tourEdit(fn func(tr *tour)) {
	a.mu.Lock()
	if a.tour != nil {
		fn(a.tour)
	}
	a.mu.Unlock()
}

// openTour shows the tour again over the window, from Settings › Run mode,
// the run mode in use looked at first.
func (a *App) openTour() {
	a.mu.Lock()
	a.settingsOpen, a.settingsFocus = false, settingsFocus{}
	a.tour = &tour{replay: true, mode: a.currentChoice()}
	a.mu.Unlock()
}

// closeTour closes the tour shown from Settings and goes back there.
func (a *App) closeTour() {
	a.mu.Lock()
	a.tour = nil
	a.settingsOpen, a.settingsAt = true, setRunMode
	a.mu.Unlock()
}

// tourSheet is the tour shown again from Settings: a sheet over the
// window dimmed, as Settings is. Escape, a click outside or Close closes
// it; nothing changes unless its last step switches the run mode.
func (a *App) tourSheet(c *ui.Context) {
	a.mu.Lock()
	show := a.tour != nil && a.tour.replay && a.page != pageWelcome
	a.mu.Unlock()
	if !show {
		return
	}
	t := c.Theme()
	w, h := c.Size()
	open := true
	ui.DialogBase(c, &open, func(back, panel ui.Element) {
		back.Background(sheetScrim).Padding(sheetTop, sheetX, sheetBottom, sheetX)
		panel.Radius(paneRadius).Background(t.Background).Shadow(0, 10, 30, 0, sheetShadow).Clip()
		ui.Column(c.Key("tour-sheet")).
			Width(min(max(w-2*sheetX, tourSheetMW), tourSheetW)).
			Height(min(max(h-sheetTop-sheetBottom, tourSheetMH), tourSheetH)).
			Children(func() { a.tourView(c) })
	})
	if ownWindowButtons {
		ui.Overlay(c, func() {
			ui.Row(c.Key("tour-win")).Absolute().Top(paneMargin + (paneHeaderH-winButtonH)/2).Right(2 * paneMargin).Gap(8).Children(func() {
				a.windowButtons(c, "tour-win", true)
			})
		})
	}
	if !open {
		a.closeTour()
	}
}

// tourView is the tour in the space it has (doc/en-us/gui.md, "First
// run"): the step's illustration on the left; beside it the dots of the
// steps, the language and Skip, the step, then Back and Continue.
func (a *App) tourView(c *ui.Context) {
	t := c.Theme()
	k := tok(c)
	a.mu.Lock()
	if a.tour == nil {
		a.mu.Unlock()
		return
	}
	tr := *a.tour
	busy := a.busy["mode"]
	// The first run has no toast: a failed choice stays on its last step.
	failure := ""
	if a.toastErr && !tr.replay {
		failure = a.toast
	}
	login := a.openAtLogin
	current := a.currentChoice()
	a.mu.Unlock()
	move := func(by int) {
		a.tourEdit(func(tr *tour) {
			tr.step = max(0, min(tr.step+tourStep(by), tourReady))
			tr.dir, tr.askSkip = by, false
		})
	}
	ui.Row(c.Key("tour")).Fill().AlignItems(ui.Stretch).Children(func() {
		a.tourArt(c, tr, login, current)
		ui.Column(c.Key("tour-main")).Grow(1).MinWidth(0).Children(func() {
			ui.Row(c.Key("tour-top")).Height(tourTopH).Shrink(0).Padding(0, tourPadX).Gap(12).AlignItems(ui.Center).Children(func() {
				tourDots(c, tr.step)
				ui.Box(c).Grow(1)
				a.languageSelect(c, "tour-lang", tourLangW)
				if tr.replay {
					if ui.Button(c.Key("tour-close"), L.ActionClose()).Clicked() {
						a.closeTour()
					}
				} else if tr.step != tourReady {
					if ui.Button(c.Key("tour-skip"), L.TourSkip()).Disabled(busy).Clicked() {
						a.tourEdit(func(tr *tour) { tr.askSkip = !tr.askSkip })
					}
				}
			})
			ui.Scroll(c.Key("tour-scroll")).Grow(1).MinHeight(0).FillWidth().Padding(8, tourPadX).Justify(ui.Center).ClipX().Children(func() {
				enter := &ui.Motion{X: tourSlide * float32(tr.dir)}
				if tr.dir == 0 {
					enter = &ui.Motion{Y: 10}
				}
				ui.Column(c.Key("tour-step-" + strconv.Itoa(int(tr.step)))).FillWidth().MaxWidth(tourBodyW).
					Transition(ui.ElementTransition{Duration: 320 * time.Millisecond, Enter: enter, Exit: &ui.Motion{Scale: 0.98}}).
					Children(func() { a.tourStepView(c, tr, login, current, busy) })
			})
			ui.Row(c.Key("tour-foot")).Height(tourFootH).Shrink(0).Padding(0, tourPadX).Gap(10).AlignItems(ui.Center).Children(func() {
				if tr.step > tourWelcome && ui.Button(c.Key("tour-back"), L.TourBack()).Disabled(busy).Clicked() {
					move(-1)
				}
				if failure != "" && !busy && tr.step == tourReady {
					ui.Text(c, failure).FontSize(sizeSmall).TextColor(k.bad).Grow(1).MinWidth(0)
				} else {
					ui.Box(c).Grow(1)
				}
				if busy {
					ui.Spinner(c)
					ui.Text(c, L.ModeSwitching()).FontSize(sizeSmall).TextColor(t.TextMuted)
				}
				label := L.TourNext()
				switch {
				case tr.step == tourWelcome:
					label = L.TourStart()
				case tr.step == tourReady && tr.replay:
					label = L.TourDone()
				case tr.step == tourReady && tr.mode != choiceUser:
					label = L.TourInstallFinish()
				case tr.step == tourReady:
					label = L.TourFinish()
				}
				if ui.PrimaryButton(c.Key("tour-next"), label).Disabled(busy).Clicked() {
					switch {
					case tr.step < tourReady:
						move(1)
					case tr.replay:
						a.closeTour()
					default:
						a.chooseMode(tr.mode)
					}
				}
			})
			if tr.askSkip && !tr.replay {
				a.tourSkipCard(c)
			}
		})
	})
}

// tourDots are the steps as dots, the current one long.
func tourDots(c *ui.Context, at tourStep) {
	t := c.Theme()
	ui.Row(c.Key("tour-dots")).Gap(5).AlignItems(ui.Center).Children(func() {
		for i := range tourSteps {
			dot := ui.Box(c.Key("tour-dot-" + strconv.Itoa(int(i))))
			w, col := tourDot, t.Border
			switch {
			case i == at:
				w, col = tourDotOn, t.Text
			case i < at:
				col = t.TextMuted
			}
			dot.Width(dot.AnimateTo("w", w, ui.Snappy)).Height(tourDot).Radius(tourDot / 2).Background(col)
		}
	})
}

// tourSkipCard asks before Skip: the first run then starts in No
// privileges, which needs no approval.
func (a *App) tourSkipCard(c *ui.Context) {
	t := c.Theme()
	ui.Column(c.Key("tour-skip-card")).Absolute().Top(tourTopH-4).Right(tourPadX-12).Width(tourSkipW).
		Padding(16).Gap(10).Radius(cardRadius).Background(t.Background).Border(1, t.Border).Shadow(0, 12, 32, 0, sheetShadow.Alpha(0.4)).
		Transition(ui.ElementTransition{Enter: &ui.Motion{Y: -6}}).
		Children(func() {
			ui.Text(c, L.TourSkipTitle()).Bold()
			ui.Text(c, L.TourSkipBody()).FontSize(sizeSmall + 1).TextColor(t.TextMuted)
			ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c.Key("tour-skip-keep"), L.TourSkipKeep()).Clicked() {
					a.tourEdit(func(tr *tour) { tr.askSkip = false })
				}
				if ui.PrimaryButton(c.Key("tour-skip-go"), L.TourSkip()).Clicked() {
					a.tourEdit(func(tr *tour) { tr.askSkip = false })
					a.chooseMode(choiceUser)
				}
			})
		})
}

// tourArt is the step's illustration (tourart.go), its values animated
// here as the step's choices change.
func (a *App) tourArt(c *ui.Context, tr tour, login bool, current runChoice) {
	t := c.Theme()
	k := tok(c)
	col := artColors{text: t.Text, muted: t.TextMuted, card: t.Background, edge: t.Border,
		flow: k.download, inbound: k.local, cross: k.bad, accent: t.Accent, good: k.good}
	box := ui.Box(c.Key("tour-art")).Width(tourArtW).Shrink(0).FillHeight().Background(t.Surface).Clip()
	bool1 := func(b bool) float32 {
		if b {
			return 1
		}
		return 0
	}
	barY, barH := float32(136), float32(334) // online from startup
	if tr.mode != choiceBackground {
		barY, barH = 356, 44 // once the app is opened
		if login {
			barY, barH = 316, 84 // once you sign in
		}
	}
	st := artState{
		step:  tr.step,
		tun:   box.AnimateTo("tun", bool1(!tr.noTUN), ui.Smooth),
		mode:  box.AnimateTo("mode", float32(tr.mode), ui.Smooth),
		login: box.AnimateTo("login", bool1(login), ui.Smooth),
		barY:  box.AnimateTo("bar-y", barY, ui.Smooth),
		barH:  box.AnimateTo("bar-h", barH, ui.Smooth),
		wait:  box.AnimateTo("wait", bool1(tr.mode != choiceBackground && !login), ui.Smooth),
		still: c.Preferences().ReduceMotion,
		mode0: tr.mode,
	}
	if tr.replay { // the ready step shows the run mode in use, as its rows do
		st.mode0 = current
	}
	box.Children(func() {
		scene := ui.Box(c.Key("tour-art-" + strconv.Itoa(int(tr.step)))).Absolute().Top(0).Left(0).Right(0).Bottom(0).
			Transition(ui.ElementTransition{Duration: 300 * time.Millisecond, Enter: &ui.Motion{}, Exit: &ui.Motion{}})
		if tr.step == tourReady {
			st.pop = scene.Keyframes("pop", []ui.Keyframe{{Value: 0}, {At: 450 * time.Millisecond, Value: 1, Ease: tourPopIn}},
				ui.KeyframeOptions{Delay: 300 * time.Millisecond})
		}
		s := st
		scene.Draw(func(p *ui.Painter, r ui.Rect) {
			drawArt(p, ui.Rect{X: r.X + tourArtPad, Y: r.Y + tourArtPad, W: r.W - 2*tourArtPad, H: r.H - 2*tourArtPad}, s, col)
		})
	})
}

// tourHeading is a step's title and its sentence.
func tourHeading(c *ui.Context, title, body string) {
	t := c.Theme()
	ui.Text(c, title).FontSize(tourTitle).Bold()
	ui.Text(c, body).TextColor(t.TextMuted).Margin(10, 0, 0, 0)
}

// tourStepView is the content of a step.
func (a *App) tourStepView(c *ui.Context, tr tour, login bool, current runChoice, busy bool) {
	switch tr.step {
	case tourWelcome:
		a.tourWelcomeStep(c)
	case tourDevice:
		a.tourDeviceStep(c, tr)
	case tourModes:
		a.tourModesStep(c, tr, busy)
	case tourStartup:
		a.tourStartupStep(c, tr, login)
	case tourAwake:
		tourAwakeStep(c)
	case tourReady:
		a.tourReadyStep(c, tr, login, current, busy)
	}
}

// tourWelcomeStep: the app, what it does, and what the tour covers.
func (a *App) tourWelcomeStep(c *ui.Context) {
	t := c.Theme()
	if appIcon != nil {
		ui.Image(c, appIcon).Width(tourIcon).Height(tourIcon)
	}
	ui.Text(c, L.WelcomeTitle()).FontSize(sizeTitle+6).Bold().Margin(16, 0, 0, 0)
	ui.Text(c, L.TourWelcomeBody()).FontSize(tourLead).TextColor(t.TextMuted).Margin(10, 0, 0, 0)
	ui.Row(c.Key("tour-topics")).Wrap().Gap(8).Margin(18, 0, 0, 0).Children(func() {
		for _, s := range []string{L.TourTopicDevice(), L.TourTopicModes(), L.TourTopicStartup(), L.TourTopicAwake()} {
			ui.Row(c).Height(26).Padding(0, 11).Radius(999).AlignItems(ui.Center).Background(t.Surface).Children(func() {
				ui.Text(c, s).FontSize(sizeSmall).NoWrap()
			})
		}
	})
	ui.Text(c, L.TourWelcomeNote()).FontSize(sizeSmall).TextColor(t.TextMuted).Margin(16, 0, 0, 0)
}

// tourFact is a line with a mark: yes (1), no (-1) or a note (0).
func tourFact(c *ui.Context, kind int, text string) {
	t := c.Theme()
	k := tok(c)
	icon, ink := iconInfo, t.TextMuted
	switch kind {
	case 1:
		icon, ink = iconCheck, k.good
	case -1:
		icon, ink = iconClose, k.bad
	}
	ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Start).Margin(10, 0, 0, 0).Children(func() {
		ui.Box(c).Size(tourMark, tourMark).Shrink(0).Radius(tourMark / 2).Center().Background(ink.Alpha(0.14)).Children(func() {
			ui.Icon(c, icon).Size(12, 12).TextColor(ink)
		})
		ui.Text(c, text).Grow(1).MinWidth(0)
	})
}

// tourDeviceStep: what a virtual device gives, with a switch to see the
// network without one.
func (a *App) tourDeviceStep(c *ui.Context, tr tour) {
	tourHeading(c, L.TourDeviceTitle(), L.TourDeviceBody())
	idx := 0
	if tr.noTUN {
		idx = 1
	}
	ui.Row(c).Margin(18, 0, 6, 0).Children(func() {
		if ui.Segmented(c.Key("tour-tun"), &idx, L.TourWithDevice(), L.TourWithoutDevice()).Changed() {
			a.tourEdit(func(tr *tour) { tr.noTUN = idx == 1 })
		}
	})
	if tr.noTUN {
		tourFact(c, -1, L.TourDeviceAppsNo())
		tourFact(c, 1, L.TourDeviceInboundNo())
		tourFact(c, 0, L.TourDeviceNoApproval())
		return
	}
	tourFact(c, 1, L.TourDeviceAppsYes())
	tourFact(c, 1, L.TourDeviceInbound())
	tourFact(c, 0, L.TourDeviceNeedsHelper())
}

// tourModesStep: the three run modes as columns to choose from, what sets
// them apart as rows, and what the one chosen suits.
func (a *App) tourModesStep(c *ui.Context, tr tour, busy bool) {
	t := c.Theme()
	tourHeading(c, L.TourModesTitle(), L.TourModesBody())
	tint := t.Accent.Alpha(0.1)
	lines := []string{L.TourModeUserLine(), L.TourModeFollowLine(), L.TourModeBackgroundLine()}
	ui.Row(c.Key("tour-mode-heads")).FillWidth().Gap(6).AlignItems(ui.Stretch).Margin(20, 0, 0, 0).Children(func() {
		ui.Box(c).Width(tourLabelW).Shrink(0)
		for i, m := range runModes() {
			on := m.choice == tr.mode
			b := ui.ToggleBase(c.Key("tour-mode-"+strconv.Itoa(i)), &on).Grow(1).Basis(0).MinWidth(0).Padding(10).
				Radius(10, 10, 0, 0).Label(m.title).Disabled(busy)
			edge, face, name := ui.Transparent, t.Surface, t.Text
			switch {
			case m.choice == tr.mode:
				edge, face, name = t.Accent, tint, t.Accent
			case b.Hovered():
				face = t.SurfaceHover
			}
			b.Background(face).BorderWidth(3, 0, 0, 0).BorderColor(edge)
			if b.Changed() {
				a.tourEdit(func(tr *tour) { tr.mode = m.choice })
			}
			b.Children(func() {
				ui.Column(c).FillWidth().Gap(3).Children(func() {
					ui.Text(c, m.title).Bold().TextColor(name)
					ui.Text(c, lines[i]).FontSize(sizeSmall).TextColor(t.TextMuted)
				})
			})
		}
	})
	rows := [][4]string{
		{L.TourRowDevice(), L.TourDeviceNo(), L.TourDeviceYes(), L.TourDeviceYes()},
		{L.TourRowApps(), L.TourAppsNo(), L.TourAppsYes(), L.TourAppsYes()},
		{L.TourRowWhen(), L.TourWhenOpen(), L.TourWhenOpen(), L.TourWhenStartup()},
		{L.TourRowApproval(), L.TourApprovalNone(), L.TourApprovalOnce(), L.TourApprovalOnce()},
	}
	for ri, row := range rows {
		ui.Row(c.Key("tour-mode-row-" + strconv.Itoa(ri))).FillWidth().Gap(6).AlignItems(ui.Stretch).Children(func() {
			ui.Box(c).Width(tourLabelW).Shrink(0).Padding(8, 0).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
				ui.Text(c, row[0]).FontSize(sizeSmall).TextColor(t.TextMuted)
			})
			for i := range 3 {
				cell := ui.Box(c).Grow(1).Basis(0).MinWidth(0).Padding(8, 10).BorderWidth(1, 0, 0, 0).BorderColor(t.Border)
				txt := t.Text
				if runChoice(i) == tr.mode {
					cell.Background(tint)
					if ri == len(rows)-1 {
						cell.Radius(0, 0, 10, 10)
					}
				}
				cell.Children(func() {
					tx := ui.Text(c, row[i+1]).FontSize(sizeSmall + 1).TextColor(txt)
					if runChoice(i) == tr.mode {
						tx.Bold()
					}
				})
			}
		})
	}
	note := []string{L.TourModeUserNote(), L.TourModeFollowNote(), L.TourModeBackgroundNote()}[tr.mode]
	ui.Text(c, note).FontSize(sizeSmall+1).Margin(14, 0, 0, 0)
}

// tourStartupStep: when the networks come up after the computer starts in
// the run mode looked at, and the open-at-login switch.
func (a *App) tourStartupStep(c *ui.Context, tr tour, login bool) {
	t := c.Theme()
	if tr.mode == choiceBackground {
		tourHeading(c, L.TourStartupSvcTitle(), L.TourStartupSvcBody())
	} else {
		tourHeading(c, L.TourStartupAppTitle(), L.TourStartupAppBody(modeTitle(tr.mode)))
	}
	sub := L.TourLoginOff()
	switch {
	case tr.mode == choiceBackground:
		sub = L.TourLoginSvc()
	case login:
		sub = L.TourLoginOn()
	}
	ui.Row(c.Key("tour-login-row")).FillWidth().Gap(12).Padding(12, 14).Radius(cardRadius).Border(1, t.Border).AlignItems(ui.Center).Margin(18, 0, 0, 0).Children(func() {
		ui.Box(c).Size(tileSize, tileSize).Shrink(0).Radius(8).Center().Background(tileBackground(t)).Children(func() {
			ui.Icon(c, iconPower).Size(16, 16).TextColor(t.TextMuted)
		})
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, L.SettingsLaunchAtLogin()).Bold()
			ui.Text(c, sub).FontSize(sizeSmall).TextColor(t.TextMuted)
		})
		on := login
		if ui.Switch(c.Key("tour-login"), &on).Label(L.SettingsLaunchAtLogin()).Changed() {
			a.setOpenAtLogin(on)
		}
	})
	if tr.mode != choiceBackground {
		ui.Row(c.Key("tour-use-svc")).Gap(6).AlignItems(ui.Center).Margin(14, 0, 0, 0).Children(func() {
			ui.Text(c, L.TourUseBackgroundAsk()).FontSize(sizeSmall + 1)
			b := ui.ButtonBase(c.Key("tour-use-svc-btn")).Label(L.TourUseBackground())
			b.Children(func() {
				ui.Text(c, L.TourUseBackground()).FontSize(sizeSmall + 1).TextColor(t.Accent).Underline()
			})
			if b.Clicked() {
				a.tourEdit(func(tr *tour) { tr.mode = choiceBackground })
			}
		})
	}
}

// tourAwakeStep: sleep takes the computer offline in every run mode, and
// where this system keeps it awake.
func tourAwakeStep(c *ui.Context) {
	t := c.Theme()
	tourHeading(c, L.TourAwakeTitle(), L.TourAwakeBody())
	tips := []string{L.TourAwakeLinux1(), L.TourAwakeLinux2()}
	switch artPlatform {
	case "windows":
		tips = []string{L.TourAwakeWin1(), L.TourAwakeWin2()}
	case "darwin":
		tips = []string{L.TourAwakeMac1(), L.TourAwakeMac2()}
	}
	for i, tip := range tips {
		ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Start).Margin(12, 0, 0, 0).Children(func() {
			ui.Box(c).Size(tourMark, tourMark).Shrink(0).Radius(tourMark / 2).Center().Background(t.Surface).Children(func() {
				ui.Text(c, strconv.Itoa(i+1)).FontSize(sizeSmall).Bold()
			})
			ui.Text(c, tip).Grow(1).MinWidth(0)
		})
	}
	ui.Row(c.Key("tour-power")).Gap(12).AlignItems(ui.Center).Margin(18, 0, 0, 0).Children(func() {
		if url := powerSettingsURL(artPlatform); url != "" {
			if ui.Button(c.Key("tour-power-btn"), L.TourOpenPower()).Clicked() {
				go func() { _ = mygo.Shell.OpenExternal(url) }()
			}
		}
		ui.Text(c, L.TourAwakeScreen()).FontSize(sizeSmall).TextColor(t.TextMuted)
	})
}

// powerSettingsURL opens the system's power settings, or "" where there is
// no one place for them (Linux desktops differ).
func powerSettingsURL(goos string) string {
	switch goos {
	case "windows":
		return "ms-settings:powersleep"
	case "darwin":
		return "x-apple.systempreferences:com.apple.Battery-Settings.extension"
	}
	return ""
}

// tourReadyStep: what was chosen and what finishing does; shown again from
// Settings, the run mode in use and, when another was looked at, Switch.
func (a *App) tourReadyStep(c *ui.Context, tr tour, login bool, current runChoice, busy bool) {
	shown := tr.mode
	switch {
	case tr.replay && tr.mode != current:
		shown = current
		tourHeading(c, L.TourReplayTitle(), L.TourReplayOther(modeTitle(current), modeTitle(tr.mode)))
	case tr.replay:
		shown = current
		tourHeading(c, L.TourReplayTitle(), L.TourReplayCurrent(modeTitle(current)))
	case tr.mode == choiceUser:
		tourHeading(c, L.TourReadyTitle(), L.TourReadyUser())
	default:
		auth := L.TourReadyAuthLinux()
		switch artPlatform {
		case "windows":
			auth = L.TourReadyAuthWin()
		case "darwin":
			auth = L.TourReadyAuthMac()
		}
		tourHeading(c, L.TourReadyTitle(), auth)
	}
	device, when, opens := L.TourDeviceNo(), L.TourWhenOpen(), L.TourOff()
	if shown != choiceUser {
		device = L.TourDeviceYes()
	}
	if shown == choiceBackground {
		when = L.TourWhenStartup()
	}
	if login {
		opens = L.TourOn()
	}
	r := &rows{}
	ui.Column(c).FillWidth().Margin(14, 0, 0, 0).Children(func() {
		r.section(c, "tour-sum", L.TourSummary(), nil, func(r *rows) {
			r.row(c, "tour-sum-mode", iconShield, L.SettingsRunMode(), modeTitle(shown), nil)
			r.row(c, "tour-sum-device", iconNetwork, L.TourRowDevice(), device, nil)
			r.row(c, "tour-sum-when", iconActivity, L.TourRowWhen(), when, nil)
			r.row(c, "tour-sum-login", iconPower, L.SettingsLaunchAtLogin(), opens, nil)
		})
	})
	if tr.replay && tr.mode != current {
		ui.Row(c).Margin(16, 0, 0, 0).Children(func() {
			if ui.Button(c.Key("tour-switch"), L.TourSwitchTo(modeTitle(tr.mode))).Disabled(busy).Clicked() {
				a.closeTour()
				a.chooseMode(tr.mode)
			}
		})
	}
}

// modeTitle is a run mode's name.
func modeTitle(m runChoice) string {
	for _, r := range runModes() {
		if r.choice == m {
			return r.title
		}
	}
	return ""
}

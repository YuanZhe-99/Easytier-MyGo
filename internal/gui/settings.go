package gui

import (
	_ "embed"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/localapi"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/version"
	L "github.com/YuanZhe-99/Easytier-MyGo/locales"
	"github.com/YuanZhe-99/Easytier-MyGo/third_party"
)

// appName is the product's name, a brand that is not translated.
const appName = "EasyTier MyGo"

//go:embed assets/icon.png
var appIconPNG []byte

// appIcon is the app's icon for the welcome page (nil if undecodable).
var appIcon, _ = ui.DecodeBitmap(appIconPNG)

// coresState is the core manager's view state.
type coresState struct {
	loaded   bool
	cores    []localapi.CoreInfo
	releases []localapi.ReleaseInfo
	checked  bool
}

// settingsPage is a page of the Settings sheet.
type settingsPage int

const (
	setGeneral settingsPage = iota
	setRunMode
	setCore
	setAbout
)

// settingsItem is a page in the sheet's sections.
type settingsItem struct {
	page  settingsPage
	icon  *ui.SVG
	label string
}

// settingsGroup is a group of the sheet's sections.
type settingsGroup struct {
	title string
	items []settingsItem
}

// settingsGroups are the sheet's sections (doc/en-us/gui.md, "Settings"):
// the app's preferences, this computer, and the app itself.
func settingsGroups() []settingsGroup {
	return []settingsGroup{
		{L.SettingsGroupPreferences(), []settingsItem{{setGeneral, iconSliders, L.SettingsGeneral()}}},
		{L.SettingsGroupComputer(), []settingsItem{
			{setRunMode, iconShield, L.SettingsRunMode()},
			{setCore, iconCPU, L.SettingsCores()},
		}},
		{appName, []settingsItem{{setAbout, iconInfo, L.SettingsAboutTitle()}}},
	}
}

// pageName is a Settings page's name in its sections.
func pageName(p settingsPage) string {
	for _, g := range settingsGroups() {
		for _, it := range g.items {
			if it.page == p {
				return it.label
			}
		}
	}
	return ""
}

// openSettings opens the Settings sheet at a page.
func (a *App) openSettings(p settingsPage) {
	a.update(func() {
		a.settingsOpen, a.settingsAt, a.settingsFocus = true, p, settingsFocus{}
	})
}

// settingsSheet is Settings (doc/en-us/gui.md, "Settings"): the top layer,
// a sheet over the whole window, sidebar included, over the window dimmed;
// its sections on the left under a search field, the page chosen beside
// them. Escape, a click outside or × closes it. The window's buttons stay
// where the pane's header holds them, over the dimming.
func (a *App) settingsSheet(c *ui.Context) {
	a.mu.Lock()
	open, pg := a.settingsOpen, a.settingsAt
	a.mu.Unlock()
	if !open {
		return
	}
	t := c.Theme()
	w, h := c.Size()
	ui.DialogBase(c, &open, func(back, panel ui.Element) {
		back.Background(sheetScrim).Padding(sheetTop, sheetX, sheetBottom, sheetX)
		panel.Radius(paneRadius).Background(t.Background).Shadow(0, 10, 30, 0, sheetShadow).Clip()
		ui.Column(c.Key("sheet")).Width(max(w-2*sheetX, 560)).Height(max(h-sheetTop-sheetBottom, 400)).Children(func() {
			ui.Row(c.Key("sheet-head")).Height(sheetHeaderH).Shrink(0).Padding(0, 12, 0, 20).AlignItems(ui.Center).Children(func() {
				ui.Text(c, L.PageSettings()).FontSize(sizeHeading).Bold().Grow(1)
				if iconButton(c, "sheet-close", iconClose, L.ActionClose(), false) {
					open = false
				}
			})
			ui.Divider(c)
			ui.Row(c.Key("sheet-body")).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
				ui.Scroll(c.Key("sheet-nav")).Width(settingsNavW).Shrink(0).Padding(14, 12).Gap(2).Children(func() {
					a.settingsNav(c, pg)
				})
				ui.Box(c).Width(1).Shrink(0).Background(t.Border)
				ui.Column(c.Key("sheet-page")).Grow(1).MinWidth(0).Children(func() { a.settingsPageView(c, pg) })
			})
		})
	})
	if ownWindowButtons {
		ui.Overlay(c, func() {
			ui.Row(c.Key("sheet-win")).Absolute().Top(paneMargin + (paneHeaderH-winButtonH)/2).Right(2 * paneMargin).Gap(8).Children(func() {
				a.windowButtons(c, "sheet-win", true)
			})
		})
	}
	if !open {
		a.mu.Lock()
		a.settingsOpen, a.settingsFocus = false, settingsFocus{}
		a.mu.Unlock()
	}
}

// settingsPageView draws one page of the sheet.
func (a *App) settingsPageView(c *ui.Context, pg settingsPage) {
	switch pg {
	case setRunMode:
		a.runModePage(c)
	case setCore:
		a.corePage(c)
	case setAbout:
		a.aboutPage(c)
	default:
		a.generalPage(c)
	}
}

// settingsNav is the sheet's navigation: the search field, then the
// sections, or while it holds words the settings they find, each with its
// page.
func (a *App) settingsNav(c *ui.Context, pg settingsPage) {
	t := c.Theme()
	a.mu.Lock()
	q := a.settingsQuery
	a.mu.Unlock()
	ui.Row(c.Key("sheet-search")).Height(searchH).Shrink(0).Gap(8).Padding(0, 10).Radius(9).Border(1, t.Border).AlignItems(ui.Center).Margin(0, 0, 6, 0).Children(func() {
		ui.Icon(c, iconSearch).Size(14, 14).TextColor(t.TextMuted)
		in := ui.TextInputBase(c.Key("sheet-search-in"), &q).Grow(1).Placeholder(L.SettingsSearch()).Label(L.SettingsSearch())
		if in.Changed() {
			a.mu.Lock()
			a.settingsQuery = q
			a.mu.Unlock()
		}
	})
	if strings.TrimSpace(q) != "" {
		hits := searchSettings(settingsHits(), q)
		if len(hits) == 0 {
			ui.Text(c, L.SettingsSearchNone()).FontSize(sizeSmall).TextColor(t.TextMuted).Padding(10)
		}
		for i, h := range hits {
			row := ui.ButtonBase(c.Key("sheet-hit-"+strconv.Itoa(i))).FillWidth().Radius(8).Padding(6, 10).Label(h.title)
			if row.Hovered() {
				row.Background(t.SurfaceHover)
			}
			row.Children(func() {
				ui.Column(c).FillWidth().Gap(2).Children(func() {
					ui.Text(c, h.title)
					ui.Text(c, pageName(h.page)).FontSize(sizeSmall).TextColor(t.TextMuted).SingleLine()
				})
			})
			if row.Clicked() {
				a.mu.Lock()
				a.settingsAt, a.settingsFocus = h.page, settingsFocus{title: h.title, until: time.Now().Add(focusTime)}
				a.mu.Unlock()
			}
		}
		return
	}
	for gi, g := range settingsGroups() {
		ui.Text(c, g.title).FontSize(sizeSmall).Bold().TextColor(t.TextMuted).Padding(12, 10, 4, 10)
		for _, it := range g.items {
			row := ui.ButtonBase(c.Key("sheet-nav-"+strconv.Itoa(gi)+"-"+strconv.Itoa(int(it.page)))).Height(navRowH).FillWidth().Radius(8).Padding(0, 10).Label(it.label)
			fg := t.TextMuted
			switch {
			case it.page == pg:
				row.Background(t.SurfaceHover)
				fg = t.Text
			case row.Hovered():
				row.Background(t.SurfaceHover.Alpha(0.6))
			}
			row.Children(func() {
				ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, it.icon).Size(navIconSize, navIconSize).TextColor(fg)
					txt := ui.Text(c, it.label).TextColor(fg).SingleLine()
					if it.page == pg {
						txt.Bold()
					}
				})
			})
			if row.Clicked() {
				a.mu.Lock()
				a.settingsAt, a.settingsFocus = it.page, settingsFocus{}
				a.mu.Unlock()
			}
		}
	}
}

// settingsFocus is the setting a search hit leads to: its title, until
// when its row stays tinted, and whether its page has scrolled it into
// view yet.
type settingsFocus struct {
	title string
	until time.Time
	shown bool
}

// focusTime is how long a setting found by search stays tinted.
const focusTime = 2 * time.Second

// focused reports whether a row or section is the one a search hit leads
// to and still tinted, and whether it is to scroll into view in this frame
// (only once).
func (a *App) focused(title string) (tint, scroll bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	f := &a.settingsFocus
	if f.title == "" || f.title != title {
		return false, false
	}
	scroll = !f.shown
	f.shown = true
	return time.Now().Before(f.until), scroll
}

// settingsRows is a page's rows, tinting what a search leads to.
func (a *App) settingsRows(c *ui.Context) *rows {
	a.mu.Lock()
	f := a.settingsFocus
	a.mu.Unlock()
	if f.title != "" && time.Now().Before(f.until) {
		c.After(time.Until(f.until))
	}
	return &rows{hit: a.focused}
}

// settingsHit is a setting search finds: the page it is on, its title (a
// row's or a section's), and more words that find it.
type settingsHit struct {
	page  settingsPage
	title string
	words string
}

// settingsHits are the settings search finds, each a row or section title
// of its page (TestSettingsHitsShow).
func settingsHits() []settingsHit {
	join := func(words ...string) string { return strings.Join(words, " ") }
	hits := []settingsHit{
		{setGeneral, L.SettingsLanguage(), L.SettingsLanguageAbout()},
		{setGeneral, L.SettingsTheme(), join(L.SettingsThemeAbout(), L.ThemeSystem(), L.ThemeLight(), L.ThemeDark())},
		{setGeneral, L.SettingsLaunchAtLogin(), L.SettingsLaunchAtLoginHint()},
		{setGeneral, L.SettingsStartHidden(), L.SettingsStartHiddenAbout()},
		{setGeneral, L.SettingsCloseButton(), join(L.SettingsCloseAbout(), L.CloseHide(), L.CloseQuit())},
		{setGeneral, L.NotifyConnections(), join(L.SettingsNotifications(), L.NotifyConnectionsAbout())},
		{setRunMode, L.ModeUserTitle(), L.ModeUserBody()},
		{setRunMode, L.ModeFollowTitle(), L.ModeFollowBody()},
		{setRunMode, L.ModeBackgroundTitle(), L.ModeBackgroundBody()},
		{setRunMode, L.TourRewatchTitle(), join(L.TourRewatchAbout(), L.TourRewatch())},
		{setRunMode, L.SectionHelper(), join(L.ActionUpdateHelper(), L.ActionUninstallHelper())},
		{setCore, L.SectionInstalled(), join(L.SettingsCores(), L.CoreBundled(), L.ActionUseCore())},
		{setCore, L.SettingsCheckUpdates(), L.SettingsCheckUpdatesHint()},
		{setCore, L.CoresAvailable(), join(L.ActionCheckReleases(), L.CoresShowPre())},
		{setAbout, L.AboutLicenseTitle(), join(L.AboutLicense(), L.AboutSource())},
		{setAbout, L.DiagFile(), join(L.ActionExportDiagnostics(), L.SettingsAdvancedHint())},
		{setAbout, L.DiagLogs(), join(L.ActionOpenLogs(), L.DiagLogsAbout())},
	}
	if runtime.GOOS == "darwin" {
		hits = append(hits, settingsHit{setGeneral, L.SettingsHideDock(), L.SettingsHideDockHint()})
	}
	if mygo.Updater.Enabled() {
		hits = append(hits, settingsHit{setAbout, L.SettingsCheckAppUpdates(), join(L.ActionCheckAppUpdate(), L.SectionUpdates())})
	}
	return hits
}

// searchSettings lists the settings whose title or words hold every word of
// q, ignoring case, titles that match first.
func searchSettings(all []settingsHit, q string) []settingsHit {
	words := strings.Fields(strings.ToLower(q))
	var byTitle, byWords []settingsHit
	for _, h := range all {
		title, more := strings.ToLower(h.title), strings.ToLower(h.words)
		inTitle, inAny := true, true
		for _, w := range words {
			inTitle = inTitle && strings.Contains(title, w)
			inAny = inAny && (strings.Contains(title, w) || strings.Contains(more, w))
		}
		switch {
		case inTitle:
			byTitle = append(byTitle, h)
		case inAny:
			byWords = append(byWords, h)
		}
	}
	return slices.Concat(byTitle, byWords)
}

// languageSelect picks the interface language: the system's, or one of the
// catalogs'. Settings › General and the tour show it.
// Side effects: switches the locale and remembers the choice.
func (a *App) languageSelect(c *ui.Context, key string, width float32) {
	langs := mygo.App.Languages()
	options := []string{L.LanguageSystem()}
	for _, l := range langs {
		options = append(options, i18n.Autonym(l))
	}
	cur := options[0]
	a.mu.Lock()
	chosen := a.prefs.Language
	a.mu.Unlock()
	if i := slices.Index(langs, chosen); i >= 0 {
		cur = options[i+1]
	}
	if ui.Select(c.Key(key), &cur, options).Width(width).Label(L.SettingsLanguage()).Changed() {
		i := slices.Index(options, cur)
		lang := ""
		if i > 0 {
			lang = langs[i-1]
		}
		mygo.App.SetLocale(lang)
		a.mu.Lock()
		p := a.prefs
		a.mu.Unlock()
		p.Language = lang
		a.setPrefs(p)
	}
}

// setOpenAtLogin turns the login item on or off (main thread), as Settings
// › General and the tour's startup step do.
// Side effects: a toast when the system refuses.
func (a *App) setOpenAtLogin(on bool) {
	if err := mygo.App.SetOpenAtLogin(on); err != nil {
		a.showToast(err.Error(), true)
		return
	}
	a.mu.Lock()
	a.openAtLogin = on
	a.mu.Unlock()
}

// generalPage: language and look, start and close, notifications.
func (a *App) generalPage(c *ui.Context) {
	r := a.settingsRows(c)
	pageColumn(c, "set-general", pageColumnW, func() {
		pageIntro(c, L.SettingsGeneral(), L.SettingsGeneralAbout())
		r.section(c, "look", L.SectionLook(), nil, func(r *rows) {
			a.mu.Lock()
			themeIdx := max(slices.Index([]string{"system", "light", "dark"}, a.prefs.Theme), 0)
			a.mu.Unlock()
			r.row(c, "lang", iconLanguage, L.SettingsLanguage(), L.SettingsLanguageAbout(), func() {
				a.languageSelect(c, "lang-select", 200)
			})
			r.row(c, "theme", iconSunMoon, L.SettingsTheme(), L.SettingsThemeAbout(), func() {
				if ui.Segmented(c.Key("theme-seg"), &themeIdx, L.ThemeSystem(), L.ThemeLight(), L.ThemeDark()).Changed() {
					a.setTheme([]string{"system", "light", "dark"}[themeIdx])
				}
			})
		})
		a.mu.Lock()
		login := a.openAtLogin
		p := a.prefs
		a.mu.Unlock()
		changed := false
		r.section(c, "start", L.SectionStart(), nil, func(r *rows) {
			r.row(c, "login", iconPower, L.SettingsLaunchAtLogin(), L.SettingsLaunchAtLoginHint(), func() {
				if ui.Switch(c.Key("login-sw"), &login).Label(L.SettingsLaunchAtLogin()).Changed() {
					a.setOpenAtLogin(login)
				}
			})
			r.row(c, "hidden", iconTray, L.SettingsStartHidden(), L.SettingsStartHiddenAbout(), func() {
				changed = ui.Switch(c.Key("hidden-sw"), &p.StartHidden).Label(L.SettingsStartHidden()).Changed() || changed
			})
			closeIdx := 0
			if p.CloseQuits {
				closeIdx = 1
			}
			r.row(c, "close", iconXSquare, L.SettingsCloseButton(), L.SettingsCloseAbout(), func() {
				if ui.Segmented(c.Key("close-seg"), &closeIdx, L.CloseHide(), L.CloseQuit()).Changed() {
					p.CloseQuits = closeIdx == 1
					changed = true
				}
			})
			if runtime.GOOS == "darwin" && !p.CloseQuits {
				r.row(c, "dock", iconDock, L.SettingsHideDock(), L.SettingsHideDockHint(), func() {
					changed = ui.Switch(c.Key("dock-sw"), &p.HideDock).Label(L.SettingsHideDock()).Changed() || changed
				})
			}
		})
		r.section(c, "notify", L.SettingsNotifications(), nil, func(r *rows) {
			r.row(c, "notify", iconBell, L.NotifyConnections(), L.NotifyConnectionsAbout(), func() {
				changed = ui.Switch(c.Key("notify-sw"), &p.Notify).Label(L.NotifyConnections()).Changed() || changed
			})
		})
		if changed {
			a.setPrefs(p)
		}
	})
}

// setPrefs stores the GUI preferences.
func (a *App) setPrefs(p Prefs) {
	a.mu.Lock()
	a.prefs = p
	a.mu.Unlock()
	if err := savePrefs(p); err != nil {
		a.showToast(err.Error(), true)
	}
}

// setTheme applies and remembers the appearance.
func (a *App) setTheme(theme string) {
	a.mu.Lock()
	p := a.prefs
	a.mu.Unlock()
	p.Theme = theme
	a.setPrefs(p)
	ApplyTheme(theme)
}

// ApplyTheme sets the app's appearance ("system", "light", "dark").
func ApplyTheme(theme string) {
	switch theme {
	case "light":
		mygo.Theme.SetSource(mygo.ThemeLight)
	case "dark":
		mygo.Theme.SetSource(mygo.ThemeDark)
	default:
		mygo.Theme.SetSource(mygo.ThemeSystem)
	}
}

// runMode is a run mode as Settings and the welcome page offer it.
type runMode struct {
	choice            runChoice
	icon              *ui.SVG
	title, body, need string
	needIcon          *ui.SVG
}

// runModes are the three run modes, in order.
func runModes() []runMode {
	return []runMode{
		{choiceUser, iconUser, L.ModeUserTitle(), L.ModeUserBody(), L.ModeUserNeed(), iconCheck},
		{choiceFollow, iconLink, L.ModeFollowTitle(), L.ModeFollowBody(), L.ModeFollowNeed(), iconShield},
		{choiceBackground, iconServer, L.ModeBackgroundTitle(), L.ModeBackgroundBody(), L.ModeBackgroundNeed(), iconShield},
	}
}

// runModePage: the three run modes as rows, the one in use marked and the
// others with Switch; then the privileged helper.
func (a *App) runModePage(c *ui.Context) {
	t := c.Theme()
	r := a.settingsRows(c)
	a.mu.Lock()
	current, busy, st, outdated := a.currentChoice(), a.busy["mode"], a.helperSt, a.helperOutdated
	a.mu.Unlock()
	pageColumn(c, "set-mode", pageColumnW, func() {
		pageIntro(c, L.SettingsRunMode(), L.SettingsRunModeHint())
		r.section(c, "modes", L.SectionRunModes(), nil, func(r *rows) {
			for _, m := range runModes() {
				r.row(c, "mode-"+strconv.Itoa(int(m.choice)), m.icon, m.title, m.body+" "+m.need, func() {
					if m.choice == current {
						pill(c, L.ModeCurrent(), t.Accent)
						return
					}
					if ui.Button(c.Key("mode-switch-"+strconv.Itoa(int(m.choice))), L.ActionSwitch()).Disabled(busy).Clicked() {
						a.chooseMode(m.choice)
					}
				})
			}
			r.row(c, "mode-tour", iconPlay, L.TourRewatchTitle(), L.TourRewatchAbout(), func() {
				if ui.Button(c.Key("mode-tour-go"), L.TourRewatch()).Disabled(busy).Clicked() {
					a.openTour()
				}
			})
		})
		ui.Row(c.Key("mode-note")).Gap(8).AlignItems(ui.Center).Margin(10, 4, 0, 4).Children(func() {
			if busy {
				ui.Spinner(c)
				ui.Text(c, L.ModeSwitching()).FontSize(sizeSmall).TextColor(t.TextMuted)
				return
			}
			ui.Text(c, L.RunModeSwitchNote()).FontSize(sizeSmall).TextColor(t.TextMuted)
		})
		r.section(c, "helper", L.SectionHelper(), nil, func(r *rows) {
			if !st.Installed {
				r.row(c, "helper", iconShield, L.HelperNotInstalled(), L.HelperNotInstalledAbout(), nil)
				return
			}
			r.row(c, "helper", iconShield, L.HelperInstalled(st.Version), L.HelperAbout(), func() {
				if outdated && ui.PrimaryButton(c.Key("helper-update"), L.ActionUpdateHelper()).Disabled(busy).Clicked() {
					a.reinstallHelper()
				}
				if ui.Button(c.Key("helper-remove"), L.ActionUninstallHelper()).Disabled(busy).Clicked() {
					a.uninstallHelper(false)
				}
			})
		})
	})
}

// pill is a small rounded label in a color's tint.
func pill(c *ui.Context, text string, col ui.Color) {
	ui.Row(c).Height(22).Padding(0, 9).Radius(999).AlignItems(ui.Center).Background(col.Alpha(0.14)).Shrink(0).Children(func() {
		ui.Text(c, text).FontSize(sizeSmall).Bold().TextColor(col).NoWrap()
	})
}

// corePage: the cores installed, the daily check, and the official
// releases with Download.
func (a *App) corePage(c *ui.Context) {
	t := c.Theme()
	k := tok(c)
	r := a.settingsRows(c)
	a.mu.Lock()
	cs, connected, pre, busy := a.coresView, a.connected, a.prefs.ShowPrereleases, a.busy["cores"]
	upd, auto := a.coreUpdate, a.prefs.CheckCoreUpdates
	a.mu.Unlock()
	if connected && !cs.loaded && !busy {
		a.loadCores(false)
	}
	pageColumn(c, "set-core", pageColumnW, func() {
		pageIntro(c, L.SettingsCores(), L.SettingsCoresHint())
		if upd != "" {
			ui.Box(c).Height(14)
			callout(c, "core-update", t.Accent, iconArrowUp, L.CoreUpdateFound(upd), nil)
		}
		r.section(c, "installed", L.SectionInstalled(), nil, func(r *rows) {
			if len(cs.cores) == 0 {
				r.row(c, "cores-none", iconCPU, L.CoresWorking(), "", nil)
			}
			for _, core := range cs.cores {
				about := L.CoreDownloadedAbout()
				if core.Source == "bundled" {
					about = L.CoreBundledAbout()
				}
				r.row(c, "core-"+core.Tag, iconCPU, core.Tag, about, func() {
					if core.Tested {
						pill(c, L.CoreTested(), k.good)
					} else {
						pill(c, L.CoreUntested(), k.warn)
					}
					if core.Pre {
						pill(c, L.CorePrerelease(), k.relay)
					}
					if core.Selected {
						pill(c, L.CoreInUse(), t.Accent)
						return
					}
					if ui.Button(c.Key("use-"+core.Tag), L.ActionUseCore()).Disabled(busy).Clicked() {
						a.selectCore(core.Tag)
					}
					if core.Source != "bundled" && ui.Button(c.Key("rm-"+core.Tag), L.ActionRemove()).Disabled(busy).Clicked() {
						a.coreRequest(localapi.Request{Type: localapi.RequestCoreRemove, Tag: core.Tag})
					}
				})
			}
		})
		r.section(c, "core-updates", L.SectionUpdates(), nil, func(r *rows) {
			r.row(c, "core-auto", iconRefresh, L.SettingsCheckUpdates(), L.SettingsCheckUpdatesHint(), func() {
				if ui.Switch(c.Key("core-auto-sw"), &auto).Label(L.SettingsCheckUpdates()).Changed() {
					a.mu.Lock()
					p := a.prefs
					a.mu.Unlock()
					p.CheckCoreUpdates = auto
					a.setPrefs(p)
				}
			})
		})
		r.section(c, "releases", L.CoresAvailable(), func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				if ui.Switch(c.Key("core-pre"), &pre).Label(L.CoresShowPre()).Changed() {
					a.mu.Lock()
					p := a.prefs
					a.mu.Unlock()
					p.ShowPrereleases = pre
					a.setPrefs(p)
					a.loadCores(true)
				}
				ui.Text(c, L.CoresShowPre()).FontSize(sizeSmall)
			})
			if ui.Button(c.Key("core-check"), L.ActionCheckReleases()).Disabled(busy || !connected).Clicked() {
				a.loadCores(true)
			}
		}, func(r *rows) {
			switch {
			case busy:
				r.row(c, "rel-busy", iconDownload, L.CoresWorking(), "", func() { ui.Spinner(c) })
			case !cs.checked:
				r.row(c, "rel-unchecked", iconDownload, L.CoresUnchecked(), "", nil)
			case len(cs.releases) == 0:
				r.row(c, "rel-none", iconDownload, L.CoresNone(), "", nil)
			}
			if !cs.checked {
				return
			}
			for _, rel := range cs.releases {
				r.row(c, "rel-"+rel.Tag, iconDownload, rel.Tag, L.CorePublished(rel.Published.Format("2006-01-02")), func() {
					if rel.Prerelease {
						pill(c, L.CorePrerelease(), k.relay)
					}
					if rel.Tested {
						pill(c, L.CoreTested(), k.good)
					}
					if rel.Installed {
						ui.Text(c, L.CoreInstalled()).FontSize(sizeSmall).TextColor(t.TextMuted)
						return
					}
					if ui.Button(c.Key("dl-"+rel.Tag), L.ActionDownload(formatBytes(float64(rel.Size)))).Disabled(busy).Clicked() {
						a.coreRequest(localapi.Request{Type: localapi.RequestCoreInstall, Tag: rel.Tag})
					}
				})
			}
		})
	})
}

// loadCores reads the installed cores and, when asked, the releases.
func (a *App) loadCores(releases bool) {
	c := a.client()
	if c == nil {
		return
	}
	a.mu.Lock()
	pre := a.prefs.ShowPrereleases
	a.busy["cores"] = true
	a.coresView.loaded = true
	a.mu.Unlock()
	go func() {
		list, err := c.Do(a.ctx, localapi.Request{Type: localapi.RequestCores})
		var rels *localapi.Response
		if err == nil && releases {
			rels, err = c.Do(a.ctx, localapi.Request{Type: localapi.RequestReleases, Pre: pre})
		}
		a.update(func() {
			delete(a.busy, "cores")
			if err != nil {
				a.setToast(describeError(err), true)
				return
			}
			a.coresView.cores = list.Cores
			if rels != nil {
				a.coresView.releases, a.coresView.checked = rels.Releases, true
			}
		})
	}()
}

// coreRequest installs or removes a core, then refreshes the lists.
func (a *App) coreRequest(req localapi.Request) {
	a.do("cores", req, "", func(r *localapi.Response) {
		if req.Type == localapi.RequestCoreInstall && req.Tag == a.coreUpdate {
			a.coreUpdate = ""
		}
		a.coresView.cores = r.Cores
		for i := range a.coresView.releases {
			a.coresView.releases[i].Installed = slices.ContainsFunc(r.Cores, func(c localapi.CoreInfo) bool {
				return c.Tag == a.coresView.releases[i].Tag
			})
		}
	})
}

// selectCore makes a core the one networks run on.
func (a *App) selectCore(tag string) {
	a.mu.Lock()
	s := a.settings
	a.mu.Unlock()
	s.Core = tag
	a.do("cores", localapi.Request{Type: localapi.RequestSettingsSet, Settings: &s}, L.ToastCoreSwitched(), func(r *localapi.Response) {
		a.settings = *r.Settings
		for i := range a.coresView.cores {
			a.coresView.cores[i].Selected = a.coresView.cores[i].Tag == tag
		}
	})
}

// aboutPage: the app's version, license and credits with their links; its
// updates; and the diagnostics (export and the log folder).
func (a *App) aboutPage(c *ui.Context) {
	r := a.settingsRows(c)
	man, _ := third_party.EasyTier()
	link := func(key, label, url string) func() {
		return func() { ui.Link(c.Key(key), label, url) }
	}
	pageColumn(c, "set-about", pageColumnW, func() {
		pageIntro(c, L.SettingsAboutTitle(), L.SettingsAboutAbout())
		r.section(c, "app", appName, nil, func(r *rows) {
			r.row(c, "version", iconNetwork, L.AboutVersion(version.Version), L.AboutVersions(man.Bundled),
				link("src", L.AboutSource(), "https://github.com/YuanZhe-99/Easytier-MyGo"))
			r.row(c, "license", iconFile, L.AboutLicenseTitle(), L.AboutLicense(), link("et-src", L.AboutEasyTierSource(), man.Source))
			r.row(c, "design", iconHeart, L.AboutDesignTitle(), L.AboutDesign(), link("ios", "EasyTier-iOS", "https://github.com/EasyTier/EasyTier-iOS"))
		})
		r.section(c, "updates", L.SectionUpdates(), nil, func(r *rows) { a.appUpdateRows(c, r) })
		r.section(c, "diag", L.SectionDiagnostics(), nil, func(r *rows) {
			r.row(c, "diag-file", iconPulse, L.DiagFile(), L.SettingsAdvancedHint(), func() {
				if ui.Button(c.Key("diag-export"), L.ActionExportDiagnostics()).Clicked() {
					a.exportDiagnostics()
				}
			})
			r.row(c, "diag-logs", iconFolder, L.DiagLogs(), L.DiagLogsAbout(), func() {
				if ui.Button(c.Key("diag-open"), L.ActionOpenLogs()).Clicked() {
					dir := userStateDir()
					go func() { _ = mygo.Shell.OpenPath(dir) }()
				}
			})
		})
	})
}

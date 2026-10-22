package gui

import (
	_ "embed"
	"fmt"
	"runtime"
	"slices"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/localapi"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/version"
	L "github.com/YuanZhe-99/Easytier-MyGo/locales"
	"github.com/YuanZhe-99/Easytier-MyGo/third_party"
)

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

// settingsView: general, run mode, cores, about.
func (a *App) settingsView(c *ui.Context) {
	ui.Scroll(c.Key("settings-page")).Grow(1).Padding(24).Gap(16).Children(func() {
		ui.Text(c, L.PageSettings()).FontSize(sizeTitle).Bold()
		a.generalSettings(c)
		a.runModeSettings(c)
		a.coreSettings(c)
		a.advancedSettings(c)
		a.aboutSettings(c)
	})
}

func settingRow(c *ui.Context, key, title, hint string, control func()) {
	t := c.Theme()
	ui.Row(c.Key(key)).Gap(16).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			ui.Text(c, title)
			if hint != "" {
				ui.Text(c, hint).FontSize(sizeSmall).TextColor(t.TextMuted)
			}
		})
		control()
	})
}

func (a *App) generalSettings(c *ui.Context) {
	card(c).Children(func() {
		ui.Text(c, L.SettingsGeneral()).FontSize(sizeHeading).Bold()
		// Language: the system's, or one of the catalogs'.
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
		settingRow(c, "lang", L.SettingsLanguage(), "", func() {
			if ui.Select(c.Key("lang-select"), &cur, options).Width(200).Changed() {
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
		})
		a.mu.Lock()
		themeIdx := slices.Index([]string{"system", "light", "dark"}, a.prefs.Theme)
		a.mu.Unlock()
		themeIdx = max(themeIdx, 0)
		settingRow(c, "theme", L.SettingsTheme(), "", func() {
			if ui.Segmented(c.Key("theme-seg"), &themeIdx, L.ThemeSystem(), L.ThemeLight(), L.ThemeDark()).Changed() {
				a.setTheme([]string{"system", "light", "dark"}[themeIdx])
			}
		})
		a.mu.Lock()
		login := a.openAtLogin
		a.mu.Unlock()
		settingRow(c, "login", L.SettingsLaunchAtLogin(), L.SettingsLaunchAtLoginHint(), func() {
			if ui.Switch(c.Key("login-sw"), &login).Label(L.SettingsLaunchAtLogin()).Changed() {
				if err := mygo.App.SetOpenAtLogin(login); err != nil {
					a.setToast(err.Error(), true)
				} else {
					a.mu.Lock()
					a.openAtLogin = login
					a.mu.Unlock()
				}
			}
		})
		a.mu.Lock()
		p := a.prefs
		a.mu.Unlock()
		changed := false
		settingRow(c, "hidden", L.SettingsStartHidden(), "", func() {
			changed = ui.Switch(c.Key("hidden-sw"), &p.StartHidden).Label(L.SettingsStartHidden()).Changed() || changed
		})
		closeIdx := 0
		if p.CloseQuits {
			closeIdx = 1
		}
		settingRow(c, "close", L.SettingsCloseButton(), "", func() {
			if ui.Segmented(c.Key("close-seg"), &closeIdx, L.CloseHide(), L.CloseQuit()).Changed() {
				p.CloseQuits = closeIdx == 1
				changed = true
			}
		})
		if runtime.GOOS == "darwin" && !p.CloseQuits {
			settingRow(c, "dock", L.SettingsHideDock(), L.SettingsHideDockHint(), func() {
				changed = ui.Switch(c.Key("dock-sw"), &p.HideDock).Label(L.SettingsHideDock()).Changed() || changed
			})
		}
		settingRow(c, "notify", L.SettingsNotifications(), "", func() {
			changed = ui.Switch(c.Key("notify-sw"), &p.Notify).Label(L.SettingsNotifications()).Changed() || changed
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

// runModeSettings shows the three run modes as cards.
func (a *App) runModeSettings(c *ui.Context) {
	t := c.Theme()
	a.mu.Lock()
	choice, busy, st := a.currentChoice(), a.busy["mode"], a.helperSt
	a.mu.Unlock()
	card(c).Children(func() {
		ui.Text(c, L.SettingsRunMode()).FontSize(sizeHeading).Bold()
		ui.Text(c, L.SettingsRunModeHint()).FontSize(sizeSmall).TextColor(t.TextMuted)
		a.modeCards(c, choice, busy)
		if st.Installed {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, L.HelperInstalled(st.Version)).FontSize(sizeSmall).TextColor(t.TextMuted).Grow(1)
				if ui.Button(c, L.ActionUninstallHelper()).Disabled(busy).Clicked() {
					a.uninstallHelper(false)
				}
			})
		}
	})
}

// modeCards are the three run modes; the welcome page uses them too.
func (a *App) modeCards(c *ui.Context, current runChoice, busy bool) {
	t := c.Theme()
	k := tok(c)
	type mode struct {
		choice            runChoice
		title, body, need string
	}
	modes := []mode{
		{choiceUser, L.ModeUserTitle(), L.ModeUserBody(), L.ModeUserNeed()},
		{choiceFollow, L.ModeFollowTitle(), L.ModeFollowBody(), L.ModeFollowNeed()},
		{choiceBackground, L.ModeBackgroundTitle(), L.ModeBackgroundBody(), L.ModeBackgroundNeed()},
	}
	a.mu.Lock()
	unset := a.prefs.Mode == ModeUnset
	a.mu.Unlock()
	ui.Grid(c).Columns(3).Gap(gap).FillWidth().Children(func() {
		for _, m := range modes {
			active := m.choice == current && !unset
			box := ui.Column(c.Key(fmt.Sprint("mode-", m.choice))).Padding(14).Gap(8).Radius(10).Border(1, t.Border)
			if active {
				box.Border(2, t.Accent).Background(t.Accent.Alpha(0.06))
			}
			box.Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, m.title).Bold().Grow(1)
					if active {
						capsule(c, L.ModeCurrent(), t.Accent)
					}
				})
				ui.Text(c, m.body).FontSize(sizeSmall)
				ui.Text(c, m.need).FontSize(sizeSmall).TextColor(k.muted)
				if !active {
					if ui.Button(c, L.ActionUseMode()).Disabled(busy).Clicked() {
						a.chooseMode(m.choice)
					}
				}
			})
		}
	})
	if busy {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Spinner(c)
			ui.Text(c, L.ModeSwitching()).FontSize(sizeSmall).TextColor(t.TextMuted)
		})
	}
}

// coreSettings: installed cores, the selection and official releases.
func (a *App) coreSettings(c *ui.Context) {
	t := c.Theme()
	k := tok(c)
	a.mu.Lock()
	cs := a.coresView
	connected := a.connected
	selected := a.settings.Core
	pre := a.prefs.ShowPrereleases
	busy := a.busy["cores"]
	a.mu.Unlock()
	if connected && !cs.loaded && !busy {
		a.loadCores(false)
	}
	card(c).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			ui.Text(c, L.SettingsCores()).FontSize(sizeHeading).Bold().Grow(1)
			if ui.Button(c, L.ActionCheckReleases()).Disabled(busy || !connected).Clicked() {
				a.loadCores(true)
			}
		})
		ui.Text(c, L.SettingsCoresHint()).FontSize(sizeSmall).TextColor(t.TextMuted)
		a.mu.Lock()
		upd := a.coreUpdate
		auto := a.prefs.CheckCoreUpdates
		a.mu.Unlock()
		if upd != "" {
			ui.Row(c).Gap(8).Padding(8, 12).Radius(8).Background(t.Accent.Alpha(0.1)).AlignItems(ui.Center).Children(func() {
				ui.Text(c, L.CoreUpdateAvailable(upd)).FontSize(sizeSmall).Grow(1)
				if ui.Button(c, L.ActionCheckReleases()).Disabled(busy || !connected).Clicked() {
					a.loadCores(true)
				}
			})
		}
		settingRow(c, "core-auto", L.SettingsCheckUpdates(), L.SettingsCheckUpdatesHint(), func() {
			if ui.Switch(c.Key("core-auto-sw"), &auto).Label(L.SettingsCheckUpdates()).Changed() {
				a.mu.Lock()
				p := a.prefs
				a.mu.Unlock()
				p.CheckCoreUpdates = auto
				a.setPrefs(p)
			}
		})
		for _, core := range cs.cores {
			ui.Row(c.Key("core-" + core.Tag)).Gap(10).AlignItems(ui.Center).Children(func() {
				name := core.Tag
				if core.Source == "bundled" {
					name = L.CoreBundled()
				}
				ui.Text(c, name).Bold()
				if core.Tested {
					capsule(c, L.CoreTested(), k.good)
				} else {
					capsule(c, L.CoreUntested(), k.warn)
				}
				if core.Pre {
					capsule(c, L.CorePrerelease(), k.relay)
				}
				ui.Spacer(c)
				if core.Selected {
					capsule(c, L.CoreInUse(), t.Accent)
					return
				}
				if ui.Button(c, L.ActionUseCore()).Disabled(busy).Clicked() {
					a.selectCore(core.Tag)
				}
				if core.Source != "bundled" && ui.Button(c, L.ActionRemove()).Disabled(busy).Clicked() {
					a.coreRequest(localapi.Request{Type: localapi.RequestCoreRemove, Tag: core.Tag})
				}
			})
		}
		_ = selected
		if !cs.checked {
			return
		}
		ui.Divider(c)
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Text(c, L.CoresAvailable()).Bold().Grow(1)
			if ui.Checkbox(c.Key("core-pre"), &pre, L.CoresShowPre()).Changed() {
				a.mu.Lock()
				p := a.prefs
				a.mu.Unlock()
				p.ShowPrereleases = pre
				a.setPrefs(p)
				a.loadCores(true)
			}
		})
		if len(cs.releases) == 0 {
			ui.Text(c, L.CoresNone()).FontSize(sizeSmall).TextColor(t.TextMuted)
		}
		for _, r := range cs.releases {
			ui.Row(c.Key("rel-" + r.Tag)).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Text(c, r.Tag).Bold()
				ui.Text(c, r.Published.Format("2006-01-02")).FontSize(sizeSmall).TextColor(t.TextMuted)
				if r.Prerelease {
					capsule(c, L.CorePrerelease(), k.relay)
				}
				if r.Tested {
					capsule(c, L.CoreTested(), k.good)
				}
				ui.Spacer(c)
				if r.Installed {
					ui.Text(c, L.CoreInstalled()).FontSize(sizeSmall).TextColor(t.TextMuted)
					return
				}
				if ui.Button(c, L.ActionDownload(formatBytes(float64(r.Size)))).Disabled(busy).Clicked() {
					a.coreRequest(localapi.Request{Type: localapi.RequestCoreInstall, Tag: r.Tag})
				}
			})
		}
		if busy {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c)
				ui.Text(c, L.CoresWorking()).FontSize(sizeSmall).TextColor(t.TextMuted)
			})
		}
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

func (a *App) aboutSettings(c *ui.Context) {
	t := c.Theme()
	man, _ := third_party.EasyTier()
	card(c).Children(func() {
		ui.Text(c, L.SettingsAbout()).FontSize(sizeHeading).Bold()
		ui.Text(c, "EasyTier MyGo "+version.Version).Bold()
		ui.Text(c, L.AboutVersions(man.Bundled)).FontSize(sizeSmall).TextColor(t.TextMuted)
		a.appUpdateSettings(c)
		ui.Text(c, L.AboutLicense()).FontSize(sizeSmall).TextColor(t.TextMuted)
		ui.Row(c).Gap(12).Children(func() {
			ui.Link(c, L.AboutSource(), "https://github.com/YuanZhe-99/Easytier-MyGo")
			ui.Link(c, L.AboutEasyTierSource(), man.Source)
			ui.Link(c, "EasyTier-iOS", "https://github.com/EasyTier/EasyTier-iOS")
		})
	})
}

// welcomeView is the first run: choose a run mode.
func (a *App) welcomeView(c *ui.Context) {
	t := c.Theme()
	a.mu.Lock()
	busy := a.busy["mode"]
	// The welcome page has no toast area: a failed choice stays here until
	// the next attempt.
	failure := ""
	if a.toastErr {
		failure = a.toast
	}
	a.mu.Unlock()
	k := tok(c)
	ui.Scroll(c.Key("welcome")).Grow(1).Fill().Background(t.Background).Padding(40).Gap(20).AlignItems(ui.Center).Children(func() {
		ui.Column(c).MaxWidth(860).Gap(16).Children(func() {
			if appIcon != nil {
				ui.Image(c, appIcon).Width(72).Height(72)
			}
			ui.Text(c, L.WelcomeTitle()).FontSize(28).Bold()
			ui.Text(c, L.WelcomeBody()).TextColor(t.TextMuted)
			a.modeCards(c, choiceUser, busy)
			if failure != "" && !busy {
				ui.Text(c, failure).FontSize(sizeSmall).TextColor(k.bad)
			}
			ui.Text(c, L.WelcomeLater()).FontSize(sizeSmall).TextColor(t.TextMuted)
		})
	})
}

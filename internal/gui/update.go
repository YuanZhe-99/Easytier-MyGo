package gui

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/localapi"
	L "github.com/YuanZhe-99/Easytier-MyGo/locales"
)

// appCheckEvery is how often the automatic app update check runs.
const appCheckEvery = 24 * time.Hour

// Automatic installs (Prefs.AutoInstall).
const (
	autoInstallIdle  = "idle"
	autoInstallDaily = "daily"
	// autoIdleAfter is how long without keyboard or mouse counts as idle.
	autoIdleAfter = 15 * time.Minute
	// autoRetryAfter keeps a failed automatic install from repeating.
	autoRetryAfter = 6 * time.Hour
	// appLoopEvery is how often the loop looks at checks and installs.
	appLoopEvery = 5 * time.Minute
)

// autoInstallDue reports whether an automatic install may start now.
// Inputs: mode and hour from the prefs; idle is the system's idle time (0
// when it does not tell); lastTry is the last automatic install attempt.
// Notes: a system that does not report idle time never counts as idle.
func autoInstallDue(mode string, hour int, now time.Time, idle time.Duration, lastTry time.Time) bool {
	if !lastTry.IsZero() && now.Sub(lastTry) < autoRetryAfter {
		return false
	}
	switch mode {
	case autoInstallIdle:
		return idle >= autoIdleAfter
	case autoInstallDaily:
		return now.Hour() == hour
	}
	return false
}

// appUpdateLoop runs the daily app update check while the GUI runs, the
// first one shortly after launch when one is due, and installs a found
// version when the automatic install's time has come.
func (a *App) appUpdateLoop() {
	if !mygo.Updater.Enabled() {
		return // a package manager or a development build
	}
	select {
	case <-a.ctx.Done():
		return
	case <-time.After(10 * time.Second):
	}
	var lastTry time.Time
	for {
		a.mu.Lock()
		due := a.prefs.CheckAppUpdates && time.Since(a.prefs.LastAppCheck) >= appCheckEvery
		a.mu.Unlock()
		if due {
			a.checkApp(false)
		}
		a.mu.Lock()
		found := a.appUpdate != nil && !a.updating && !a.busy["app-update"]
		mode, hour := a.prefs.AutoInstall, a.prefs.AutoInstallHour
		a.mu.Unlock()
		if found && mode != "" {
			var idle time.Duration
			if mode == autoInstallIdle {
				idle = mygo.Power.IdleTime()
			}
			if now := time.Now(); autoInstallDue(mode, hour, now, idle, lastTry) {
				lastTry = now
				a.installApp()
			}
		}
		select {
		case <-a.ctx.Done():
			return
		case <-time.After(appLoopEvery):
		}
	}
}

// checkApp asks the GitHub release feed for a newer signed version.
// Inputs: manual is a check the user asked for: it reports "up to date"
// and failures, and shows a version the automatic check already announced.
// Side effects: records the check in the prefs; the automatic check
// notifies once per version.
func (a *App) checkApp(manual bool) {
	a.update(func() { a.busy["app-update"] = true })
	ctx, cancel := context.WithTimeout(a.ctx, time.Minute)
	defer cancel()
	up, err := mygo.Updater.Check(ctx)
	var announce bool
	a.update(func() {
		delete(a.busy, "app-update")
		if err != nil {
			if manual {
				a.setToast(L.AppUpdateCheckFailed(), true)
			}
			return
		}
		a.prefs.LastAppCheck = time.Now()
		a.appUpdate = up
		switch {
		case up == nil && manual:
			a.setToast(L.AppUpToDate(), false)
		case up != nil && !manual && a.prefs.AnnouncedApp != up.Version:
			a.prefs.AnnouncedApp = up.Version
			announce = true
		}
		_ = savePrefs(a.prefs)
	})
	if announce {
		a.notify("app-update", L.NotifyAppUpdateTitle(), L.AppUpdateAvailable(up.Version))
	}
}

// installApp downloads the found version, checks its signature, replaces
// the app and relaunches into it.
// Side effects: in No privileges mode the user daemon (and its networks)
// stops first, because Windows cannot move a folder whose programs run;
// it starts again with the new version. On failure it starts again at once.
// A window closed into the tray stays closed after the relaunch.
func (a *App) installApp() {
	a.mu.Lock()
	up, user := a.appUpdate, a.prefs.Mode != ModeHelper
	a.mu.Unlock()
	if up == nil {
		return
	}
	a.update(func() { a.busy["app-update"], a.updating, a.updateProgress = true, true, 0 })
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 30*time.Minute)
		defer cancel()
		if user {
			a.user.stop()
			waitGone(ctx, localapi.UserSocketPath())
		}
		err := up.Install(ctx, func(done, total int64) {
			if total > 0 {
				a.update(func() { a.updateProgress = float64(done) / float64(total) })
			}
		})
		if err != nil {
			a.update(func() {
				delete(a.busy, "app-update")
				a.updating = false
				a.setToast(L.AppUpdateInstallFailed(), true)
			})
			a.reconnect()
			return
		}
		a.mu.Lock()
		a.quitting = true
		a.prefs.RelaunchHidden = !a.visible
		_ = savePrefs(a.prefs)
		a.mu.Unlock()
		mygo.App.Relaunch()
	}()
}

// appUpdateRows are About & updates' Updates rows: the state of the app's
// update (installing with its progress, a version found with Install and
// restart, else when it was last checked with Check for updates), then the
// daily check. A copy that cannot update itself says so instead.
func (a *App) appUpdateRows(c *ui.Context, r *rows) {
	if !mygo.Updater.Enabled() {
		r.row(c, "app-update", iconArrowUp, L.SectionUpdates(), L.AppUpdateUnavailable(), nil)
		return
	}
	a.mu.Lock()
	up, busy, updating, progress, auto, last := a.appUpdate, a.busy["app-update"], a.updating, a.updateProgress, a.prefs.CheckAppUpdates, a.prefs.LastAppCheck
	a.mu.Unlock()
	switch {
	case updating:
		r.row(c, "app-update", iconArrowUp, L.AppUpdateInstalling(int(progress*100)), L.AppUpdateInstallingAbout(), func() { ui.Spinner(c) })
	case up != nil:
		r.row(c, "app-update", iconArrowUp, L.AppUpdateAvailable(up.Version), L.AppUpdateAvailableAbout(), func() {
			if ui.PrimaryButton(c.Key("app-install"), L.ActionInstallUpdate()).Disabled(busy).Clicked() {
				a.installApp()
			}
		})
	default:
		about := L.AppUpdateNeverChecked()
		if !last.IsZero() {
			about = L.AppUpdateLastChecked(last.Local().Format("2006-01-02 15:04"))
		}
		r.row(c, "app-update", iconCheck, L.SectionUpdates(), about, func() {
			if ui.Button(c.Key("app-check"), L.ActionCheckAppUpdate()).Disabled(busy).Clicked() {
				go a.checkApp(true)
			}
		})
	}
	r.row(c, "app-auto", iconRefresh, L.SettingsCheckAppUpdates(), L.SettingsCheckAppUpdatesHint(), func() {
		if ui.Switch(c.Key("app-auto-sw"), &auto).Label(L.SettingsCheckAppUpdates()).Changed() {
			a.mu.Lock()
			p := a.prefs
			a.mu.Unlock()
			p.CheckAppUpdates = auto
			a.setPrefs(p)
		}
	})
	if auto {
		a.autoInstallRows(c, r)
	}
}

// autoInstallRows choose the automatic install: never, when idle, or daily
// at an hour (the hour row shows only for daily). They need the daily
// check, which finds what they install.
func (a *App) autoInstallRows(c *ui.Context, r *rows) {
	modes := []string{"", autoInstallIdle, autoInstallDaily}
	a.mu.Lock()
	p := a.prefs
	a.mu.Unlock()
	idx := max(slices.Index(modes, p.AutoInstall), 0)
	r.row(c, "app-autoinstall", iconArrowUp, L.SettingsAutoInstall(), L.SettingsAutoInstallAbout(), func() {
		if ui.Segmented(c.Key("app-autoinstall-seg"), &idx, L.AutoInstallOff(), L.AutoInstallIdle(), L.AutoInstallDaily()).Changed() {
			p.AutoInstall = modes[idx]
			a.setPrefs(p)
		}
	})
	if p.AutoInstall != autoInstallDaily {
		return
	}
	hours := make([]string, 24)
	for h := range hours {
		hours[h] = fmt.Sprintf("%02d:00", h)
	}
	cur := hours[min(max(p.AutoInstallHour, 0), 23)]
	r.row(c, "app-autoinstall-hour", iconClock, L.SettingsAutoInstallTime(), L.SettingsAutoInstallTimeAbout(), func() {
		if ui.Select(c.Key("app-autoinstall-hour-sel"), &cur, hours).Width(hourSelectW).Label(L.SettingsAutoInstallTime()).Changed() {
			p.AutoInstallHour = slices.Index(hours, cur)
			a.setPrefs(p)
		}
	})
}

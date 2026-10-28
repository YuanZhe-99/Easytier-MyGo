package gui

import (
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/fsutil"
)

// Mode is the run mode the user chose (doc/en-us/run-modes.md). Follow GUI
// and Background service both use the helper; the daemon's own setting
// tells them apart.
type Mode string

const (
	ModeUnset  Mode = ""       // first run: the welcome flow asks
	ModeUser   Mode = "user"   // no privileges: a user-level daemon child
	ModeHelper Mode = "helper" // the privileged helper
)

// Prefs are the GUI's own settings, kept per user.
type Prefs struct {
	Mode Mode `json:"mode"`
	// Theme is "system", "light" or "dark".
	Theme string `json:"theme,omitempty"`
	// CloseQuits makes the close button quit instead of hiding to the tray.
	CloseQuits bool `json:"close_quits,omitempty"`
	// HideDock hides the macOS Dock icon while the window is closed into
	// the tray (minimizing keeps it); other systems ignore it.
	HideDock bool `json:"hide_dock_on_close"`
	// StartHidden starts in the tray when launched at login.
	StartHidden bool `json:"start_hidden"`
	// Notify shows desktop notifications on connection changes.
	Notify bool `json:"notify"`
	// TrayHintShown records that the hide-to-tray notice was shown once.
	TrayHintShown bool `json:"tray_hint_shown,omitempty"`
	// CheckCoreUpdates checks for new EasyTier releases once a day.
	CheckCoreUpdates bool `json:"check_core_updates"`
	// Language is the chosen interface language ("" follows the system).
	Language string `json:"language,omitempty"`
	// LastCoreCheck and AnnouncedCore keep the daily update check quiet:
	// at most one check a day, one notification per release.
	LastCoreCheck time.Time `json:"last_core_check,omitzero"`
	AnnouncedCore string    `json:"announced_core,omitempty"`
	// CheckAppUpdates checks GitHub for a newer signed app once a day;
	// LastAppCheck and AnnouncedApp keep it to one notification a version.
	CheckAppUpdates bool      `json:"check_app_updates"`
	LastAppCheck    time.Time `json:"last_app_check,omitzero"`
	AnnouncedApp    string    `json:"announced_app,omitempty"`
	// AutoInstall installs a found app update without asking: "" never,
	// "idle" once the computer is idle, "daily" within AutoInstallHour
	// (local time, 0–23).
	AutoInstall     string `json:"auto_install,omitempty"`
	AutoInstallHour int    `json:"auto_install_hour"`
	// RelaunchHidden starts the next launch in the tray: an update
	// installed while the window was closed keeps it closed.
	RelaunchHidden bool `json:"relaunch_hidden,omitempty"`
	// ShowPrereleases lists pre-releases in the core manager.
	ShowPrereleases bool `json:"show_prereleases,omitempty"`
	// SidebarRail keeps the sidebar folded to its icons.
	SidebarRail bool `json:"sidebar_rail,omitempty"`
}

func defaultPrefs() Prefs {
	return Prefs{Theme: "system", HideDock: true, StartHidden: true, Notify: true, CheckCoreUpdates: true, CheckAppUpdates: true, AutoInstallHour: 3}
}

// prefsPath is $XDG_CONFIG_HOME/easytier-mygo/gui.json.
func prefsPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "easytier-mygo", "gui.json")
}

func loadPrefs() Prefs {
	p := defaultPrefs()
	_, _ = fsutil.ReadJSON(prefsPath(), &p)
	return p
}

func savePrefs(p Prefs) error { return fsutil.WriteJSON(prefsPath(), p) }

// ThemePref is the remembered appearance.
func (a *App) ThemePref() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.prefs.Theme
}

// StartHidden reports whether a login launch starts in the tray.
func (a *App) StartHidden() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.prefs.StartHidden
}

// TakeRelaunchHidden reports whether this launch follows an update
// installed while the window was closed, and forgets it.
// Side effects: saves the prefs when the mark was set.
func (a *App) TakeRelaunchHidden() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.prefs.RelaunchHidden {
		return false
	}
	a.prefs.RelaunchHidden = false
	_ = savePrefs(a.prefs)
	return true
}

// HidesDock reports whether the Dock icon goes away with the window: macOS
// only, and only while the user wants it.
func (a *App) HidesDock() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return runtime.GOOS == "darwin" && a.prefs.HideDock
}

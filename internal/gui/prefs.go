package gui

import (
	"os"
	"path/filepath"
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
	// ShowPrereleases lists pre-releases in the core manager.
	ShowPrereleases bool `json:"show_prereleases,omitempty"`
}

func defaultPrefs() Prefs {
	return Prefs{Theme: "system", StartHidden: true, Notify: true, CheckCoreUpdates: true}
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

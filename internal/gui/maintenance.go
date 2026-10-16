package gui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/localapi"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/version"
	L "github.com/YuanZhe-99/Easytier-MyGo/locales"
	"github.com/YuanZhe-99/Easytier-MyGo/third_party"
)

// coreCheckEvery is how often the automatic core update check runs.
const coreCheckEvery = 24 * time.Hour

// newerTag reports whether tag a (v2.7.0) is newer than b.
func newerTag(a, b string) bool {
	pa, pb := tagParts(a), tagParts(b)
	for i := range pa {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

func tagParts(tag string) [3]int {
	var out [3]int
	for i, s := range strings.SplitN(strings.TrimPrefix(tag, "v"), ".", 3) {
		n, _ := strconv.Atoi(strings.TrimFunc(s, func(r rune) bool { return r < '0' || r > '9' }))
		out[i] = n
	}
	return out
}

// maybeCheckCores runs the daily check for a newer stable EasyTier and
// tells the user once per release. It only reads release metadata; it
// never downloads.
func (a *App) maybeCheckCores(c *localapi.Client) {
	a.mu.Lock()
	p := a.prefs
	a.mu.Unlock()
	if !p.CheckCoreUpdates || time.Since(p.LastCoreCheck) < coreCheckEvery {
		return
	}
	go func() {
		r, err := c.Do(a.ctx, localapi.Request{Type: localapi.RequestReleases})
		if err != nil || len(r.Releases) == 0 {
			return
		}
		latest := r.Releases[0]
		man, _ := third_party.EasyTier()
		a.mu.Lock()
		a.prefs.LastCoreCheck = time.Now()
		announce := newerTag(latest.Tag, man.Bundled) && !latest.Installed && a.prefs.AnnouncedCore != latest.Tag
		if announce {
			a.prefs.AnnouncedCore = latest.Tag
			a.coreUpdate = latest.Tag
		}
		prefs := a.prefs
		a.mu.Unlock()
		_ = savePrefs(prefs)
		if announce {
			a.notify("core-update", L.NotifyCoreUpdateTitle(), L.CoreUpdateAvailable(latest.Tag))
			a.update(func() {})
		}
	}()
}

// advancedSettings: diagnostics export and the log folder.
func (a *App) advancedSettings(c *ui.Context) {
	t := c.Theme()
	card(c).Children(func() {
		ui.Text(c, L.SettingsAdvanced()).FontSize(sizeHeading).Bold()
		ui.Text(c, L.SettingsAdvancedHint()).FontSize(sizeSmall).TextColor(t.TextMuted)
		ui.Row(c).Gap(8).Children(func() {
			if ui.Button(c, L.ActionExportDiagnostics()).Clicked() {
				a.exportDiagnostics()
			}
			if ui.Button(c, L.ActionOpenLogs()).Clicked() {
				dir := userStateDir()
				go func() { _ = mygo.Shell.OpenPath(dir) }()
			}
		})
	})
}

// userStateDir is where the GUI and the user daemon keep logs and state.
func userStateDir() string {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "easytier-mygo")
}

// diagnostics is the exported support bundle. It holds states, versions
// and events; never configuration text or secrets.
type diagnostics struct {
	Created  time.Time                `json:"created"`
	App      string                   `json:"app"`
	Platform string                   `json:"platform"`
	Mode     Mode                     `json:"mode"`
	Daemon   localapi.Info            `json:"daemon"`
	Settings localapi.Settings        `json:"settings"`
	Networks []localapi.NetworkStatus `json:"networks"`
	Events   any                      `json:"events"`
}

// exportDiagnostics writes the bundle where the user chooses.
func (a *App) exportDiagnostics() {
	a.mu.Lock()
	d := diagnostics{Created: time.Now().UTC(), App: version.Version, Platform: runtime.GOOS + "/" + runtime.GOARCH,
		Mode: a.prefs.Mode, Daemon: a.info, Settings: a.settings,
		Networks: append([]localapi.NetworkStatus(nil), a.networks...), Events: append(a.events[:0:0], a.events...)}
	w := a.window
	a.mu.Unlock()
	go func() {
		path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: w, Title: L.ActionExportDiagnostics(),
			DefaultPath: "easytier-mygo-diagnostics-" + time.Now().Format("20060102-150405") + ".json"})
		if err != nil || path == "" {
			return
		}
		data, _ := json.MarshalIndent(d, "", "  ")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			a.showToast(err.Error(), true)
			return
		}
		a.showToast(L.ToastExported(), false)
	}()
}

package gui

import (
	"image/png"
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/health"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/helper"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/localapi"
	L "github.com/YuanZhe-99/Easytier-MyGo/locales"
)

// demoApp is an App with the state a connected daemon would push: one
// connected network with traffic and peers, one stopped.
func demoApp(t *testing.T) *App {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	a := New()
	a.prefs.Mode = ModeHelper
	a.page = pageNetwork
	a.connected, a.lastPush = true, time.Now()
	now := time.Now()
	a.networks = []localapi.NetworkStatus{
		{ID: "n1", Name: "Home", Run: true, State: localapi.StateConnected, Level: health.LevelHealthy,
			NetworkName: "family-net", VirtualIPv4: "10.144.144.2/24", PeerCount: 3, NATType: "Symmetric",
			CoreVersion: "2.6.4-8428a89d", StartedAt: now.Add(-95 * time.Minute), RxRate: 312 * 1024, TxRate: 48 * 1024},
		{ID: "n2", Name: "Office lab", State: localapi.StateStopped, NetworkName: "lab", NoTUN: true},
	}
	var pts []localapi.TrafficPoint
	for i := 0; i < 300; i++ {
		ts := now.Add(time.Duration(i-299) * time.Second)
		gapPt := i > 120 && i < 128
		rx := 200*1024 + 150*1024*math.Sin(float64(i)/17) + float64(i%7)*9000
		tx := 40*1024 + 25*1024*math.Cos(float64(i)/11)
		pts = append(pts, localapi.TrafficPoint{Seq: uint64(i + 1), Time: ts, Rx: rx, Tx: tx, Gap: gapPt})
	}
	a.traffic["n1"] = pts
	a.selected = "n1"
	a.expanded["/nas/10.144.144.1/24"] = true
	a.coreUpdate = "v2.7.1"
	a.details["n1"] = &localapi.NetworkDetail{Peers: []localapi.PeerInfo{
		{Hostname: "Windows-Dev-Kit", IPv4: "10.144.144.2/24", Local: true, NATType: "Symmetric"},
		{Hostname: "nas", IPv4: "10.144.144.1/24", Direct: true, Cost: 1, LatencyMS: 3.4, Tunnel: "udp", NATType: "FullCone", Version: "2.6.4",
			Conns: []localapi.ConnInfo{{Tunnel: "udp", Local: "udp://192.0.2.10:11010", Remote: "udp://198.51.100.7:11010", LatencyMS: 3.4, RxBytes: 48 << 20, TxBytes: 9 << 20},
				{Tunnel: "tcp", Local: "tcp://192.0.2.10:51234", Remote: "tcp://198.51.100.7:11010", LatencyMS: 5.1, LossRate: 0.03, RxBytes: 2 << 20, TxBytes: 1 << 20}}},
		{Hostname: "phone", IPv4: "10.144.144.7/24", Direct: false, Cost: 2, LatencyMS: 128.6, LossRate: 0.04, NATType: "Symmetric", Version: "2.6.4"},
		{Hostname: "macbook-pro", IPv4: "10.144.144.5/24", Direct: true, Cost: 1, LatencyMS: 12.5, Tunnel: "udp", NATType: "PortRestricted", Version: "2.6.4"},
		{Hostname: "Intel-NUC8i5BEH", IPv4: "10.144.144.9/24", Direct: true, Cost: 1, LatencyMS: 231, Tunnel: "tcp", NATType: "OpenInternet", Version: "2.7.0"},
	}}
	a.events = []health.Event{
		{Time: now.Add(-90 * time.Minute), Component: health.Component{Kind: health.KindInstance, ID: "n1"}, Kind: health.EventLevelChanged, To: health.LevelHealthy},
		{Time: now.Add(-30 * time.Minute), Component: health.Component{Kind: health.KindCore, ID: "n1"}, Kind: health.EventLevelChanged, To: health.LevelFailed, Reason: "process-exited"},
		{Time: now.Add(-29 * time.Minute), Component: health.Component{Kind: health.KindCore, ID: "n1"}, Kind: health.EventRecoveryFinished},
	}
	return a
}

// TestViewsRender renders every page in each language and theme; it fails
// on a panic. With SCREENSHOT_DIR set it writes PNGs for review.
func TestViewsRender(t *testing.T) {
	dir := os.Getenv("SCREENSHOT_DIR")
	pages := map[string]func(a *App){
		"network": func(a *App) {},
		"stopped": func(a *App) {
			a.selected = "n2"
			a.summaries["n2"] = summaryOf("dhcp = true\nlisteners = [\"tcp://0.0.0.0:11010\"]\n\n[network_identity]\nnetwork_name = \"lab\"\n\n[[peer]]\nuri = \"tcp://public.easytier.cn:11010\"\n", "")
		},
		"rail":             func(a *App) { a.prefs.SidebarRail = true },
		"stale":            func(a *App) { a.lastPush = time.Now().Add(-time.Minute) },
		"empty":            func(a *App) { a.networks, a.selected = nil, "" },
		"settings":         func(a *App) { a.settingsOpen, a.settingsAt = true, setGeneral },
		"settings-mode":    func(a *App) { a.settingsOpen, a.settingsAt = true, setRunMode },
		"settings-core":    func(a *App) { a.settingsOpen, a.settingsAt = true, setCore },
		"settings-about":   func(a *App) { a.settingsOpen, a.settingsAt = true, setAbout },
		"settings-search":  func(a *App) { a.settingsOpen, a.settingsQuery = true, "update" },
		"logs":             func(a *App) { a.page = pageLogs },
		"welcome":          func(a *App) { a.page, a.prefs.Mode = pageWelcome, ModeUnset },
		"edit-toml": func(a *App) {
			a.page = pageEdit
			a.editor = &configEditor{id: "n1", name: "Home"}
			a.editor.load("instance_name = \"home\"\n", "rev1")
			a.editor.tab = 1
		},
		"edit": func(a *App) {
			a.page = pageEdit
			a.editor = &configEditor{id: "n1", name: "Home"}
			a.editor.load("instance_name = \"home\"\nhostname = \"laptop\"\ndhcp = true\nlisteners = [\"tcp://0.0.0.0:11010\"]\n\n[network_identity]\nnetwork_name = \"family-net\"\nnetwork_secret = \"s3cret\"\n\n[[peer]]\nuri = \"tcp://public.easytier.cn:11010\"\n\n[flags]\nno_tun = false\n", "rev1")
		},
		"create": func(a *App) { a.createDlg = &createDialog{name: "Office"} },
		"helper-stopped": func(a *App) {
			a.connected, a.connErr, a.prefs.Mode = false, "daemon not running", ModeHelper
			a.helperSt = helper.Status{Installed: true}
		},
	}
	for _, lang := range []string{"en", "zh-Hans", "zh-Hant", "ja"} {
		i18n.SetLocaleFunc(func() string { return lang })
		for name, setup := range pages {
			t.Log(lang, name)
			for _, dark := range []bool{false, true} {
				if dark && lang != "en" && name != "network" {
					continue
				}
				a := demoApp(t)
				setup(a)
				tt := ui.NewTester(a.view, 1100, 720)
				tt.SetDark(dark)
				tt.Frame()
				if dir == "" {
					continue
				}
				suffix := ""
				if dark {
					suffix = "-dark"
				}
				f, err := os.Create(filepath.Join(dir, lang+"-"+name+suffix+".png"))
				if err != nil {
					t.Fatal(err)
				}
				if err := png.Encode(f, tt.Image()); err != nil {
					t.Fatal(err)
				}
				f.Close()
			}
		}
	}
	i18n.SetLocaleFunc(nil)
}

// TestHeaderClearsWindowButtons checks that the system's window buttons
// cover nothing: the sidebar's top row (and the rail's) starts past the
// traffic lights, the pane header's actions end before buttons on the
// right, and in full screen the sidebar keeps no room for them.
func TestHeaderClearsWindowButtons(t *testing.T) {
	const w = 1100
	cases := map[string]ui.TitleBar{
		"mac":   {Height: 60, Left: 76},
		"linux": {Height: 60, Right: 120},
	}
	for name, bar := range cases {
		for _, rail := range []bool{false, true} {
			a := demoApp(t)
			a.prefs.SidebarRail = rail
			tt := ui.NewTester(a.view, w, 720)
			tt.SetTitleBar(bar)
			tt.Frame()
			first := L.SidebarHide()
			if rail {
				first = L.SidebarShow()
			}
			r, ok := tt.Find(first)
			if !ok {
				t.Fatalf("%s rail=%v: no %q", name, rail, first)
			}
			if r.X < bar.Left && r.Y < bar.Height {
				t.Errorf("%s rail=%v: %q at %+v under the window buttons %+v", name, rail, first, r, bar)
			}
			d, ok := tt.Find(L.ActionDisconnect())
			if !ok {
				t.Fatalf("%s rail=%v: no Disconnect in the header", name, rail)
			}
			if d.X+d.W > w-bar.Right || d.Y+d.H > paneMargin+paneHeaderH {
				t.Errorf("%s rail=%v: Disconnect %+v under the window buttons %+v", name, rail, d, bar)
			}
		}
	}
	a := demoApp(t)
	a.fullScreen = true
	tt := ui.NewTester(a.view, w, 720)
	tt.SetTitleBar(ui.TitleBar{Height: 60, Left: 76})
	tt.Frame()
	if r, _ := tt.Find(L.SidebarHide()); r.X > 40 {
		t.Errorf("full screen: the sidebar keeps the traffic lights' room (%+v)", r)
	}
}

// TestSettingsHitsShow: every setting the Settings search finds shows on
// the page it leads to, in each language.
func TestSettingsHitsShow(t *testing.T) {
	for _, lang := range []string{"en", "zh-Hans", "zh-Hant", "ja"} {
		i18n.SetLocaleFunc(func() string { return lang })
		for _, h := range settingsHits() {
			a := demoApp(t)
			a.settingsOpen, a.settingsAt = true, h.page
			tt := ui.NewTester(a.view, 1280, 4000)
			tt.Frame()
			if !tt.HasText(h.title) {
				t.Errorf("%s: %q is not on %s", lang, h.title, pageName(h.page))
			}
		}
	}
	i18n.SetLocaleFunc(nil)
}

// TestSearchSettings: every word must match, and titles that match come
// before settings found by their other words.
func TestSearchSettings(t *testing.T) {
	all := []settingsHit{
		{setAbout, "Logs", "open the log folder"},
		{setCore, "Check for releases daily", "nothing is downloaded"},
		{setGeneral, "Language", "follows the system"},
	}
	for _, tc := range []struct {
		q    string
		want []string
	}{
		{"log", []string{"Logs"}},
		{"SYSTEM", []string{"Language"}},
		{"check daily", []string{"Check for releases daily"}},
		{"daily folder", nil},
		{"o", []string{"Logs", "Check for releases daily", "Language"}},
	} {
		var got []string
		for _, h := range searchSettings(all, tc.q) {
			got = append(got, h.title)
		}
		if !slices.Equal(got, tc.want) {
			t.Errorf("%q: got %v, want %v", tc.q, got, tc.want)
		}
	}
}

// TestDayName: Activity's days are Today, Yesterday, else the date.
func TestDayName(t *testing.T) {
	now := time.Date(2026, 10, 24, 0, 30, 0, 0, time.Local)
	for _, tc := range []struct {
		at   time.Time
		want string
	}{
		{now.Add(-10 * time.Minute), L.DayToday()},
		{now.Add(-40 * time.Minute), L.DayYesterday()},
		{now.Add(-25 * time.Hour), "2026-10-22"},
	} {
		if got := dayName(tc.at, now); got != tc.want {
			t.Errorf("%v: %q, want %q", tc.at, got, tc.want)
		}
	}
}

func TestNewerTag(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{{"v2.7.0", "v2.6.4", true}, {"v2.6.4", "v2.6.4", false}, {"v2.6.10", "v2.6.9", true}, {"v2.6.4", "v2.7.0", false}, {"v3.0.0-rc1", "v2.9.9", true}} {
		if got := newerTag(tc.a, tc.b); got != tc.want {
			t.Errorf("newerTag(%s, %s) = %v", tc.a, tc.b, got)
		}
	}
}

// TestHideDockDefaultsOn: a preferences file written before the Dock
// setting existed hides the Dock icon on close; an explicit off is kept.
func TestHideDockDefaultsOn(t *testing.T) {
	for _, tc := range []struct {
		file string
		want bool
	}{
		{`{"mode":"helper","start_hidden":true}`, true},
		{`{"mode":"helper","hide_dock_on_close":false}`, false},
	} {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		if err := os.MkdirAll(filepath.Dir(prefsPath()), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(prefsPath(), []byte(tc.file), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := loadPrefs().HideDock; got != tc.want {
			t.Errorf("%s: HideDock = %v, want %v", tc.file, got, tc.want)
		}
	}
}

// TestShownStatusStale checks that the network page never shows a last
// reported state as current once the daemon's data is stale.
func TestShownStatusStale(t *testing.T) {
	n := localapi.NetworkStatus{Run: true, State: localapi.StateConnected, Error: "x", Reason: "no-peers"}
	cases := []struct {
		stale bool
		want  localapi.State
	}{
		{false, localapi.StateConnected},
		{true, localapi.StateUnknown},
	}
	for _, c := range cases {
		got := shownStatus(n, c.stale)
		if got.State != c.want || c.stale && (got.Error != "" || got.Reason != "") {
			t.Errorf("stale=%v: got %+v, want state %s", c.stale, got, c.want)
		}
	}
}

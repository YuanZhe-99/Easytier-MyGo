package gui

import (
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/health"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/helper"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/localapi"
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
		"network":  func(a *App) {},
		"stopped":  func(a *App) { a.selected = "n2" },
		"settings": func(a *App) { a.page = pageSettings },
		"logs":     func(a *App) { a.page = pageLogs },
		"welcome":  func(a *App) { a.page, a.prefs.Mode = pageWelcome, ModeUnset },
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

// TestTitleBarClearsWindowButtons checks that the title-bar chip sits in the
// strip, past the room the system's window buttons take, and that the
// strip goes away in full screen. With SCREENSHOT_DIR set it writes PNGs.
func TestTitleBarClearsWindowButtons(t *testing.T) {
	dir := os.Getenv("SCREENSHOT_DIR")
	cases := map[string]ui.TitleBar{
		"mac":     {Height: titleBarH, Left: 76},
		"windows": {Height: titleBarH, Right: 138},
	}
	for name, bar := range cases {
		for _, welcome := range []bool{false, true} {
			a := demoApp(t)
			if welcome {
				a.page, a.prefs.Mode = pageWelcome, ModeUnset
			}
			tt := ui.NewTester(a.view, 1100, 720)
			tt.SetTitleBar(bar)
			tt.Frame()
			r, ok := tt.Find(appName)
			if !ok {
				t.Fatalf("%s welcome=%v: no title-bar chip", name, welcome)
			}
			if r.X < bar.Left || r.X+r.W > 1100-bar.Right || r.Y+r.H > titleBarH {
				t.Errorf("%s welcome=%v: chip %+v overlaps the window buttons %+v", name, welcome, r, bar)
			}
			if dir != "" && !welcome {
				f, err := os.Create(filepath.Join(dir, "titlebar-"+name+".png"))
				if err != nil {
					t.Fatal(err)
				}
				_ = png.Encode(f, tt.Image())
				f.Close()
			}
		}
	}
	a := demoApp(t)
	a.fullScreen = true
	tt := ui.NewTester(a.view, 1100, 720)
	tt.Frame()
	if tt.HasText(appName) {
		t.Error("the title-bar strip shows in full screen")
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

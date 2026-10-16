package gui

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestFriendlyName(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"macbook-pro", "MacBook Pro"},
		{"Intel-NUC8i5BEH", "Intel NUC8i5BEH"},
		{"Mac-mini-2024", "Mac mini 2024"},
		{"mac-mini", "Mac mini"},
		{"Windows-Dev-Kit", "Windows Dev Kit"},
		{"PublicServer_Osaka-Relay", "PublicServer Osaka Relay"},
		{"home-nas", "Home NAS"},
		{"nuc8i5beh", "nuc8i5beh"},         // letters and digits: left alone
		{"rpi4-garage", "rpi4 Garage"},     // likewise for the word
		{"thinkpad-x1", "ThinkPad x1"},     // "x1" mixes letters and digits
		{"Zhe's MacBook", "Zhe's MacBook"}, // not a machine-style name
		{"host.lan", "host.lan"},
		{"-edge-", "-edge-"},
		{"a--b", "a--b"},
		{"笔记本", "笔记本"},
		{"", ""},
	} {
		if got := friendlyName(tc.in); got != tc.want {
			t.Errorf("friendlyName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestMagicDNSName(t *testing.T) {
	for _, tc := range []struct{ host, zone, want string }{
		{"MacBook-Pro", "et.net", "macbook-pro.et.net"},
		{"nas", "home.lan", "nas.home.lan"},
		{"nas", "", ""},                 // MagicDNS off
		{"Zhe's MacBook", "et.net", ""}, // not a DNS label
		{"host.lan", "et.net", ""},
	} {
		if got := magicDNSName(tc.host, tc.zone); got != tc.want {
			t.Errorf("magicDNSName(%q, %q) = %q, want %q", tc.host, tc.zone, got, tc.want)
		}
	}
}

func TestVendorLogoName(t *testing.T) {
	for _, tc := range []struct{ host, want string }{
		{"MacBook-Pro", "apple"},
		{"Mac-mini-2024", "apple"},
		{"iPhone15", "apple"},
		{"Intel-NUC11PAHi5", "intel"},
		{"NUC8i5BEH", "intel"},
		{"Linksys-MX8500", "linksys"},
		{"Windows-Dev-Kit", "windows"},
		{"tp-link-deco", "tplink"},
		{"raspberrypi", "raspberrypi"},
		{"ubuntu-on-dell", "dell"}, // makers before operating systems
		{"CloudCone-S196V", ""},
		{"PublicServer_Osaka-Relay", ""},
		{"php-dev", ""}, // "hp" only as a whole word
		{"algo-box", ""},
		{"harmony", ""},
		{"", ""},
	} {
		if got := vendorLogoName(tc.host); got != tc.want {
			t.Errorf("vendorLogoName(%q) = %q, want %q", tc.host, got, tc.want)
		}
	}
}

// TestVendorLogosParse: every bundled logo parses, and every logo the
// keyword table names is bundled, so no match draws nothing.
func TestVendorLogosParse(t *testing.T) {
	files, err := fs.Glob(logoFS, "assets/logos/*.svg")
	if err != nil || len(files) == 0 {
		t.Fatalf("no logos embedded: %v", err)
	}
	for _, f := range files {
		b, _ := logoFS.ReadFile(f)
		if _, err := ui.ParseSVG(b); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
	for _, k := range vendorKeywords {
		if loadLogo(k.logo) == nil {
			t.Errorf("keyword logo %q is missing or unparsable", k.logo)
		}
		for _, w := range k.words {
			if w != strings.ToLower(w) {
				t.Errorf("keyword %q must be lower-case", w)
			}
		}
	}
}

// TestPeerCopyActions: a peer row's context menu copies its address and
// MagicDNS name, and the copy button copies the address without expanding
// the row.
func TestPeerCopyActions(t *testing.T) {
	a := demoApp(t)
	a.networks[0].MagicDNSZone = "et.net"
	a.expanded = map[string]bool{}
	tt := ui.NewTester(a.view, 1100, 720)
	tt.Frame()
	for _, tc := range []struct{ item, want string }{
		{"Copy IP address", "10.144.144.7"},
		{"Copy MagicDNS name", "phone.et.net"},
		{"Copy hostname", "phone"},
	} {
		if err := tt.RightClick("Phone"); err != nil {
			t.Fatal(err)
		}
		if err := tt.ChooseMenuItem(tc.item); err != nil {
			t.Fatalf("%s: %v (menu %v)", tc.item, err, tt.Menu())
		}
		tt.Frame()
		if got := tt.Clipboard(); got != tc.want {
			t.Errorf("%s copied %q, want %q", tc.item, got, tc.want)
		}
	}

	tt.SetClipboard("")
	a.mu.Lock()
	a.details["n1"].Peers = a.details["n1"].Peers[1:2] // only "nas", so its copy button is the one found
	a.mu.Unlock()
	tt.Frame()
	r, ok := tt.Find("NAS")
	if !ok {
		t.Fatal("NAS row not found")
	}
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	if _, ok := tt.Find("Copy IP address"); !ok {
		t.Fatal("copy button not found")
	}
	if err := tt.Click("Copy IP address"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := tt.Clipboard(); got != "10.144.144.1" {
		t.Errorf("copy button copied %q", got)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for k, v := range a.expanded {
		if v {
			t.Errorf("copy button expanded %s", k)
		}
	}
}

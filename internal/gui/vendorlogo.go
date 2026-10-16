package gui

import (
	"embed"
	"strings"
	"sync"
	"unicode"

	"github.com/egoist/mygo/ui"
)

// Vendor logos (sources and licenses: assets/logos/SOURCES.md and
// resources/NOTICE; doc/en-us/gui.md, "Peer rows").
//
//go:embed assets/logos/*.svg
var logoFS embed.FS

// vendorKeywords maps hostname words to logo files, in priority order: device
// makers and product lines first, then operating systems, cloud providers
// and VPNs. The first entry with a matching word wins.
var vendorKeywords = []struct {
	logo  string
	words []string
}{
	{"apple", []string{"apple", "mac", "macbook", "macmini", "macpro", "macstudio", "imac", "iphone", "ipad", "ipod", "mbp", "mba"}},
	{"intel", []string{"intel", "nuc"}},
	{"amd", []string{"amd", "ryzen", "epyc", "threadripper"}},
	{"nvidia", []string{"nvidia", "jetson"}},
	{"qualcomm", []string{"qualcomm", "snapdragon"}},
	{"mediatek", []string{"mediatek"}},
	{"broadcom", []string{"broadcom"}},
	{"raspberrypi", []string{"raspberrypi", "raspberry", "rpi"}},
	{"microsoft", []string{"microsoft", "surface", "xbox"}},
	{"lenovo", []string{"lenovo", "thinkpad", "thinkcentre", "thinkstation", "ideapad", "legion"}},
	{"dell", []string{"dell", "xps", "optiplex", "poweredge", "latitude", "alienware"}},
	{"hp", []string{"hp", "elitebook", "probook", "elitedesk", "prodesk", "proliant", "omen"}},
	{"asus", []string{"asus", "rog", "zenbook", "vivobook"}},
	{"acer", []string{"acer", "aspire", "predator"}},
	{"samsung", []string{"samsung", "galaxy"}},
	{"google", []string{"google", "pixel"}},
	{"huawei", []string{"huawei", "matebook"}},
	{"xiaomi", []string{"xiaomi", "redmi"}},
	{"oneplus", []string{"oneplus"}},
	{"sony", []string{"sony", "playstation", "ps4", "ps5"}},
	{"nintendo", []string{"nintendo"}},
	{"valve", []string{"valve", "steamdeck"}},
	{"razer", []string{"razer"}},
	{"lg", []string{"lg"}},
	{"toshiba", []string{"toshiba", "dynabook"}},
	{"arm", []string{"arm"}},
	{"linksys", []string{"linksys"}},
	{"netgear", []string{"netgear", "orbi", "nighthawk"}},
	{"tplink", []string{"tplink", "deco", "archer"}},
	{"glinet", []string{"glinet"}},
	{"ubiquiti", []string{"ubiquiti", "unifi", "ubnt", "edgerouter", "udm"}},
	{"westerndigital", []string{"westerndigital", "wd"}},
	{"seagate", []string{"seagate"}},
	{"kingston", []string{"kingston"}},
	{"sandisk", []string{"sandisk"}},
	{"kioxia", []string{"kioxia"}},
	{"micron", []string{"micron", "crucial"}},
	{"skhynix", []string{"skhynix", "hynix"}},
	{"openwrt", []string{"openwrt", "lede", "immortalwrt"}},
	{"windows", []string{"windows", "win", "wsl"}},
	{"macos", []string{"macos", "osx"}},
	{"ios", []string{"ios", "ipados"}},
	{"android", []string{"android"}},
	{"chromeos", []string{"chromeos", "chromebook"}},
	{"harmonyos", []string{"harmonyos"}},
	{"ubuntu", []string{"ubuntu"}},
	{"debian", []string{"debian"}},
	{"fedora", []string{"fedora"}},
	{"archlinux", []string{"archlinux", "arch"}},
	{"freebsd", []string{"freebsd"}},
	{"linux", []string{"linux"}},
	{"amazonaws", []string{"aws", "ec2", "lightsail", "amazon"}},
	{"googlecloud", []string{"googlecloud", "gcp", "gce"}},
	{"oracle", []string{"oracle", "oci"}},
	{"digitalocean", []string{"digitalocean", "droplet"}},
	{"vultr", []string{"vultr"}},
	{"hetzner", []string{"hetzner"}},
	{"akamai", []string{"akamai", "linode"}},
	{"cloudflare", []string{"cloudflare"}},
	{"ovh", []string{"ovh", "ovhcloud"}},
	{"tailscale", []string{"tailscale"}},
	{"zerotier", []string{"zerotier"}},
	{"wireguard", []string{"wireguard"}},
}

// logoCache holds parsed logos by file name; a nil entry records a file that
// failed to parse, so it is tried once.
var logoCache = struct {
	sync.Mutex
	m map[string]*ui.SVG
}{m: map[string]*ui.SVG{}}

// vendorLogoName returns the logo file (without ".svg") a hostname names, or
// "" when no keyword matches.
// Notes: only whole words match (see hostWords), so "hp" does not match
// "php-dev" and "lg" does not match "algo".
func vendorLogoName(host string) string {
	words := hostWords(host)
	for _, k := range vendorKeywords {
		for _, w := range k.words {
			if words[w] {
				return k.logo
			}
		}
	}
	return ""
}

// vendorLogo returns the parsed logo for a hostname, or nil when none
// matches.
// Side effects: parses the SVG on first use and caches it.
func vendorLogo(host string) *ui.SVG {
	name := vendorLogoName(host)
	if name == "" {
		return nil
	}
	return loadLogo(name)
}

// loadLogo parses an embedded logo once.
// Returns: nil for a missing or unparsable file.
func loadLogo(name string) *ui.SVG {
	logoCache.Lock()
	defer logoCache.Unlock()
	if s, ok := logoCache.m[name]; ok {
		return s
	}
	var s *ui.SVG
	if b, err := logoFS.ReadFile("assets/logos/" + name + ".svg"); err == nil {
		if p, err := ui.ParseSVG(b); err == nil {
			s = p
		}
	}
	logoCache.m[name] = s
	return s
}

// hostWords splits a hostname into lower-case words for logo matching.
// Returns: a set holding each part between separators ("-", "_", ".",
// spaces), each pair of neighboring parts joined ("tp-link" → "tplink"), and
// the pieces of a part split by splitCase ("NUC8i5BEH" → "nuc", "i", "beh";
// "MacMini" → "mac", "mini"; "iPhone15" → "iphone").
func hostWords(host string) map[string]bool {
	words := map[string]bool{}
	parts := strings.FieldsFunc(host, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	for i, p := range parts {
		words[strings.ToLower(p)] = true
		if i > 0 {
			words[strings.ToLower(parts[i-1]+p)] = true
		}
		for _, s := range splitCase(p, false) {
			words[strings.ToLower(s)] = true
		}
		for _, s := range splitCase(p, true) {
			words[strings.ToLower(s)] = true
		}
	}
	return words
}

// splitCase splits a word where letters meet digits and, with camel set,
// also where a lower-case letter is followed by a capital ("iPhone15" →
// "iPhone", "15"; with camel → "i", "Phone", "15").
func splitCase(w string, camel bool) []string {
	var out []string
	rs := []rune(w)
	start := 0
	for i := 1; i < len(rs); i++ {
		a, b := rs[i-1], rs[i]
		if unicode.IsDigit(a) != unicode.IsDigit(b) || camel && unicode.IsLower(a) && unicode.IsUpper(b) {
			out = append(out, string(rs[start:i]))
			start = i
		}
	}
	return append(out, string(rs[start:]))
}

package gui

import (
	"strings"
	"unicode"
)

// nameWords are words whose written form a plain capitalization gets wrong,
// keyed by their lower-case spelling (doc/en-us/gui.md, "Peer rows").
var nameWords = map[string]string{
	"macbook": "MacBook", "imac": "iMac", "iphone": "iPhone", "ipad": "iPad", "ipod": "iPod",
	"macmini": "Mac mini", "macpro": "Mac Pro", "macstudio": "Mac Studio", "airpods": "AirPods",
	"macos": "macOS", "ios": "iOS", "ipados": "iPadOS",
	"nuc": "NUC", "nas": "NAS", "pc": "PC", "vps": "VPS", "vm": "VM", "wsl": "WSL", "rpi": "RPi",
	"thinkpad": "ThinkPad", "thinkcentre": "ThinkCentre", "thinkstation": "ThinkStation",
	"ideapad": "IdeaPad", "elitebook": "EliteBook", "probook": "ProBook", "matebook": "MateBook",
	"raspberrypi": "Raspberry Pi", "openwrt": "OpenWrt", "tplink": "TP-Link", "glinet": "GL.iNet",
	"unifi": "UniFi", "edgerouter": "EdgeRouter", "playstation": "PlayStation", "xbox": "Xbox",
	"chromebook": "Chromebook", "freebsd": "FreeBSD", "truenas": "TrueNAS", "pfsense": "pfSense",
	"opnsense": "OPNsense", "homeassistant": "Home Assistant", "oneplus": "OnePlus",
	"hp": "HP", "lg": "LG", "asus": "ASUS", "msi": "MSI", "amd": "AMD", "nvidia": "NVIDIA",
	"xps": "XPS", "aws": "AWS", "gcp": "GCP", "oci": "OCI", "ec2": "EC2", "ap": "AP", "nvr": "NVR",
}

// friendlyName turns a machine-style hostname into a name for people:
// "macbook-pro" → "MacBook Pro", "Intel-NUC8i5BEH" → "Intel NUC8i5BEH".
// Returns: the hostname unchanged unless it is made only of ASCII letters,
// digits, "-" and "_" with no empty parts.
// Notes: words already written with a capital keep their spelling, and
// lower-case words mixing letters and digits ("nuc8i5beh", "rpi4") are left
// alone, because their intended capitalization cannot be guessed. "mini"
// stays lower-case after "Mac" and "iPad", as Apple writes it.
func friendlyName(host string) string {
	if host == "" {
		return host
	}
	for _, r := range host {
		if !(r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) || r == '-' || r == '_') {
			return host
		}
	}
	parts := strings.FieldsFunc(host, func(r rune) bool { return r == '-' || r == '_' })
	if len(parts) != strings.Count(host, "-")+strings.Count(host, "_")+1 {
		return host // leading, trailing or doubled separators
	}
	out := make([]string, len(parts))
	for i, w := range parts {
		out[i] = friendlyWord(w, i > 0 && (out[i-1] == "Mac" || out[i-1] == "iPad"))
	}
	return strings.Join(out, " ")
}

// friendlyWord writes one hostname word for people (see friendlyName).
// Inputs: afterApple is set when the word follows "Mac" or "iPad".
func friendlyWord(w string, afterApple bool) string {
	if strings.ToLower(w) != w {
		return w // the user chose the capitalization
	}
	if s, ok := nameWords[w]; ok {
		return s
	}
	if w == "mini" && afterApple {
		return w
	}
	for _, r := range w {
		if !unicode.IsLetter(r) {
			return w // digits, or letters mixed with digits
		}
	}
	return strings.ToUpper(w[:1]) + w[1:]
}

// avatarInitial is the letter shown in a peer's avatar when no vendor logo
// matches: the first letter of its friendly name, upper-cased.
func avatarInitial(name string) string {
	for _, r := range name {
		return string(unicode.ToUpper(r))
	}
	return "P"
}

// magicDNSName is the name a peer resolves as through EasyTier's MagicDNS:
// "<hostname>.<zone>", lower-cased (DNS names are case-insensitive).
// Inputs: zone is NetworkStatus.MagicDNSZone, empty when MagicDNS is off.
// Returns: "" when MagicDNS is off or the hostname is not a DNS label
// (EasyTier publishes the hostname as it is, so a name with spaces or dots
// would not resolve as a single label).
func magicDNSName(host, zone string) string {
	if zone == "" || host == "" || len(host) > 63 {
		return ""
	}
	for _, r := range host {
		if !(r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) || r == '-' || r == '_') {
			return ""
		}
	}
	return strings.ToLower(host) + "." + zone
}

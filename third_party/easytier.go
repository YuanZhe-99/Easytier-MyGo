// Package third_party embeds the pinned EasyTier release manifest
// (easytier.json): the bundled tag, the versions verified with this build,
// and every platform asset with its SHA-256.
package third_party

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed easytier.json
var manifestJSON []byte

// Asset is one platform archive of an EasyTier release.
type Asset struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	// Dir is the archive's top-level directory holding the binaries.
	Dir string `json:"dir"`
}

// Manifest pins the EasyTier core shipped with this build.
type Manifest struct {
	Repository string           `json:"repository"`
	License    string           `json:"license"`
	Source     string           `json:"source"`
	Bundled    string           `json:"bundled"`
	Published  string           `json:"published"`
	Tested     []string         `json:"tested"`
	Assets     map[string]Asset `json:"assets"`
}

// EasyTier parses the embedded manifest.
// Returns: the manifest; an error only if the embedded file is malformed.
func EasyTier() (Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(manifestJSON, &m); err != nil {
		return m, fmt.Errorf("easytier.json: %w", err)
	}
	return m, nil
}

// Platform maps GOOS/GOARCH to the manifest's asset key, e.g. "linux-amd64".
func Platform(goos, goarch string) string { return goos + "-" + goarch }

// EasyTierPlatformName maps GOOS/GOARCH to EasyTier's asset naming:
// "linux-x86_64", "macos-aarch64", "windows-arm64" (Windows uses arm64,
// Linux and macOS use aarch64).
func EasyTierPlatformName(goos, goarch string) (string, bool) {
	osName := map[string]string{"linux": "linux", "darwin": "macos", "windows": "windows"}[goos]
	if osName == "" {
		return "", false
	}
	switch goarch {
	case "amd64":
		return osName + "-x86_64", true
	case "arm64":
		if goos == "windows" {
			return osName + "-arm64", true
		}
		return osName + "-aarch64", true
	}
	return "", false
}

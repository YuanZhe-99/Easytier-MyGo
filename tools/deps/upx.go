package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/cores"
)

const upxManifestPath = "third_party/upx.json"

// upxManifest pins the upx release bundled with the app, which the core
// manager uses to unpack downloaded EasyTier cores (UPX ships no macOS
// build; macOS cores are not UPX-packed).
type upxManifest struct {
	Version string              `json:"version"`
	License string              `json:"license"`
	Source  string              `json:"source"`
	Assets  map[string]upxAsset `json:"assets"`
}

type upxAsset struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

// upxAssetNames maps platforms to upx release asset names (Windows on Arm
// runs the x64 build).
func upxAssetNames(version string) map[string]string {
	v := strings.TrimPrefix(version, "v")
	return map[string]string{
		"linux-amd64":   fmt.Sprintf("upx-%s-amd64_linux.tar.xz", v),
		"linux-arm64":   fmt.Sprintf("upx-%s-arm64_linux.tar.xz", v),
		"windows-amd64": fmt.Sprintf("upx-%s-win64.zip", v),
		"windows-arm64": fmt.Sprintf("upx-%s-win64.zip", v),
	}
}

// updateUPX pins the latest upx release.
func updateUPX() error {
	rel, err := githubReleaseOf("upx/upx", "latest")
	if err != nil {
		return err
	}
	m := upxManifest{Version: rel.TagName, License: "GPL-2.0-or-later with the UPX exception",
		Source: "https://github.com/upx/upx/tree/" + rel.TagName, Assets: map[string]upxAsset{}}
	sums, err := releaseSums(rel)
	if err != nil {
		return err
	}
	for plat, name := range upxAssetNames(rel.TagName) {
		a, ok := rel.asset(name)
		if !ok || sums[name] == "" {
			return fmt.Errorf("upx %s: no checksummed asset %s", rel.TagName, name)
		}
		m.Assets[plat] = upxAsset{Name: name, URL: a.URL, SHA256: sums[name]}
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	fmt.Printf("upx pinned to %s\n", rel.TagName)
	return os.WriteFile(upxManifestPath, append(data, '\n'), 0o644)
}

// fetchUPX downloads the pinned upx for a platform into dest (as upx or
// upx.exe, with its license as upx-COPYING). It returns the binary's path,
// or "" for a platform without a upx build.
func fetchUPX(platform, dest string) (string, error) {
	data, err := os.ReadFile(upxManifestPath)
	if err != nil {
		return "", err
	}
	var m upxManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return "", err
	}
	a, ok := m.Assets[platform]
	if !ok {
		return "", nil
	}
	archive, err := download(a.URL)
	if err != nil {
		return "", err
	}
	if got := cores.SHA256(archive); got != a.SHA256 {
		return "", fmt.Errorf("%s: SHA-256 %s, manifest says %s", a.Name, got, a.SHA256)
	}
	bin := filepath.Join(dest, "upx")
	if strings.HasSuffix(a.Name, ".zip") {
		bin += ".exe"
		zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return "", err
		}
		for _, f := range zr.File {
			switch filepath.Base(f.Name) {
			case "upx.exe":
				err = writeZipFile(f, bin, 0o755)
			case "COPYING":
				err = writeZipFile(f, filepath.Join(dest, "upx-COPYING"), 0o644)
			}
			if err != nil {
				return "", err
			}
		}
		return bin, nil
	}
	// .tar.xz: the standard library has no xz reader; use tar(1).
	tmp, err := os.MkdirTemp("", "upx-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	cmd := exec.Command("tar", "-xJf", "-", "-C", tmp, "--strip-components=1")
	cmd.Stdin = bytes.NewReader(archive)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("unpack %s: %v: %s", a.Name, err, out)
	}
	for src, dst := range map[string]string{"upx": bin, "COPYING": filepath.Join(dest, "upx-COPYING")} {
		b, err := os.ReadFile(filepath.Join(tmp, src))
		if err != nil {
			return "", err
		}
		mode := os.FileMode(0o644)
		if src == "upx" {
			mode = 0o755
		}
		if err := os.WriteFile(dst, b, mode); err != nil {
			return "", err
		}
	}
	return bin, nil
}

func writeZipFile(f *zip.File, dst string, mode os.FileMode) error {
	r, err := f.Open()
	if err != nil {
		return err
	}
	defer r.Close()
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, mode)
}

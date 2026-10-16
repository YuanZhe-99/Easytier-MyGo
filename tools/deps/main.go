// Command deps keeps the two upstream pins current: the MyGo module and the
// bundled EasyTier release (third_party/easytier.json).
//
//	go run ./tools/deps check [-strict]          compare pins with the latest releases
//	go run ./tools/deps update [-easytier TAG]   move both pins (EasyTier: latest stable unless TAG)
//	go run ./tools/deps fetch [-platform P]      download, verify and unpack the bundled core
//
// fetch unpacks into resources/<goos>-<goarch>/bin, where `mygo build`
// picks the files up. Run from the repository root.
package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/YuanZhe-99/Easytier-MyGo/third_party"
)

const (
	manifestPath = "third_party/easytier.json"
	mygoModule   = "github.com/egoist/mygo"
)

// platforms lists the asset keys this project ships.
var platforms = []struct{ goos, goarch string }{
	{"linux", "amd64"}, {"linux", "arm64"},
	{"windows", "amd64"}, {"windows", "arm64"},
	{"darwin", "amd64"}, {"darwin", "arm64"},
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "check":
		err = check(os.Args[2:])
	case "update":
		err = update(os.Args[2:])
	case "fetch":
		err = fetch(os.Args[2:])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "deps:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: go run ./tools/deps check [-strict] | update [-easytier TAG] | fetch [-platform goos-goarch] [-out dir]")
	os.Exit(2)
}

// check prints the pinned and latest versions.
// Side effects: network requests to the Go proxy and the GitHub API.
// Notes: with -strict a newer upstream version is an error (CI uses it).
func check(args []string) error {
	fs := flag.NewFlagSet("check", flag.ExitOnError)
	strict := fs.Bool("strict", false, "fail when an upstream pin is outdated")
	fs.Parse(args)

	curMyGo, latestMyGo, err := mygoVersions()
	if err != nil {
		return err
	}
	m, err := third_party.EasyTier()
	if err != nil {
		return err
	}
	rel, err := githubRelease("latest")
	if err != nil {
		return err
	}
	outdated := false
	report := func(name, cur, latest string) {
		state := "current"
		if cur != latest {
			state = "UPDATE AVAILABLE"
			outdated = true
		}
		fmt.Printf("%-9s pinned %-10s latest %-10s %s\n", name, cur, latest, state)
	}
	report("MyGo", curMyGo, latestMyGo)
	report("EasyTier", m.Bundled, rel.TagName)
	if outdated && *strict {
		return errors.New("upstream pins are outdated")
	}
	return nil
}

// update moves MyGo to its latest version and rewrites the EasyTier
// manifest for the latest stable release (or -easytier TAG).
// Side effects: runs `go get`/`go mod tidy`, rewrites third_party/easytier.json.
// Notes: "tested" is kept as is; add a tag only after the live checks pass.
func update(args []string) error {
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	tag := fs.String("easytier", "", "EasyTier tag (default: latest stable)")
	skipMyGo := fs.Bool("skip-mygo", false, "leave the MyGo pin alone")
	fs.Parse(args)

	if !*skipMyGo {
		for _, cmd := range [][]string{{"go", "get", mygoModule + "@latest"}, {"go", "mod", "tidy"}} {
			c := exec.Command(cmd[0], cmd[1:]...)
			c.Stdout, c.Stderr = os.Stdout, os.Stderr
			if err := c.Run(); err != nil {
				return fmt.Errorf("%s: %w", strings.Join(cmd, " "), err)
			}
		}
	}

	which := "latest"
	if *tag != "" {
		which = "tags/" + *tag
	}
	rel, err := githubRelease(which)
	if err != nil {
		return err
	}
	old, err := third_party.EasyTier()
	if err != nil {
		return err
	}
	sums, err := releaseSums(rel)
	if err != nil {
		return err
	}
	m := third_party.Manifest{
		Repository: "EasyTier/EasyTier",
		License:    "LGPL-3.0",
		Source:     "https://github.com/EasyTier/EasyTier/tree/" + rel.TagName,
		Bundled:    rel.TagName,
		Published:  rel.PublishedAt.Format("2006-01-02"),
		Tested:     old.Tested,
		Assets:     map[string]third_party.Asset{},
	}
	for _, p := range platforms {
		up, _ := third_party.EasyTierPlatformName(p.goos, p.goarch)
		name := fmt.Sprintf("easytier-%s-%s.zip", up, rel.TagName)
		a, ok := rel.asset(name)
		if !ok {
			return fmt.Errorf("release %s has no asset %s", rel.TagName, name)
		}
		sum := sums[name]
		if sum == "" {
			return fmt.Errorf("no SHA-256 published for %s", name)
		}
		m.Assets[third_party.Platform(p.goos, p.goarch)] = third_party.Asset{
			Name: name, URL: a.URL, SHA256: sum, Size: a.Size, Dir: "easytier-" + up,
		}
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(manifestPath, append(data, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Printf("EasyTier pinned to %s (tested: %s)\n", rel.TagName, strings.Join(m.Tested, ", "))
	return nil
}

// fetch downloads the bundled archive for one platform, verifies it and
// unpacks the binaries into <out>/<goos>-<goarch>/bin.
// Side effects: network download; replaces the target bin directory.
func fetch(args []string) error {
	fs := flag.NewFlagSet("fetch", flag.ExitOnError)
	platform := fs.String("platform", third_party.Platform(runtime.GOOS, runtime.GOARCH), "goos-goarch")
	out := fs.String("out", "resources", "resources directory")
	upx := fs.String("upx", "", "upx binary used to unpack the release's UPX-packed executables (default: upx from PATH; \"none\" keeps them packed)")
	fs.Parse(args)

	m, err := third_party.EasyTier()
	if err != nil {
		return err
	}
	a, ok := m.Assets[*platform]
	if !ok {
		return fmt.Errorf("no EasyTier asset for %s", *platform)
	}
	data, err := download(a.URL)
	if err != nil {
		return err
	}
	if got := sha256Hex(data); got != a.SHA256 {
		return fmt.Errorf("%s: SHA-256 %s, manifest says %s", a.Name, got, a.SHA256)
	}
	dest := filepath.Join(*out, *platform, "bin")
	if err := os.RemoveAll(dest); err != nil {
		return err
	}
	if err := unpack(data, a.Dir, dest); err != nil {
		return err
	}
	if err := unpackUPX(dest, *upx); err != nil {
		return err
	}
	fmt.Printf("EasyTier %s for %s → %s\n", m.Bundled, *platform, dest)
	return nil
}

// unpackUPX decompresses the UPX-packed executables in dir. EasyTier's
// release binaries are UPX-packed, which costs ~250 ms of CPU on every
// start (measured with v2.6.4); unpacked, an easytier-cli query takes ~3 ms.
// Side effects: rewrites the executables in place.
// Notes: without upx the files stay packed and a warning is printed.
func unpackUPX(dir, upx string) error {
	if upx == "none" {
		return nil
	}
	if upx == "" {
		found, err := exec.LookPath("upx")
		if err != nil {
			fmt.Fprintln(os.Stderr, "deps: warning: upx not found; binaries stay UPX-packed (slow to start)")
			return nil
		}
		upx = found
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if !bytes.Contains(data[:min(len(data), 4096)], []byte("UPX!")) {
			continue
		}
		c := exec.Command(upx, "-d", "-q", p)
		c.Stderr = os.Stderr
		if err := c.Run(); err != nil {
			return fmt.Errorf("upx -d %s: %w", e.Name(), err)
		}
	}
	return nil
}

// unpack extracts the files under dir/ in a zip archive into dest, keeping
// their permission bits (the archives store 0755 for the binaries).
// Notes: the web console binaries are skipped; this app does not use them.
func unpack(data []byte, dir, dest string) error {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	n := 0
	for _, f := range zr.File {
		rel, ok := strings.CutPrefix(f.Name, dir+"/")
		if !ok || rel == "" || f.FileInfo().IsDir() || strings.Contains(rel, "/") {
			continue
		}
		if strings.HasPrefix(rel, "easytier-web") {
			continue
		}
		if rel != path.Clean(rel) || strings.HasPrefix(rel, ".") {
			return fmt.Errorf("unexpected archive entry %q", f.Name)
		}
		mode := f.Mode().Perm()
		if mode == 0 {
			mode = 0o644
		}
		if err := extract(f, filepath.Join(dest, rel), mode); err != nil {
			return err
		}
		n++
	}
	if n == 0 {
		return fmt.Errorf("archive has no files under %s/", dir)
	}
	return nil
}

func extract(f *zip.File, target string, mode os.FileMode) error {
	r, err := f.Open()
	if err != nil {
		return err
	}
	defer r.Close()
	w, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, r); err != nil {
		w.Close()
		return err
	}
	return w.Close()
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// mygoVersions returns the pinned and the latest MyGo versions.
func mygoVersions() (current, latest string, err error) {
	outp, err := exec.Command("go", "list", "-m", "-u", "-json", mygoModule).Output()
	if err != nil {
		return "", "", fmt.Errorf("go list %s: %w", mygoModule, err)
	}
	var mod struct {
		Version string
		Update  *struct{ Version string }
	}
	if err := json.Unmarshal(outp, &mod); err != nil {
		return "", "", err
	}
	latest = mod.Version
	if mod.Update != nil {
		latest = mod.Update.Version
	}
	return mod.Version, latest, nil
}

type release struct {
	TagName     string    `json:"tag_name"`
	PublishedAt time.Time `json:"published_at"`
	Assets      []struct {
		Name   string `json:"name"`
		URL    string `json:"browser_download_url"`
		Size   int64  `json:"size"`
		Digest string `json:"digest"`
	} `json:"assets"`
}

type releaseAsset struct {
	URL  string
	Size int64
}

func (r release) asset(name string) (releaseAsset, bool) {
	for _, a := range r.Assets {
		if a.Name == name {
			return releaseAsset{URL: a.URL, Size: a.Size}, true
		}
	}
	return releaseAsset{}, false
}

// releaseSums collects SHA-256 values: the API's per-asset digest, else a
// SHA256SUMS asset (published since v2.7.0).
func releaseSums(r release) (map[string]string, error) {
	sums := map[string]string{}
	for _, a := range r.Assets {
		if v, ok := strings.CutPrefix(a.Digest, "sha256:"); ok {
			sums[a.Name] = v
		}
	}
	for _, a := range r.Assets {
		if a.Name != "SHA256SUMS" {
			continue
		}
		data, err := download(a.URL)
		if err != nil {
			return nil, err
		}
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 {
				name := strings.TrimPrefix(fields[1], "*")
				if sums[name] == "" {
					sums[name] = fields[0]
				}
			}
		}
	}
	return sums, nil
}

// githubRelease reads /releases/<which> of EasyTier ("latest" is the
// newest stable release; pre-releases are never "latest").
func githubRelease(which string) (release, error) {
	var r release
	req, err := http.NewRequest("GET", "https://api.github.com/repos/EasyTier/EasyTier/releases/"+which, nil)
	if err != nil {
		return r, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if tok := os.Getenv("GITHUB_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return r, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return r, fmt.Errorf("GitHub releases/%s: %s", which, resp.Status)
	}
	return r, json.NewDecoder(resp.Body).Decode(&r)
}

var httpClient = &http.Client{Timeout: 5 * time.Minute}

func download(url string) ([]byte, error) {
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 256<<20))
}

// Package cores manages EasyTier cores: the bundled one, official releases
// downloaded on request, and custom builds. A core is a directory holding
// a matching easytier-core and easytier-cli (plus DLLs on Windows) from one
// release.
//
// Nothing here downloads on its own: callers decide when (the user asks).
// Every official download is verified against the SHA-256 GitHub publishes
// for the asset, and UPX-packed executables are unpacked once at install,
// because a packed easytier-cli costs ~250 ms of CPU per call.
package cores

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/fsutil"
	"github.com/YuanZhe-99/Easytier-MyGo/third_party"
)

// Source says where a core came from.
type Source string

const (
	SourceBundled  Source = "bundled"
	SourceOfficial Source = "official"
	SourceCustom   Source = "custom"
)

// BundledTag names the bundled core in selections.
const BundledTag = "bundled"

// Kind tells the full EasyTier core from EasyTier mini, the compact client
// built from the same source (EasyTier/easytier-mini): one executable,
// TCP and UDP only, a fixed management port, no --check-config, and no
// CLI of its own, so a mini core carries the full release's easytier-cli
// of the same tag.
type Kind string

const (
	KindFull Kind = ""
	KindMini Kind = "mini"
)

// MiniPrefix starts the tag of an installed mini core ("mini-v2.7.0").
const MiniPrefix = "mini-"

// MiniRPC is EasyTier mini's management portal; it cannot be changed, so
// one mini core runs per computer.
const MiniRPC = "127.0.0.1:15888"

// Core is one installed core.
type Core struct {
	Tag    string `json:"tag"`    // release tag, BundledTag, a custom name, or MiniPrefix+tag
	Dir    string `json:"dir"`    // holds the core and easytier-cli
	Source Source `json:"source"` // bundled, official, custom
	Kind   Kind   `json:"kind,omitempty"`
	Tested bool   `json:"tested"` // verified with this build of the app
	Pre    bool   `json:"pre,omitempty"`
}

// Release returns the EasyTier release tag the core was built from ("" for
// a custom build; the bundled core's pinned tag).
func (c Core) Release() string {
	switch c.Source {
	case SourceBundled:
		man, _ := third_party.EasyTier()
		return man.Bundled
	case SourceOfficial:
		return strings.TrimPrefix(c.Tag, MiniPrefix)
	}
	return ""
}

// Binaries returns the core's executable and its CLI.
func (c Core) Binaries() (core, cli string) {
	if c.Kind == KindMini {
		_, cli = Binaries(c.Dir)
		return filepath.Join(c.Dir, exe("easytier-mini")), cli
	}
	return Binaries(c.Dir)
}

// Release is an official EasyTier release with this platform's asset.
type Release struct {
	Tag        string    `json:"tag"`
	Prerelease bool      `json:"prerelease"`
	Published  time.Time `json:"published"`
	Asset      string    `json:"asset"`
	URL        string    `json:"url"`
	SHA256     string    `json:"sha256"`
	Size       int64     `json:"size"`
	// dir is the archive's top-level directory ("" for mini archives,
	// whose files sit at the root).
	Dir  string `json:"dir"`
	Kind Kind   `json:"kind,omitempty"`
}

// Manager keeps cores under one directory (<Dir>/<tag>/).
type Manager struct {
	// Dir holds downloaded and custom cores and the release cache.
	Dir string
	// BundledDir holds the bundled core (next to the daemon binary).
	BundledDir string
	// UPX unpacks downloaded executables ("" disables unpacking).
	UPX string
	// HTTP is the client for GitHub (nil: a client with a timeout).
	HTTP *http.Client
	// API and MiniAPI are the release endpoints of EasyTier and EasyTier
	// mini (tests override them).
	API, MiniAPI string
	// GOOS/GOARCH select the asset (default: this machine).
	GOOS, GOARCH string
}

func (m *Manager) client() *http.Client {
	if m.HTTP != nil {
		return m.HTTP
	}
	return &http.Client{Timeout: 10 * time.Minute}
}

func (m *Manager) platform() (string, string) {
	goos, goarch := m.GOOS, m.GOARCH
	if goos == "" {
		goos = runtime.GOOS
	}
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	return goos, goarch
}

func exe(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

// Binaries returns the core and CLI paths inside a core directory.
func Binaries(dir string) (core, cli string) {
	return filepath.Join(dir, exe("easytier-core")), filepath.Join(dir, exe("easytier-cli"))
}

// List returns the bundled core (when present) followed by the installed
// ones, newest tag first.
func (m *Manager) List() []Core {
	man, _ := third_party.EasyTier()
	tested := func(c Core) bool {
		if c.Kind == KindMini {
			return slices.Contains(man.MiniTested, c.Release())
		}
		return slices.Contains(man.Tested, c.Tag)
	}
	var out []Core
	if m.BundledDir != "" {
		if core, _ := Binaries(m.BundledDir); exists(core) {
			out = append(out, Core{Tag: BundledTag, Dir: m.BundledDir, Source: SourceBundled, Tested: true})
		}
	}
	entries, _ := os.ReadDir(m.Dir)
	var installed []Core
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		var meta Core
		if ok, err := fsutil.ReadJSON(filepath.Join(m.Dir, e.Name(), "core.json"), &meta); !ok || err != nil {
			continue
		}
		meta.Dir = filepath.Join(m.Dir, e.Name())
		meta.Tested = meta.Source == SourceOfficial && tested(meta)
		installed = append(installed, meta)
	}
	slices.SortFunc(installed, func(a, b Core) int { return strings.Compare(b.Tag, a.Tag) })
	return append(out, installed...)
}

// Find returns an installed core by tag ("" or BundledTag: the bundled one).
func (m *Manager) Find(tag string) (Core, bool) {
	if tag == "" {
		tag = BundledTag
	}
	for _, c := range m.List() {
		if c.Tag == tag {
			return c, true
		}
	}
	return Core{}, false
}

// NewestMini returns the newest installed EasyTier mini core, which every
// network made for mini runs on.
func (m *Manager) NewestMini() (Core, bool) {
	for _, c := range m.List() { // newest tag first
		if c.Kind == KindMini {
			return c, true
		}
	}
	return Core{}, false
}

// Remove deletes an installed (non-bundled) core.
func (m *Manager) Remove(tag string) error {
	if tag == "" || tag == BundledTag || strings.ContainsAny(tag, `/\`) || tag == "." || tag == ".." {
		return fmt.Errorf("cannot remove core %q", tag)
	}
	return os.RemoveAll(filepath.Join(m.Dir, tag))
}

// apiRelease is the part of GitHub's release JSON this package reads.
type apiRelease struct {
	TagName     string    `json:"tag_name"`
	Prerelease  bool      `json:"prerelease"`
	Draft       bool      `json:"draft"`
	PublishedAt time.Time `json:"published_at"`
	Assets      []struct {
		Name   string `json:"name"`
		URL    string `json:"browser_download_url"`
		Size   int64  `json:"size"`
		Digest string `json:"digest"`
	} `json:"assets"`
}

type releaseCache struct {
	ETag string       `json:"etag"`
	Body []apiRelease `json:"body"`
}

const (
	defaultAPI     = "https://api.github.com/repos/EasyTier/EasyTier/releases?per_page=20"
	defaultMiniAPI = "https://api.github.com/repos/EasyTier/easytier-mini/releases?per_page=20"
)

// Releases lists official EasyTier releases that have an asset for this
// platform, newest first (see ReleasesOf).
func (m *Manager) Releases(ctx context.Context, includePre bool) ([]Release, error) {
	return m.ReleasesOf(ctx, KindFull, includePre)
}

// ReleasesOf lists the official releases of one kind that have an asset
// for this platform, newest first. Pre-releases are included only when
// asked for. The GitHub response is cached with its ETag, so a repeated
// check costs no quota.
// Side effects: one network request; updates <Dir>/releases.json (or
// releases-mini.json).
func (m *Manager) ReleasesOf(ctx context.Context, kind Kind, includePre bool) ([]Release, error) {
	api, cacheName := m.API, "releases.json"
	if api == "" {
		api = defaultAPI
	}
	if kind == KindMini {
		api, cacheName = m.MiniAPI, "releases-mini.json"
		if api == "" {
			api = defaultMiniAPI
		}
	}
	cachePath := filepath.Join(m.Dir, cacheName)
	var cache releaseCache
	_, _ = fsutil.ReadJSON(cachePath, &cache)

	req, err := http.NewRequestWithContext(ctx, "GET", api, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if cache.ETag != "" {
		req.Header.Set("If-None-Match", cache.ETag)
	}
	resp, err := m.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNotModified:
	case http.StatusOK:
		var body []apiRelease
		if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&body); err != nil {
			return nil, err
		}
		cache = releaseCache{ETag: resp.Header.Get("ETag"), Body: body}
		_ = fsutil.WriteJSON(cachePath, cache)
	default:
		return nil, fmt.Errorf("GitHub releases: %s", resp.Status)
	}
	goos, goarch := m.platform()
	var out []Release
	for _, r := range cache.Body {
		if r.Draft || (r.Prerelease && !includePre) {
			continue
		}
		if rel, ok := matchAsset(r, kind, goos, goarch); ok {
			out = append(out, rel)
		}
	}
	return out, nil
}

// matchAsset picks this platform's archive and its SHA-256 from a release.
func matchAsset(r apiRelease, kind Kind, goos, goarch string) (Release, bool) {
	up, ok := third_party.EasyTierPlatformName(goos, goarch)
	if !ok {
		return Release{}, false
	}
	name, dir := fmt.Sprintf("easytier-%s-%s.zip", up, r.TagName), "easytier-"+up
	if kind == KindMini {
		name, dir = fmt.Sprintf("easytier-mini-%s-%s.zip", up, r.TagName), ""
	}
	for _, a := range r.Assets {
		if a.Name != name {
			continue
		}
		sum, _ := strings.CutPrefix(a.Digest, "sha256:")
		return Release{Tag: r.TagName, Prerelease: r.Prerelease, Published: r.PublishedAt, Asset: a.Name,
			URL: a.URL, SHA256: sum, Size: a.Size, Dir: dir, Kind: kind}, sum != ""
	}
	return Release{}, false
}

// Install downloads a release, verifies it, unpacks the core and CLI into
// <Dir>/<tag>/ (<Dir>/mini-<tag>/ for mini) and records its metadata. A
// mini release also downloads the full release of the same tag, for its
// easytier-cli.
// Side effects: network downloads (tens of MB); replaces an existing copy.
func (m *Manager) Install(ctx context.Context, r Release) (Core, error) {
	data, err := m.download(ctx, r)
	if err != nil {
		return Core{}, err
	}
	if r.Kind != KindMini {
		c := Core{Tag: r.Tag, Source: SourceOfficial, Pre: r.Prerelease}
		return c, m.place(c, func(dir string) error { return Unpack(data, r.Dir, dir) })
	}
	full, err := m.Releases(ctx, true)
	if err != nil {
		return Core{}, err
	}
	i := slices.IndexFunc(full, func(f Release) bool { return f.Tag == r.Tag })
	if i < 0 {
		return Core{}, fmt.Errorf("EasyTier %s has no release for this platform, so its easytier-cli is missing", r.Tag)
	}
	cliData, err := m.download(ctx, full[i])
	if err != nil {
		return Core{}, err
	}
	c := Core{Tag: MiniPrefix + r.Tag, Source: SourceOfficial, Kind: KindMini, Pre: r.Prerelease}
	return c, m.place(c, func(dir string) error {
		if err := Unpack(data, "", dir); err != nil {
			return err
		}
		_, cli := Binaries(dir)
		return unpackOnly(cliData, full[i].Dir, dir, filepath.Base(cli))
	})
}

// download fetches a release archive and checks its published SHA-256.
func (m *Manager) download(ctx context.Context, r Release) ([]byte, error) {
	if r.SHA256 == "" {
		return nil, errors.New("release has no published SHA-256; refusing to install")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", r.URL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := m.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: %s", r.Asset, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 512<<20))
	if err != nil {
		return nil, err
	}
	if got := SHA256(data); got != r.SHA256 {
		return nil, fmt.Errorf("%s: SHA-256 %s does not match the published %s", r.Asset, got, r.SHA256)
	}
	return data, nil
}

// AddCustom copies a custom build (a directory with easytier-core and
// easytier-cli) under a name.
func (m *Manager) AddCustom(name, from string) (Core, error) {
	if name == "" || name == BundledTag || strings.ContainsAny(name, `/\ `) || strings.HasPrefix(name, ".") || strings.HasPrefix(name, MiniPrefix) {
		return Core{}, fmt.Errorf("invalid core name %q", name)
	}
	srcCore, srcCLI := Binaries(from)
	for _, p := range []string{srcCore, srcCLI} {
		if !exists(p) {
			return Core{}, fmt.Errorf("%s not found", p)
		}
	}
	c := Core{Tag: name, Source: SourceCustom}
	return c, m.place(c, func(dir string) error {
		dstCore, dstCLI := Binaries(dir)
		if err := copyFile(srcCore, dstCore); err != nil {
			return err
		}
		return copyFile(srcCLI, dstCLI)
	})
}

// place fills a fresh directory for c and swaps it into <Dir>/<tag>.
func (m *Manager) place(c Core, fill func(dir string) error) error {
	if err := os.MkdirAll(m.Dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(m.Dir, ".install-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err := os.Chmod(tmp, 0o755); err != nil {
		return err
	}
	if err := fill(tmp); err != nil {
		return err
	}
	if err := UnpackUPX(tmp, m.UPX); err != nil {
		return err
	}
	if err := fsutil.WriteJSON(filepath.Join(tmp, "core.json"), c); err != nil {
		return err
	}
	final := filepath.Join(m.Dir, c.Tag)
	if err := os.RemoveAll(final); err != nil {
		return err
	}
	return os.Rename(tmp, final)
}

// Unpack extracts the files under dir/ of a release zip ("" for the
// archive's root) into dest, keeping their permission bits. The web
// console binaries are skipped.
func Unpack(data []byte, dir, dest string) error {
	return unpackOnly(data, dir, dest, "")
}

// unpackOnly is Unpack limited to one file name ("" for every file).
func unpackOnly(data []byte, dir, dest, only string) error {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	prefix := ""
	if dir != "" {
		prefix = dir + "/"
	}
	n := 0
	for _, f := range zr.File {
		rel, ok := strings.CutPrefix(f.Name, prefix)
		if !ok || rel == "" || f.FileInfo().IsDir() || strings.Contains(rel, "/") {
			continue
		}
		if strings.HasPrefix(rel, "easytier-web") || only != "" && rel != only {
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
		what := "files"
		if only != "" {
			what = only
		}
		return fmt.Errorf("archive has no %s under %s/", what, dir)
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

// UnpackUPX decompresses the UPX-packed executables in dir with the upx
// binary ("" skips it; the files stay packed and slow to start).
func UnpackUPX(dir, upx string) error {
	if upx == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		if e.IsDir() || e.Name() == "upx" || e.Name() == "upx.exe" || !IsUPX(p) {
			continue
		}
		if out, err := exec.Command(upx, "-d", "-q", p).CombinedOutput(); err != nil {
			return fmt.Errorf("upx -d %s: %v: %s", e.Name(), err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

// IsUPX reports whether a file carries the UPX marker in its first 4 KiB.
func IsUPX(p string) bool {
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	head := make([]byte, 4096)
	n, _ := io.ReadFull(f, head)
	return bytes.Contains(head[:n], []byte("UPX!"))
}

// SHA256 returns the hex digest of data.
func SHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o755)
}

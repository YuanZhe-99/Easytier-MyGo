package cores

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// fakeZip builds a release archive like EasyTier's: one top-level
// directory with the binaries (0755) and the unused web console.
func fakeZip(t *testing.T, dir string) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range []string{exe("easytier-core"), exe("easytier-cli"), exe("easytier-web")} {
		h := &zip.FileHeader{Name: dir + "/" + name, Method: zip.Deflate}
		h.SetMode(0o755)
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(w, "#!/bin/sh\necho %s\n", name)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// fakeGitHub serves a release list and the assets; tamper corrupts the
// served archive.
func fakeGitHub(t *testing.T, tamper bool) *httptest.Server {
	archive := fakeZip(t, "easytier-linux-x86_64")
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases":
			if r.Header.Get("If-None-Match") == `"v1"` {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", `"v1"`)
			rel := func(tag string, pre bool) map[string]any {
				return map[string]any{"tag_name": tag, "prerelease": pre, "published_at": "2026-10-01T00:00:00Z",
					"assets": []map[string]any{
						{"name": "easytier-linux-x86_64-" + tag + ".zip", "browser_download_url": srv.URL + "/asset", "size": len(archive), "digest": "sha256:" + SHA256(archive)},
						{"name": "easytier-windows-arm64-" + tag + ".zip", "browser_download_url": srv.URL + "/other", "size": 1, "digest": "sha256:00"},
					}}
			}
			json.NewEncoder(w).Encode([]any{rel("v2.7.0", true), rel("v2.6.4", false)})
		case "/asset":
			data := archive
			if tamper {
				data = append(append([]byte(nil), archive...), 0)
			}
			w.Write(data)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func manager(t *testing.T, srv *httptest.Server) *Manager {
	return &Manager{Dir: t.TempDir(), API: srv.URL + "/releases", GOOS: "linux", GOARCH: "amd64"}
}

func TestReleasesMatchPlatformAndChannel(t *testing.T) {
	srv := fakeGitHub(t, false)
	m := manager(t, srv)
	stable, err := m.Releases(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(stable) != 1 || stable[0].Tag != "v2.6.4" || stable[0].Dir != "easytier-linux-x86_64" {
		t.Fatalf("stable releases: %+v", stable)
	}
	// The second call is answered from the ETag cache (304).
	all, err := m.Releases(context.Background(), true)
	if err != nil || len(all) != 2 || !all[0].Prerelease {
		t.Fatalf("with pre-releases: %+v, %v", all, err)
	}
}

func TestInstallVerifiesAndUnpacks(t *testing.T) {
	srv := fakeGitHub(t, false)
	m := manager(t, srv)
	rels, _ := m.Releases(context.Background(), false)
	c, err := m.Install(context.Background(), rels[0])
	if err != nil {
		t.Fatal(err)
	}
	core, cli := Binaries(filepath.Join(m.Dir, c.Tag))
	for _, p := range []string{core, cli} {
		info, err := os.Stat(p)
		if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm()&0o100 == 0) {
			t.Fatalf("%s not installed as an executable: %v", p, err)
		}
	}
	if _, err := os.Stat(filepath.Join(m.Dir, c.Tag, exe("easytier-web"))); err == nil {
		t.Fatal("the web console must be skipped")
	}
	got, ok := m.Find("v2.6.4")
	if !ok || got.Source != SourceOfficial || !got.Tested {
		t.Fatalf("installed core: %+v ok=%v", got, ok)
	}
	if err := m.Remove("v2.6.4"); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Find("v2.6.4"); ok {
		t.Fatal("core still listed after removal")
	}
}

func TestTamperedDownloadIsRefused(t *testing.T) {
	srv := fakeGitHub(t, true)
	m := manager(t, srv)
	rels, _ := m.Releases(context.Background(), false)
	if _, err := m.Install(context.Background(), rels[0]); err == nil {
		t.Fatal("a download that does not match its SHA-256 was installed")
	}
	if _, ok := m.Find("v2.6.4"); ok {
		t.Fatal("a refused download left a core behind")
	}
}

func TestRemoveRefusesBundledAndPaths(t *testing.T) {
	m := &Manager{Dir: t.TempDir()}
	for _, tag := range []string{"", BundledTag, "../x", ".."} {
		if err := m.Remove(tag); err == nil {
			t.Errorf("Remove(%q) accepted", tag)
		}
	}
}

// A mini release installs EasyTier mini with the easytier-cli of the full
// release of the same tag, both checked against their published SHA-256.
func TestInstallMiniTakesCLIFromFullRelease(t *testing.T) {
	full := fakeZip(t, "easytier-linux-x86_64")
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	h := &zip.FileHeader{Name: exe("easytier-mini"), Method: zip.Deflate}
	h.SetMode(0o755)
	w, _ := zw.CreateHeader(h)
	fmt.Fprint(w, "#!/bin/sh\necho mini\n")
	zw.Close()
	mini := buf.Bytes()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asset := func(name, path string, data []byte) map[string]any {
			return map[string]any{"name": name, "browser_download_url": srv.URL + path, "size": len(data), "digest": "sha256:" + SHA256(data)}
		}
		switch r.URL.Path {
		case "/releases":
			json.NewEncoder(w).Encode([]any{map[string]any{"tag_name": "v2.7.0", "prerelease": true, "published_at": "2026-10-06T00:00:00Z",
				"assets": []any{asset("easytier-linux-x86_64-v2.7.0.zip", "/full", full)}}})
		case "/mini-releases":
			json.NewEncoder(w).Encode([]any{map[string]any{"tag_name": "v2.7.0", "prerelease": true, "published_at": "2026-10-06T00:00:00Z",
				"assets": []any{asset("easytier-mini-linux-x86_64-v2.7.0.zip", "/mini", mini)}}})
		case "/full":
			w.Write(full)
		case "/mini":
			w.Write(mini)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	m := manager(t, srv)
	m.MiniAPI = srv.URL + "/mini-releases"
	rels, err := m.ReleasesOf(context.Background(), KindMini, true)
	if err != nil || len(rels) != 1 || rels[0].Kind != KindMini || rels[0].Dir != "" {
		t.Fatalf("mini releases: %+v %v", rels, err)
	}
	c, err := m.Install(context.Background(), rels[0])
	if err != nil {
		t.Fatal(err)
	}
	got, ok := m.NewestMini()
	if !ok || got.Tag != "mini-v2.7.0" || got.Kind != KindMini || got.Release() != "v2.7.0" || c.Tag != got.Tag {
		t.Fatalf("installed mini: %+v ok=%v", got, ok)
	}
	core, cli := got.Binaries()
	if filepath.Base(core) != exe("easytier-mini") {
		t.Fatalf("mini executable: %s", core)
	}
	for _, p := range []string{core, cli} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s missing: %v", p, err)
		}
	}
	if old, _ := Binaries(got.Dir); exists(old) {
		t.Fatal("a mini core must not carry the full easytier-core")
	}
}

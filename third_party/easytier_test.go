package third_party

import "testing"

// TestManifestCoversShippedPlatforms: every platform we ship has a pinned,
// checksummed asset, and the bundled tag is among the tested ones.
func TestManifestCoversShippedPlatforms(t *testing.T) {
	m, err := EasyTier()
	if err != nil {
		t.Fatal(err)
	}
	tested := false
	for _, v := range m.Tested {
		tested = tested || v == m.Bundled
	}
	if !tested {
		t.Fatalf("bundled %s is not in tested %v", m.Bundled, m.Tested)
	}
	for _, goos := range []string{"linux", "windows", "darwin"} {
		for _, goarch := range []string{"amd64", "arm64"} {
			a, ok := m.Assets[Platform(goos, goarch)]
			if !ok || len(a.SHA256) != 64 || a.URL == "" {
				t.Errorf("%s/%s: asset %+v", goos, goarch, a)
			}
		}
	}
}

func TestEasyTierPlatformName(t *testing.T) {
	for in, want := range map[[2]string]string{
		{"linux", "amd64"}:   "linux-x86_64",
		{"linux", "arm64"}:   "linux-aarch64",
		{"darwin", "arm64"}:  "macos-aarch64",
		{"windows", "arm64"}: "windows-arm64",
	} {
		if got, _ := EasyTierPlatformName(in[0], in[1]); got != want {
			t.Errorf("%v = %q, want %q", in, got, want)
		}
	}
	if _, ok := EasyTierPlatformName("freebsd", "amd64"); ok {
		t.Error("unsupported OS accepted")
	}
}

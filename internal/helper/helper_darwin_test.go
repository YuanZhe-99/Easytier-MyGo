package helper

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestReplaceSigned checks that a self-update installs the app's binaries
// only when their code signature satisfies the installed copies'
// designated requirement, and leaves nothing staged either way
// (regression: release builds re-sign the binaries, so the bundle manifest
// never matched and every macOS update needed an administrator prompt).
func TestReplaceSigned(t *testing.T) {
	chownRoot = func(string) error { return nil } // tests run unprivileged
	// adhoc writes an ad hoc signed copy of a system tool for every binary.
	adhoc := func(dir, tool string) {
		for _, n := range Binaries {
			p := filepath.Join(dir, n)
			data, err := os.ReadFile(tool)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, data, 0o755); err != nil {
				t.Fatal(err)
			}
			if out, err := exec.Command("codesign", "-f", "-s", "-", p).CombinedOutput(); err != nil {
				t.Fatalf("codesign: %v: %s", err, out)
			}
		}
	}
	cases := []struct {
		name    string
		app     string // the app's binaries (echo prints a version of 0.0.0)
		running string
		wantErr bool
	}{
		{"same signature", "/bin/echo", "0.0.0", false},
		{"other signature", "/usr/bin/false", "0.0.0", true},
		{"older version", "/bin/echo", "0.1.0", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bin, app := t.TempDir(), t.TempDir()
			adhoc(bin, "/bin/echo")
			adhoc(app, c.app)
			before, _ := os.ReadFile(filepath.Join(bin, Binaries[0]))
			err := replaceSigned(bin, app, c.running)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, want error %v", err, c.wantErr)
			}
			if after, _ := os.ReadFile(filepath.Join(bin, Binaries[0])); c.wantErr && string(after) != string(before) {
				t.Error("a rejected update changed the installed binary")
			}
			if left, _ := filepath.Glob(filepath.Join(bin, "*.new")); len(left) > 0 {
				t.Errorf("staged copies left behind: %v", left)
			}
		})
	}
}

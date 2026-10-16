package helper

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestUnitsVerify checks the generated units with systemd-analyze when it
// is available (it validates syntax and the referenced executable).
func TestUnitsVerify(t *testing.T) {
	if !strings.Contains(ServiceUnit(), "run --host helper --idle-exit") || !strings.Contains(SocketUnit(), "ListenStream=/run/easytier-mygo/ctl.sock") {
		t.Fatal("units lost their essential lines")
	}
	analyze, err := exec.LookPath("systemd-analyze")
	if err != nil {
		t.Skip("systemd-analyze not available")
	}
	dir := t.TempDir()
	for name, text := range map[string]string{Unit + ".socket": SocketUnit(), Unit + ".service": ServiceUnit()} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out, _ := exec.Command(analyze, "verify", filepath.Join(dir, Unit+".socket"), filepath.Join(dir, Unit+".service")).CombinedOutput()
	for _, line := range strings.Split(string(out), "\n") {
		// The executable is not installed on the test machine; everything
		// else must verify cleanly.
		if line == "" || strings.Contains(line, svcPath()) || strings.Contains(line, "is not executable") {
			continue
		}
		t.Errorf("systemd-analyze: %s", line)
	}
}

// TestRemoveWithParent checks that uninstalling removes the product folder
// only once nothing else is left in it (regression: Windows left empty
// `EasyTier MyGo` folders behind).
func TestRemoveWithParent(t *testing.T) {
	cases := []struct {
		name          string
		dir, sibling  bool // what exists under the parent beforehand
		parentRemains bool
	}{
		{"only dir", true, false, false},
		{"dir and sibling", true, true, true},
		{"dir already gone", false, false, false},
		{"dir gone, sibling stays", false, true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			parent := filepath.Join(t.TempDir(), "EasyTier MyGo")
			dir := filepath.Join(parent, "helper")
			if err := os.MkdirAll(parent, 0o755); err != nil {
				t.Fatal(err)
			}
			if c.dir {
				if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "sub", "f"), []byte("x"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if c.sibling {
				if err := os.Mkdir(filepath.Join(parent, "state"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if err := removeWithParent(dir); err != nil {
				t.Fatal(err)
			}
			if fileExists(dir) {
				t.Error("dir still exists")
			}
			if got := fileExists(parent); got != c.parentRemains {
				t.Errorf("parent exists = %v, want %v", got, c.parentRemains)
			}
			// A second call (repeated uninstall) must succeed too.
			if err := removeWithParent(dir); err != nil {
				t.Errorf("repeat: %v", err)
			}
		})
	}
}

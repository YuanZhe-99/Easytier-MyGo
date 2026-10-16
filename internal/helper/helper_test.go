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

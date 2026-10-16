package localapi

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestListenSocketPathLength guards the readable error for socket paths
// over the kernel's sun_path limit (bind fails with EINVAL on macOS).
func TestListenSocketPathLength(t *testing.T) {
	dir := t.TempDir()
	long := filepath.Join(dir, strings.Repeat("d", maxSocketPath), "ctl.sock")
	if _, err := Listen(long); err == nil || !strings.Contains(err.Error(), "longer than") {
		t.Fatalf("Listen(%d-byte path) = %v, want a path-length error", len(long), err)
	}
}

//go:build !windows

package localapi

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// SystemSocketPath is the privileged helper's socket.
var SystemSocketPath = func() string {
	if runtime.GOOS == "darwin" {
		return "/var/run/easytier-mygo/ctl.sock"
	}
	return "/run/easytier-mygo/ctl.sock"
}()

func userRuntimeDir() string {
	if base := os.Getenv("XDG_RUNTIME_DIR"); base != "" {
		return base
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("easytier-mygo-%d", os.Getuid()))
}

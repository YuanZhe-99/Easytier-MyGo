package localapi

import (
	"os"
	"path/filepath"
)

// SystemSocketPath is the privileged helper's AF_UNIX socket, in a
// directory only SYSTEM and Administrators may write.
var SystemSocketPath = filepath.Join(os.Getenv("ProgramData"), "EasyTier MyGo", "ctl.sock")

func userRuntimeDir() string {
	if base := os.Getenv("LOCALAPPDATA"); base != "" {
		return base
	}
	return os.TempDir()
}

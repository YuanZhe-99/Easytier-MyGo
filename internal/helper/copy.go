package helper

import (
	"os"
	"runtime"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/fsutil"
)

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// copyRootOwned copies an executable to dst atomically, owned by root
// (Unix) so only an administrator can replace it.
func copyRootOwned(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := fsutil.WriteFileAtomic(dst, data, 0o755); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		return nil
	}
	return os.Chown(dst, 0, 0)
}

// exeName adds .exe on Windows.
func exeName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

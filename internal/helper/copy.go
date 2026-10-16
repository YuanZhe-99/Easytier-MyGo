package helper

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
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

// removeWithParent deletes dir and everything below it, then its parent
// when that is left empty.
//
// Inputs: dir, a directory that may be absent.
// Side effects: removes files; the parent stays when anything else is in it.
// Notes: a missing dir or parent is not an error, so a repeated uninstall
// succeeds.
func removeWithParent(dir string) error {
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	parent := filepath.Dir(dir)
	entries, err := os.ReadDir(parent)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil || len(entries) > 0 {
		return err
	}
	if err := os.Remove(parent); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

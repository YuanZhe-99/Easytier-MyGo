package helper

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/bundlesig"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/fsutil"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/version"
)

// fileExists reports whether p exists.
func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// copyRootOwned copies an executable to dst atomically, owned by root
// (Unix) so only an administrator can replace it.
// Notes: reads src once; used by Install, where an administrator approved
// the source. A signed self-update writes its verified bytes through
// installBundle instead.
func copyRootOwned(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return writeRootOwned(dst, data)
}

// chownRoot gives p to root; tests replace it to run unprivileged.
var chownRoot = func(p string) error { return os.Chown(p, 0, 0) }

// writeRootOwned writes data to dst atomically as an executable, owned by
// root (Unix) so only an administrator can replace it.
func writeRootOwned(dst string, data []byte) error {
	if err := fsutil.WriteFileAtomic(dst, data, 0o755); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		return nil
	}
	return chownRoot(dst)
}

// verifiedBundle reads the signed bundle in from and checks that it is not
// older than the running helper.
//
// Inputs: from, the user-writable app directory; running, this helper's
// version.
// Returns: the verified bundle, whose Data is what SelfUpdate installs.
func verifiedBundle(from, running string) (bundlesig.Bundle, error) {
	key, err := bundlesig.PublicKey()
	if err != nil {
		return bundlesig.Bundle{}, err
	}
	b, err := bundlesig.Read(from, key)
	if err != nil {
		return b, err
	}
	if version.Older(b.Version, running) {
		return b, fmt.Errorf("bundle %s is older than the helper %s", b.Version, running)
	}
	return b, nil
}

// installBundle writes a verified bundle's files into binDir.
//
// Inputs: b, from bundlesig.Read; binDir, the root-owned helper directory.
// Side effects: replaces files in binDir. On Windows a running executable
// cannot be overwritten, so each old file is renamed aside to <name>.old
// first and put back when its write fails.
// Notes: writes exactly b.Data, never re-reading the app directory, so a
// file swapped there after verification is not installed.
func installBundle(binDir string, b bundlesig.Bundle) error {
	for _, name := range b.Names() {
		dst := filepath.Join(binDir, name)
		if runtime.GOOS != "windows" {
			if err := writeRootOwned(dst, b.Data[name]); err != nil {
				return err
			}
			continue
		}
		old := dst + ".old"
		_ = os.Remove(old)
		_ = os.Rename(dst, old)
		if err := writeRootOwned(dst, b.Data[name]); err != nil {
			_ = os.Rename(old, dst)
			return err
		}
	}
	return nil
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

package daemon

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/configdoc"
)

type draftMigration struct {
	Version     int      `json:"version"`
	Source      string   `json:"source"`
	Destination string   `json:"destination"`
	Files       []string `json:"files"`
}

// MigrateProfileDrafts copies a legacy library into a manifest worker's own
// config store. Invalid drafts and backup bytes are data, never activation.
func MigrateProfileDrafts(stateDir, manifestPath, configDir, id string) error {
	if runtime.GOOS != "linux" {
		return errors.New("draft migration is implemented only on Linux")
	}
	if !filepath.IsAbs(stateDir) || !filepath.IsAbs(configDir) {
		return errors.New("absolute state and config directories required")
	}
	manifest, err := readNetworkManifest(manifestPath)
	if err != nil {
		return err
	}
	var ids []string
	found := false
	for _, entry := range manifest.Networks {
		ids = append(ids, entry.ProfileID)
		found = found || entry.ProfileID == id
	}
	if !found {
		return errors.New("unknown profile target")
	}
	release, err := lockNetworkState(stateDir, ids)
	if err != nil {
		return err
	}
	defer release()
	source, err := filepath.EvalSymlinks(configDir)
	if err != nil {
		return err
	}
	destination := filepath.Join(source, "profiles", id)
	marker := filepath.Join(stateDir, "profiles", id, "draft-migration.json")
	if b, err := os.ReadFile(marker); err == nil {
		var done draftMigration
		if err := json.Unmarshal(b, &done); err != nil {
			return err
		}
		if done.Version != 1 || done.Source != source || done.Destination != destination {
			return errors.New("draft migration source changed")
		}
		return nil // completed copies never overwrite later edits/deletions
	} else if !os.IsNotExist(err) {
		return err
	}
	entries, err := os.ReadDir(source)
	if err != nil {
		return err
	}
	files := map[string][]byte{}
	read := func(relative string) error {
		path := filepath.Join(source, relative)
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("draft migration requires regular files")
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[relative] = b
		return nil
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), configdoc.Ext) {
			if err := configdoc.ValidName(strings.TrimSuffix(entry.Name(), configdoc.Ext)); err != nil {
				return err
			}
			if err := read(entry.Name()); err != nil {
				return err
			}
		}
	}
	backups := filepath.Join(source, configdoc.BackupsDir)
	if info, err := os.Lstat(backups); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("backup directory must be a real directory")
		}
		entries, err := os.ReadDir(backups)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := read(filepath.Join(configdoc.BackupsDir, entry.Name())); err != nil {
				return err
			}
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	// Refuse symlink destination components before copying; no caller-owned
	// content is replaced, including a partial previous copy that differs.
	for _, dir := range []string{filepath.Join(source, "profiles"), destination, filepath.Join(destination, configdoc.BackupsDir)} {
		if err := os.Mkdir(dir, 0o700); err != nil && !os.IsExist(err) {
			return err
		}
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("draft destination must be a real directory")
		}
	}
	done := draftMigration{Version: 1, Source: source, Destination: destination}
	for relative, b := range files {
		path := filepath.Join(destination, relative)
		info, err := os.Lstat(path)
		if err == nil {
			if !info.Mode().IsRegular() {
				return errors.New("draft destination is not a regular file")
			}
			existing, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if !bytes.Equal(existing, b) {
				return errors.New("draft destination conflicts; existing content retained")
			}
		} else if os.IsNotExist(err) {
			if err := atomicState(path, b); err != nil {
				return err
			}
		} else {
			return err
		}
		done.Files = append(done.Files, relative)
	}
	b, err := json.Marshal(done)
	if err != nil {
		return err
	}
	return atomicState(marker, b)
}

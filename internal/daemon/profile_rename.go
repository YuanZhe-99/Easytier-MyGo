package daemon

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
)

// renameProfile runs within the manager's existing lifetime state ownership.
// Commit the registry before publishing the new name to concurrent snapshots.
func (m *networkManager) renameProfile(ctx context.Context, id, name string) error {
	m.mutations.Lock()
	defer m.mutations.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.stateDir == "" {
		return errors.New("profile registry unavailable")
	}
	r, err := loadProfiles(m.stateDir)
	if err != nil {
		return err
	}
	for workerID, worker := range m.workers {
		found := false
		for _, p := range r.Profiles {
			if p.ID == workerID {
				if p.Deleted {
					return errors.New("active profile has deletion marker")
				}
				found = true
			}
		}
		if !found {
			m.namesMu.RLock()
			display := m.names[worker]
			m.namesMu.RUnlock()
			if display == "" {
				display = workerID
			}
			r.Profiles = append(r.Profiles, profileIdentity{ID: workerID, Name: display})
		}
	}
	if err := r.rename(id, name); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := saveProfiles(m.stateDir, r); err != nil {
		return err
	}
	m.namesMu.Lock()
	if m.names == nil {
		m.names = make(map[*Daemon]string)
	}
	m.names[m.workers[id]] = name
	m.namesMu.Unlock()
	return nil
}

// RenameProfile changes only the display-name index while the service is offline.
// The manifest continues to own configs/RPC; registry names take precedence when
// constructing workers. A single atomic index replacement avoids dual-file commit.
func RenameProfile(stateDir, manifestPath, id, name string) error {
	if runtime.GOOS != "linux" {
		return errors.New("offline profile rename is implemented only on Linux")
	}
	if !filepath.IsAbs(stateDir) {
		return errors.New("absolute state directory required")
	}
	manifest, err := readNetworkManifest(manifestPath)
	if err != nil {
		return err
	}
	var ids []string
	found := false
	for _, definition := range manifest.Networks {
		ids = append(ids, definition.ProfileID)
		found = found || definition.ProfileID == id
	}
	if !found {
		return errors.New("unknown profile target")
	}
	release, err := lockNetworkState(stateDir, ids)
	if err != nil {
		return err
	}
	defer release()
	r, err := loadProfiles(stateDir)
	if err != nil {
		return err
	}
	// Bootstrap registry names for manually authored manifests as well as
	// migrated ones; never invent a new stable ID for an existing worker.
	for _, definition := range manifest.Networks {
		registered := false
		for _, profile := range r.Profiles {
			registered = registered || profile.ID == definition.ProfileID
		}
		if !registered {
			display := definition.Name
			if display == "" {
				display = definition.ProfileID
			}
			r.Profiles = append(r.Profiles, profileIdentity{ID: definition.ProfileID, Name: display})
		}
	}
	if err := r.rename(id, name); err != nil {
		return err
	}
	return saveProfiles(stateDir, r)
}

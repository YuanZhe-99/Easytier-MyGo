package daemon

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/health"
)

// SetProfileDeleted atomically removes a profile from manifest startup without
// erasing user documents or evidence. Restore writes stopped intent before
// clearing the tombstone, so interruption cannot resurrect run authorization.
func SetProfileDeleted(stateDir, manifestPath, id string, deleted bool) error {
	return setProfileDeleted(stateDir, manifestPath, id, deleted, func(string) error { return nil })
}

func setProfileDeleted(stateDir, manifestPath, id string, deleted bool, checkpoint func(string) error) error {
	if runtime.GOOS != "linux" {
		return errors.New("offline profile deletion is implemented only on Linux")
	}
	if !filepath.IsAbs(stateDir) {
		return errors.New("absolute state directory required")
	}
	manifest, err := readNetworkManifest(manifestPath)
	if err != nil {
		return err
	}
	var ids []string
	var target networkDefinition
	for _, entry := range manifest.Networks {
		ids = append(ids, entry.ProfileID)
		if entry.ProfileID == id {
			target = entry
		}
	}
	if target.ProfileID == "" {
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
	index := -1
	for i, p := range r.Profiles {
		if p.ID == id {
			index = i
			break
		}
	}
	if index < 0 {
		if !deleted {
			return errors.New("profile is not deleted")
		}
		name := target.Name
		if name == "" {
			name = id
		}
		r.Profiles = append(r.Profiles, profileIdentity{ID: id, Name: name})
		index = len(r.Profiles) - 1
	}
	if r.Profiles[index].Deleted == deleted {
		return nil
	}
	if !deleted {
		path := filepath.Join(stateDir, "profiles", id, "run-intent.json")
		intent := runIntent{Version: 1, Target: target.Config}
		if b, err := os.ReadFile(path); err == nil {
			if err := json.Unmarshal(b, &intent); err != nil {
				return err
			}
			if intent.Version != 1 || intent.Target == "" || intent.Generation == ^uint64(0) {
				return errors.New("invalid retained run intent")
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		intent.Desired = health.DesiredStop
		intent.Generation++
		b, err := json.Marshal(intent)
		if err != nil {
			return err
		}
		if err := atomicState(path, b); err != nil {
			return err
		}
		if err := checkpoint("intent"); err != nil {
			return err
		}
	}
	r.Version = 2 // older builds must refuse rather than ignore tombstones
	r.Profiles[index].Deleted = deleted
	if err := saveProfiles(stateDir, r); err != nil {
		return err
	}
	return checkpoint("registry")
}

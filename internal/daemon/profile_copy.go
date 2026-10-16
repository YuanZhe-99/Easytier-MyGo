package daemon

import (
	"encoding/json"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/health"
)

type profileCopy struct {
	Version  int    `json:"version"`
	SourceID string `json:"source_id"`
	ID       string `json:"profile_id"`
	Name     string `json:"name"`
	RPC      string `json:"rpc"`
}

// CopyProfile copies the exact applied document to a fresh, stopped identity.
// Its independent manifest is published last; callers explicitly choose when to
// combine it with other networks after resolving raw-config resource conflicts.
func CopyProfile(stateDir, manifestPath, id, name, rpc string) (string, error) {
	return copyProfile(stateDir, manifestPath, id, name, rpc, func(string) error { return nil })
}

// checkpoint is invocation-scoped and used to exercise durable commit gaps.
func copyProfile(stateDir, manifestPath, id, name, rpc string, checkpoint func(string) error) (string, error) {
	if runtime.GOOS != "linux" {
		return "", errors.New("offline profile copy is implemented only on Linux")
	}
	if !filepath.IsAbs(stateDir) {
		return "", errors.New("absolute state directory required")
	}
	endpoint, err := netip.ParseAddrPort(rpc)
	if err != nil || !endpoint.Addr().IsLoopback() || endpoint.Port() == 0 {
		return "", errors.New("explicit loopback RPC required")
	}
	manifest, err := readNetworkManifest(manifestPath)
	if err != nil {
		return "", err
	}
	var source networkDefinition
	var ids []string
	for _, entry := range manifest.Networks {
		ids = append(ids, entry.ProfileID)
		if entry.ProfileID == id {
			source = entry
		}
		if entry.RPC == rpc {
			return "", errors.New("copy requires a different RPC")
		}
	}
	if source.ProfileID == "" {
		return "", errors.New("unknown profile target")
	}
	release, err := lockNetworkState(stateDir, ids)
	if err != nil {
		return "", err
	}
	defer release()
	r, err := loadProfiles(stateDir)
	if err != nil {
		return "", err
	}
	for _, p := range r.Profiles {
		if p.ID == id && p.Deleted {
			return "", errors.New("cannot copy a deleted source; restore it explicitly first")
		}
	}
	// Recover a reservation whose subsequent registry write failed. Only an
	// exact operation match can reattach that identity.
	entries, err := os.ReadDir(filepath.Join(stateDir, "profiles"))
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(stateDir, "profiles", entry.Name(), "copy.json"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		var saved profileCopy
		if err := json.Unmarshal(b, &saved); err != nil {
			return "", err
		}
		if saved.Name != name {
			continue
		}
		if saved.Version != 1 || saved.ID != entry.Name() || saved.SourceID != id || saved.RPC != rpc {
			return "", errors.New("copy reservation conflicts")
		}
		registered := false
		for _, p := range r.Profiles {
			if p.Name == name || p.ID == saved.ID {
				if p.Name != name || p.ID != saved.ID {
					return "", errors.New("copy registry conflict")
				}
				registered = true
			}
		}
		if !registered {
			r.Profiles = append(r.Profiles, profileIdentity{ID: saved.ID, Name: name})
		}
	}
	// A name selects a reserved copy operation, not a source-derived identity.
	// Reserving its ID before writing files makes failed retries deterministic.
	profile, err := r.register(name)
	if err != nil {
		return "", err
	}
	for _, existing := range manifest.Networks {
		if existing.ProfileID == profile.ID {
			return "", errors.New("copy name belongs to an existing manifest profile")
		}
	}
	worker := filepath.Join(stateDir, "profiles", profile.ID)
	unlock, err := lockState(worker)
	if err != nil {
		return "", err
	}
	defer unlock()
	reservation := filepath.Join(worker, "copy.json")
	want := profileCopy{Version: 1, SourceID: id, ID: profile.ID, Name: name, RPC: rpc}
	if b, err := os.ReadFile(reservation); err == nil {
		var saved profileCopy
		if err := json.Unmarshal(b, &saved); err != nil {
			return "", err
		}
		if saved != want {
			return "", errors.New("copy reservation conflicts with source/name/RPC")
		}
		if err := saveProfiles(stateDir, r); err != nil {
			return "", err
		}
	} else if !os.IsNotExist(err) {
		return "", err
	} else {
		// A registered profile without a copy reservation is never repurposed.
		previous, err := loadProfiles(stateDir)
		if err != nil {
			return "", err
		}
		for _, p := range previous.Profiles {
			if p.ID == profile.ID {
				return "", errors.New("copy name belongs to an existing profile")
			}
		}
		b, err := json.Marshal(want)
		if err != nil {
			return "", err
		}
		if err := atomicState(reservation, b); err != nil {
			return "", err
		}
		if err := checkpoint("reservation"); err != nil {
			return "", err
		}
		if err := saveProfiles(stateDir, r); err != nil {
			return "", err
		}
	}
	if err := checkpoint("registry"); err != nil {
		return "", err
	}
	output := filepath.Join(worker, "network.json")
	if _, err := os.Stat(output); err == nil {
		published, err := readNetworkManifest(output)
		if err != nil {
			return "", err
		}
		if len(published.Networks) != 1 || published.Networks[0].ProfileID != profile.ID || published.Networks[0].RPC != rpc {
			return "", errors.New("published copy manifest conflicts")
		}
		return output, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	// Never overwrite a partially initialized worker: verify copy ownership and
	// finish only before publication; no runtime is activated by this command.
	var applied appliedConfig
	b, err := os.ReadFile(filepath.Join(stateDir, "profiles", id, "applied-config.json"))
	if err != nil {
		return "", err
	}
	if err := json.Unmarshal(b, &applied); err != nil {
		return "", err
	}
	validator := &Daemon{opts: Options{StateDir: filepath.Join(stateDir, "profiles", id)}}
	if err := validator.validateApplied(applied); err != nil {
		return "", err
	}
	text, err := os.ReadFile(applied.Runtime)
	if err != nil {
		return "", err
	}
	config := filepath.Join(worker, "copied.toml")
	if err := atomicState(config, text); err != nil {
		return "", err
	}
	if err := checkpoint("document"); err != nil {
		return "", err
	}
	// Intent is written before publishing any usable manifest and no attempts,
	// events, collector caches, history or active probes are inherited.
	intent := runIntent{Version: 1, Desired: health.DesiredStop, Target: config, Generation: 1}
	b, err = json.Marshal(intent)
	if err != nil {
		return "", err
	}
	if err := atomicState(filepath.Join(worker, "run-intent.json"), b); err != nil {
		return "", err
	}
	if err := checkpoint("intent"); err != nil {
		return "", err
	}
	if err := saveBudget(worker, nil); err != nil {
		return "", err
	}
	if err := checkpoint("budget"); err != nil {
		return "", err
	}
	// Resolve saved (including pending) overrides into the copy's launch
	// manifest. The new worker never refers back to the source settings file.
	loggingOpts := Options{StateDir: filepath.Join(stateDir, "profiles", id)}
	if source.Logging != nil {
		loggingOpts.Logging = *source.Logging
	}
	if err := loadLoggingSettings(&loggingOpts); err != nil {
		return "", err
	}
	source.Logging = &loggingOpts.Logging
	copyManifest := networkManifest{Version: 1, Networks: []networkDefinition{{ProfileID: profile.ID, Name: name, Config: config, RPC: rpc, Logging: source.Logging}}}
	b, err = json.MarshalIndent(copyManifest, "", "  ")
	if err != nil {
		return "", err
	}
	if err := atomicState(output, b); err != nil {
		return "", err
	}
	if err := checkpoint("manifest"); err != nil {
		return "", err
	}
	return output, nil
}

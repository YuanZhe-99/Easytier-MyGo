package daemon

import (
	"encoding/json"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
)

type legacyMigration struct {
	Version   int    `json:"version"`
	ProfileID string `json:"profile_id"`
	Name      string `json:"name"`
	RPC       string `json:"rpc"`
}

// MigrateLegacyProfile copies an offline legacy state into one stable profile.
// It reserves identity before copying and publishes the runnable manifest last.
// No core is created. Originals remain as the rollback/backup source.
func MigrateLegacyProfile(stateDir, name, rpc string) (string, error) {
	if runtime.GOOS != "linux" {
		return "", errors.New("offline migration is implemented only on Linux")
	}
	if !filepath.IsAbs(stateDir) {
		return "", errors.New("absolute state directory required")
	}
	endpoint, err := netip.ParseAddrPort(rpc)
	if err != nil || !endpoint.Addr().IsLoopback() || endpoint.Port() == 0 {
		return "", errors.New("explicit loopback RPC required")
	}
	release, err := lockState(stateDir)
	if err != nil {
		return "", err
	}
	defer release()
	registry, err := loadProfiles(stateDir)
	if err != nil {
		return "", err
	}
	reservation := filepath.Join(stateDir, "legacy-migration.json")
	var migration legacyMigration
	b, err := os.ReadFile(reservation)
	if err == nil {
		if err := json.Unmarshal(b, &migration); err != nil {
			return "", err
		}
		if migration.Version != 1 || migration.Name != name || migration.RPC != rpc {
			return "", errors.New("migration already reserved with different name or RPC")
		}
	} else if !os.IsNotExist(err) {
		return "", err
	} else {
		if len(registry.Profiles) != 0 {
			return "", errors.New("legacy migration requires an empty profile registry")
		}
		profile, err := registry.register(name)
		if err != nil {
			return "", err
		}
		migration = legacyMigration{Version: 1, ProfileID: profile.ID, Name: name, RPC: rpc}
		b, err = json.Marshal(migration)
		if err != nil {
			return "", err
		}
		if err := atomicState(reservation, b); err != nil {
			return "", err
		}
	}
	profile := profileIdentity{ID: migration.ProfileID, Name: migration.Name}
	check := profileRegistry{Version: 1, Profiles: []profileIdentity{profile}}
	if err := check.validate(); err != nil {
		return "", err
	}
	for _, existing := range registry.Profiles {
		if existing != profile {
			return "", errors.New("profile registry conflicts with legacy migration")
		}
	}
	workerDir := filepath.Join(stateDir, "profiles", profile.ID)
	workerRelease, err := lockState(workerDir)
	if err != nil {
		return "", err
	}
	defer workerRelease()
	manifestPath := filepath.Join(stateDir, "migrated-networks.json")
	if _, err := os.Stat(manifestPath); err == nil {
		manifest, err := readNetworkManifest(manifestPath)
		if err != nil {
			return "", err
		}
		if len(manifest.Networks) != 1 || manifest.Networks[0].ProfileID != profile.ID || manifest.Networks[0].RPC != rpc || manifest.Networks[0].Name != name {
			return "", errors.New("published migration manifest conflicts")
		}
		return manifestPath, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if _, err := migrateProfileState(stateDir, profile); err != nil {
		return "", err
	}
	var applied appliedConfig
	b, err = os.ReadFile(filepath.Join(workerDir, "applied-config.json"))
	if err != nil {
		return "", err
	}
	if err := json.Unmarshal(b, &applied); err != nil {
		return "", err
	}
	if !filepath.IsAbs(applied.Source) {
		return "", errors.New("migration requires absolute configuration source")
	}
	if err := saveProfiles(stateDir, check); err != nil {
		return "", err
	}
	manifest := networkManifest{Version: 1, Networks: []networkDefinition{{ProfileID: profile.ID, Name: name, Config: applied.Source, RPC: rpc}}}
	b, err = json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", err
	}
	if err := atomicState(manifestPath, b); err != nil {
		return "", err
	}
	return manifestPath, nil
}

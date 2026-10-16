package daemon

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/health"
)

type profileMigration struct {
	Version   int      `json:"version"`
	ProfileID string   `json:"profile_id"`
	Files     []string `json:"files"`
}

// migrateProfileState copies offline state. Originals are retained; the marker
// is written last. Call only after the legacy daemon exits, never during checks.
func migrateProfileState(stateDir string, profile profileIdentity) (string, error) {
	id, err := hex.DecodeString(profile.ID)
	if err != nil || len(id) != 16 {
		return "", errors.New("invalid migration profile identity")
	}
	destination := filepath.Join(stateDir, "profiles", profile.ID)
	marker := filepath.Join(destination, "migration.json")
	if b, err := os.ReadFile(marker); err == nil {
		var migrated profileMigration
		if err := json.Unmarshal(b, &migrated); err != nil {
			return "", err
		}
		if migrated.Version != 1 || migrated.ProfileID != profile.ID {
			return "", errors.New("invalid migration marker")
		}
		return destination, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	files := make(map[string][]byte)
	for _, name := range []string{"active-config.json", "run-intent.json", budgetFile, "applied-config.json", "activation.json", "events.jsonl", "events.jsonl.1", "probes.json", "history-settings.json", "traffic-history.json", "logging.json"} {
		b, err := os.ReadFile(filepath.Join(stateDir, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		files[name] = b
	}
	if _, ok := files["applied-config.json"]; !ok {
		return "", errors.New("legacy applied revision required before migration")
	}
	loggingOpts := Options{StateDir: stateDir}
	if err := loadLoggingSettings(&loggingOpts); err != nil {
		return "", err
	}
	if b, ok := files[budgetFile]; ok {
		if _, err := decodeBudget(b); err != nil {
			return "", err
		}
	}
	// Reuse intent schema validation without process creation or mutation.
	validator := &Daemon{opts: Options{StateDir: stateDir}}
	if _, ok := files["run-intent.json"]; ok {
		validator.engine = health.New(health.DefaultPolicy())
		if err := validator.loadIntent(); err != nil {
			return "", err
		}
	}
	var applied appliedConfig
	if err := json.Unmarshal(files["applied-config.json"], &applied); err != nil {
		return "", err
	}
	remap := func(a appliedConfig) (appliedConfig, error) {
		if a.Runtime == "" {
			return a, errors.New("missing migration runtime revision")
		}
		if err := validator.validateApplied(a); err != nil {
			return a, err
		}
		b, err := os.ReadFile(a.Runtime)
		if err != nil {
			return a, err
		}
		files[filepath.Join("applied", a.Revision+".toml")] = b
		a.Runtime = filepath.Join(destination, "applied", a.Revision+".toml")
		return a, nil
	}
	applied, err = remap(applied)
	if err != nil {
		return "", err
	}
	files["applied-config.json"], err = json.Marshal(applied)
	if err != nil {
		return "", err
	}
	if b, ok := files["activation.json"]; ok {
		var record activationRecord
		if err := json.Unmarshal(b, &record); err != nil {
			return "", err
		}
		switch record.Phase {
		case "starting", "rollback", "committed", "unconfirmed", "rollback-failed":
		default:
			return "", errors.New("invalid migration activation phase")
		}
		if record.Previous.Runtime != "" {
			record.Previous, err = remap(record.Previous)
			if err != nil {
				return "", err
			}
		}
		record.Candidate, err = remap(record.Candidate)
		if err != nil {
			return "", err
		}
		files["activation.json"], err = json.Marshal(record)
		if err != nil {
			return "", err
		}
	}
	if err := os.MkdirAll(filepath.Join(destination, "applied"), 0o700); err != nil {
		return "", err
	}
	manifest := profileMigration{Version: 1, ProfileID: profile.ID}
	for name, data := range files {
		if err := atomicState(filepath.Join(destination, name), data); err != nil {
			return "", err
		}
		manifest.Files = append(manifest.Files, name)
	}
	sort.Strings(manifest.Files)
	b, err := json.Marshal(manifest)
	if err != nil {
		return "", err
	}
	if err := atomicState(marker, b); err != nil {
		return "", err
	}
	return destination, nil
}

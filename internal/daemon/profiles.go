package daemon

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/configdoc"
)

// profileRegistry is an identity index, never a serialization of TOML.
// Run state remains independently scoped by the stable ID.
type profileRegistry struct {
	Version  int               `json:"version"`
	Profiles []profileIdentity `json:"profiles"`
}

type profileIdentity struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Deleted bool   `json:"deleted,omitempty"`
}

func newProfileID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}

func (r *profileRegistry) validate() error {
	if r.Version != 1 && r.Version != 2 {
		return errors.New("unsupported profile registry version")
	}
	ids, names := map[string]bool{}, map[string]bool{}
	for _, profile := range r.Profiles {
		if profile.Deleted && r.Version < 2 {
			return errors.New("deleted profiles require registry version 2")
		}
		decoded, err := hex.DecodeString(profile.ID)
		if err != nil || len(decoded) != 16 || ids[profile.ID] || names[profile.Name] {
			return errors.New("invalid or duplicate profile identity")
		}
		if err := configdoc.ValidName(profile.Name); err != nil {
			return err
		}
		ids[profile.ID] = true
		names[profile.Name] = true
	}
	return nil
}

func loadProfiles(stateDir string) (profileRegistry, error) {
	b, err := os.ReadFile(filepath.Join(stateDir, "profiles.json"))
	if os.IsNotExist(err) {
		return profileRegistry{Version: 1}, nil
	}
	if err != nil {
		return profileRegistry{}, err
	}
	var r profileRegistry
	if err := json.Unmarshal(b, &r); err != nil {
		return r, err
	}
	return r, r.validate()
}

func saveProfiles(stateDir string, r profileRegistry) error {
	if err := r.validate(); err != nil {
		return err
	}
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return atomicState(filepath.Join(stateDir, "profiles.json"), b)
}

// register is idempotent for an existing name; copy supplies a new name.
func (r *profileRegistry) register(name string) (profileIdentity, error) {
	if err := configdoc.ValidName(name); err != nil {
		return profileIdentity{}, err
	}
	for _, profile := range r.Profiles {
		if profile.Name == name {
			if profile.Deleted {
				return profileIdentity{}, errors.New("profile name is reserved by a deleted profile")
			}
			return profile, nil
		}
	}
	id, err := newProfileID()
	if err != nil {
		return profileIdentity{}, err
	}
	profile := profileIdentity{ID: id, Name: name}
	r.Profiles = append(r.Profiles, profile)
	return profile, nil
}

func (r *profileRegistry) rename(id, name string) error {
	if err := configdoc.ValidName(name); err != nil {
		return err
	}
	for _, profile := range r.Profiles {
		if profile.Name == name && profile.ID != id {
			return configdoc.ErrExists
		}
	}
	for i := range r.Profiles {
		if r.Profiles[i].ID == id {
			r.Profiles[i].Name = name
			return nil
		}
	}
	return os.ErrNotExist
}

func (r *profileRegistry) remove(id string) error {
	for i, profile := range r.Profiles {
		if profile.ID == id {
			r.Profiles = append(r.Profiles[:i], r.Profiles[i+1:]...)
			return nil
		}
	}
	return os.ErrNotExist
}

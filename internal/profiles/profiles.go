// Package profiles stores the daemon's networks: each network is a raw
// EasyTier TOML document (internal/configdoc) plus metadata — a stable ID, a
// display name, the user's run/stop intent, and the revisions that were
// applied. Applied revisions are frozen copies, so editing a document never
// changes what a running core was started with.
//
// Layout under the store's root:
//
//	profiles.json             metadata of every network
//	networks/<id>.toml        the editable documents (+ networks/backups)
//	applied/<id>/<rev>.toml   frozen copies of the applied and previous revisions
package profiles

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/configdoc"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/fsutil"
)

// ErrNotFound reports an unknown network ID.
var ErrNotFound = errors.New("network not found")

// Profile is one network's metadata.
type Profile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Run is the user's intent: connected (true) or disconnected.
	Run bool `json:"run"`
	// Applied is the revision the core runs (or last ran) with; Previous is
	// the one before it, kept for rollback. Empty until first applied.
	Applied  string    `json:"applied,omitempty"`
	Previous string    `json:"previous,omitempty"`
	Created  time.Time `json:"created"`
	// Mini runs the network on EasyTier mini instead of the selected core.
	// It is set when the network is created as a copy for mini and never
	// changes, so a document is only ever stripped in its own copy.
	Mini bool `json:"mini,omitempty"`
}

type metaFile struct {
	Profiles []Profile `json:"profiles"`
}

// Store is the network store rooted at one directory. It is safe for
// concurrent use within one process; the daemon is its only writer.
type Store struct {
	root string
	docs configdoc.Store

	mu    sync.Mutex
	metas []Profile
}

// Open loads (or initializes) the store at root.
// Side effects: creates the directory layout (0700).
func Open(root string) (*Store, error) {
	s := &Store{root: root, docs: configdoc.Store{Dir: filepath.Join(root, "networks")}}
	if err := s.docs.Ensure(); err != nil {
		return nil, err
	}
	var mf metaFile
	if _, err := fsutil.ReadJSON(s.metaPath(), &mf); err != nil {
		return nil, fmt.Errorf("profiles.json: %w", err)
	}
	s.metas = mf.Profiles
	return s, nil
}

func (s *Store) metaPath() string { return filepath.Join(s.root, "profiles.json") }

// saveLocked persists the metadata; callers hold s.mu.
func (s *Store) saveLocked() error {
	return fsutil.WriteJSON(s.metaPath(), metaFile{Profiles: s.metas})
}

func (s *Store) indexLocked(id string) int {
	return slices.IndexFunc(s.metas, func(p Profile) bool { return p.ID == id })
}

// List returns every network's metadata in creation order.
func (s *Store) List() []Profile {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.metas)
}

// Get returns one network's metadata.
func (s *Store) Get(id string) (Profile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.indexLocked(id)
	if i < 0 {
		return Profile{}, ErrNotFound
	}
	return s.metas[i], nil
}

// Create adds a network with a display name and its document text.
// Returns: the new metadata (stopped, never applied).
func (s *Store) Create(name, text string) (Profile, error) {
	return s.CreateFor(name, text, false)
}

// CreateFor is Create for the full core or, with mini, for EasyTier mini.
func (s *Store) CreateFor(name, text string, mini bool) (Profile, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Profile{}, errors.New("network name is empty")
	}
	id, err := newID()
	if err != nil {
		return Profile{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.docs.Create(id, text); err != nil {
		return Profile{}, err
	}
	p := Profile{ID: id, Name: name, Created: time.Now().UTC(), Mini: mini}
	s.metas = append(s.metas, p)
	if err := s.saveLocked(); err != nil {
		s.metas = s.metas[:len(s.metas)-1]
		_ = s.docs.Delete(id)
		return Profile{}, err
	}
	return p, nil
}

// update applies fn to one network's metadata and persists the result.
func (s *Store) update(id string, fn func(*Profile)) (Profile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.indexLocked(id)
	if i < 0 {
		return Profile{}, ErrNotFound
	}
	old := s.metas[i]
	fn(&s.metas[i])
	if err := s.saveLocked(); err != nil {
		s.metas[i] = old
		return Profile{}, err
	}
	return s.metas[i], nil
}

// Rename changes the display name.
func (s *Store) Rename(id, name string) (Profile, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Profile{}, errors.New("network name is empty")
	}
	return s.update(id, func(p *Profile) { p.Name = name })
}

// SetRun records the user's connect/disconnect intent.
func (s *Store) SetRun(id string, run bool) (Profile, error) {
	return s.update(id, func(p *Profile) { p.Run = run })
}

// Delete removes a network, its document, backups excepted, and its frozen
// revisions.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.indexLocked(id)
	if i < 0 {
		return ErrNotFound
	}
	s.metas = slices.Delete(s.metas, i, i+1)
	if err := s.saveLocked(); err != nil {
		return err
	}
	_ = s.docs.Delete(id)
	return os.RemoveAll(filepath.Join(s.root, "applied", id))
}

// Config returns the editable document and its entry (revision).
func (s *Store) Config(id string) (string, configdoc.Entry, error) {
	if _, err := s.Get(id); err != nil {
		return "", configdoc.Entry{}, err
	}
	return s.docs.Read(id)
}

// SaveConfig overwrites the document if it is still at expected
// ("" skips the check); see configdoc.Store.Save.
func (s *Store) SaveConfig(id, text, expected string) (configdoc.Entry, error) {
	if _, err := s.Get(id); err != nil {
		return configdoc.Entry{}, err
	}
	return s.docs.Save(id, text, expected)
}

// Freeze copies the current document into applied/<id>/<rev>.toml and
// returns that path and revision. It does not change the metadata; call
// MarkApplied once the core runs with it.
func (s *Store) Freeze(id string) (path, revision string, err error) {
	text, entry, err := s.Config(id)
	if err != nil {
		return "", "", err
	}
	path = s.frozenPath(id, entry.Revision)
	if err := fsutil.WriteFileAtomic(path, []byte(text), 0o600); err != nil {
		return "", "", err
	}
	return path, entry.Revision, nil
}

// FrozenPath returns the frozen copy of a revision, if it exists.
func (s *Store) FrozenPath(id, revision string) (string, bool) {
	p := s.frozenPath(id, revision)
	_, err := os.Stat(p)
	return p, err == nil
}

func (s *Store) frozenPath(id, revision string) string {
	return filepath.Join(s.root, "applied", id, revision+".toml")
}

// MarkApplied records revision as applied, keeps the former one as the
// rollback target, and deletes older frozen copies.
func (s *Store) MarkApplied(id, revision string) (Profile, error) {
	p, err := s.update(id, func(p *Profile) {
		if p.Applied != revision {
			p.Previous, p.Applied = p.Applied, revision
		}
	})
	if err != nil {
		return p, err
	}
	s.pruneFrozen(p)
	return p, nil
}

// pruneFrozen keeps only the applied and previous frozen copies.
func (s *Store) pruneFrozen(p Profile) {
	dir := filepath.Join(s.root, "applied", p.ID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		rev := strings.TrimSuffix(e.Name(), ".toml")
		if rev != p.Applied && rev != p.Previous {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// newID returns a random 16-hex-digit network ID.
func newID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

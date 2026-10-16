// Package configdoc treats EasyTier network configuration files as raw
// TOML documents: validation with parser error positions, content
// revisions for conflict detection, and a directory store with atomic
// writes and backups.
//
// The raw text is the source of truth. Nothing in this package parses a
// model and re-serializes it over the user's file, so comments, unknown
// fields and formatting survive whatever the application does.
package configdoc

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	toml "github.com/pelletier/go-toml/v2"
)

// Ext is the file extension of configuration files.
const Ext = ".toml"

// BackupsDir holds previous versions, inside the config directory.
const BackupsDir = "backups"

// maxBackupsPerName is how many historical versions of one config are
// kept; pruning happens on every save/delete.
const maxBackupsPerName = 10

var (
	// ErrConflict means the file on disk no longer matches the revision
	// the caller based its edits on.
	ErrConflict = errors.New("config changed on disk since it was loaded")
	// ErrExists means a create target is already present.
	ErrExists = errors.New("config already exists")
	// ErrInvalidName means the name cannot map to a file in the store.
	ErrInvalidName = errors.New("invalid config name")
)

// Summary is a light projection of a parsed config for lists and headers.
// It never round-trips: unknown fields are simply not in it.
type Summary struct {
	InstanceName string `json:"instance_name,omitempty"`
	Hostname     string `json:"hostname,omitempty"`
	IPv4         string `json:"ipv4,omitempty"`
	NetworkName  string `json:"network_name,omitempty"`
	Listeners    int    `json:"listeners"`
	Peers        int    `json:"peers"`
	NoTun        bool   `json:"no_tun,omitempty"`
}

// rawConfig mirrors the fields the summary cares about. Everything else in
// the document is deliberately ignored (and preserved, because it is never
// rewritten from this struct).
type rawConfig struct {
	InstanceName string   `toml:"instance_name"`
	Hostname     string   `toml:"hostname"`
	IPv4         string   `toml:"ipv4"`
	Listeners    []string `toml:"listeners"`
	NetworkID    struct {
		NetworkName string `toml:"network_name"`
	} `toml:"network_identity"`
	Peers []struct {
		URI string `toml:"uri"`
	} `toml:"peer"`
	Flags struct {
		NoTun *bool `toml:"no_tun"`
	} `toml:"flags"`
}

// Parse validates the TOML text and extracts the summary. The error for
// invalid input is a *ValidateError carrying the parser's position
// information.
func Parse(text string) (Summary, error) {
	var raw rawConfig
	if err := toml.Unmarshal([]byte(text), &raw); err != nil {
		return Summary{}, &ValidateError{Err: err}
	}
	s := Summary{
		InstanceName: raw.InstanceName,
		Hostname:     raw.Hostname,
		IPv4:         raw.IPv4,
		NetworkName:  raw.NetworkID.NetworkName,
		Listeners:    len(raw.Listeners),
		Peers:        len(raw.Peers),
	}
	if raw.Flags.NoTun != nil {
		s.NoTun = *raw.Flags.NoTun
	}
	return s, nil
}

// ValidateError wraps a TOML parse failure.
type ValidateError struct{ Err error }

func (e *ValidateError) Error() string {
	var de *toml.DecodeError
	if errors.As(e.Err, &de) {
		row, col := de.Position()
		msg := strings.SplitN(de.Error(), "\n", 2)[0]
		return fmt.Sprintf("invalid TOML at line %d, column %d: %s", row, col, msg)
	}
	return "invalid TOML: " + e.Err.Error()
}
func (e *ValidateError) Unwrap() error { return e.Err }

// IsInvalid reports whether err is a validation failure (as opposed to an
// I/O or conflict error).
func IsInvalid(err error) bool {
	var ve *ValidateError
	return errors.As(err, &ve)
}

// Revision is the short content hash used for conflict detection. It is
// stable across platforms and cheap for the small files involved.
func Revision(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])[:12]
}

// ValidName checks a config name: it must be usable as one file name in
// the store, without path separators or surprises.
func ValidName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: empty", ErrInvalidName)
	}
	if name == "." || name == ".." {
		return fmt.Errorf("%w: %q", ErrInvalidName, name)
	}
	if utf8.RuneCountInString(name) > 64 {
		return fmt.Errorf("%w: longer than 64 characters", ErrInvalidName)
	}
	if strings.ContainsAny(name, "/\\") || strings.ContainsRune(name, 0) {
		return fmt.Errorf("%w: %q", ErrInvalidName, name)
	}
	if strings.TrimSpace(name) != name {
		return fmt.Errorf("%w: leading or trailing whitespace", ErrInvalidName)
	}
	if strings.HasPrefix(name, ".") {
		return fmt.Errorf("%w: leading dot", ErrInvalidName)
	}
	return nil
}

// Entry describes one file in the store.
type Entry struct {
	Name     string    `json:"name"`
	Path     string    `json:"path"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
	Revision string    `json:"revision"`
}

// Store is one directory of configuration files. Operations are safe for
// concurrent use to the extent the filesystem is; the daemon serializes
// its own calls.
type Store struct {
	Dir string
}

// path maps a name to its file, validating the name.
func (s Store) path(name string) (string, error) {
	if err := ValidName(name); err != nil {
		return "", err
	}
	return filepath.Join(s.Dir, name+Ext), nil
}

// Ensure creates the directory (and the backups directory) if needed.
func (s Store) Ensure() error {
	if err := os.MkdirAll(filepath.Join(s.Dir, BackupsDir), 0o700); err != nil {
		return err
	}
	return os.Chmod(s.Dir, 0o700)
}

// List returns the entries, sorted by name. Revision requires reading each
// file; configs are small and few, so this stays cheap.
func (s Store) List() ([]Entry, error) {
	dirEntries, err := os.ReadDir(s.Dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Entry
	for _, de := range dirEntries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), Ext) {
			continue
		}
		name := strings.TrimSuffix(de.Name(), Ext)
		if ValidName(name) != nil {
			continue
		}
		e, err := s.entryFor(name)
		if err != nil {
			continue
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s Store) entryFor(name string) (Entry, error) {
	path, err := s.path(name)
	if err != nil {
		return Entry{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return Entry{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Entry{}, err
	}
	return Entry{
		Name:     name,
		Path:     path,
		Size:     info.Size(),
		Modified: info.ModTime(),
		Revision: Revision(string(data)),
	}, nil
}

// Read returns the raw text and entry of one config.
func (s Store) Read(name string) (string, Entry, error) {
	path, err := s.path(name)
	if err != nil {
		return "", Entry{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", Entry{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", Entry{}, err
	}
	e := Entry{Name: name, Path: path, Size: info.Size(), Modified: info.ModTime(), Revision: Revision(string(data))}
	return string(data), e, nil
}

// Create writes a new config; it fails instead of overwriting.
func (s Store) Create(name, text string) (Entry, error) {
	path, err := s.path(name)
	if err != nil {
		return Entry{}, err
	}
	if _, err := os.Stat(path); err == nil {
		return Entry{}, fmt.Errorf("%w: %s", ErrExists, name)
	}
	if err := s.writeAtomic(path, text); err != nil {
		return Entry{}, err
	}
	return s.entryFor(name)
}

// Save overwrites a config. expected is the revision the caller edited
// from ("" skips the check, for scripted overwrites); a mismatch fails
// with ErrConflict and leaves the file untouched. The previous content is
// backed up first.
func (s Store) Save(name, text, expected string) (Entry, error) {
	path, err := s.path(name)
	if err != nil {
		return Entry{}, err
	}
	if cur, err := os.ReadFile(path); err == nil {
		if expected != "" && Revision(string(cur)) != expected {
			return Entry{}, fmt.Errorf("%w: %s", ErrConflict, name)
		}
		if err := s.backup(name, string(cur), ""); err != nil {
			return Entry{}, err
		}
	}
	if err := s.writeAtomic(path, text); err != nil {
		return Entry{}, err
	}
	return s.entryFor(name)
}

// Delete removes a config after backing it up.
func (s Store) Delete(name string) error {
	path, err := s.path(name)
	if err != nil {
		return err
	}
	cur, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := s.backup(name, string(cur), "deleted-"); err != nil {
		return err
	}
	return os.Remove(path)
}

// writeAtomic writes through a temporary file in the same directory and
// renames, so a crash never leaves a half-written config. Configs hold
// network secrets: mode 0600.
func (s Store) writeAtomic(path, text string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(text); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// backup copies content into the backups directory. The prefix marks
// deletions; saves leave it empty.
func (s Store) backup(name, content, prefix string) error {
	dir := filepath.Join(s.Dir, BackupsDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	stamp := time.Now().UTC().Format("20060102-150405.000")
	file := filepath.Join(dir, name+"."+prefix+stamp+Ext)
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		return err
	}
	return s.pruneBackups(name)
}

// pruneBackups keeps at most maxBackupsPerName backups for one name.
func (s Store) pruneBackups(name string) error {
	dir := filepath.Join(s.Dir, BackupsDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var mine []string
	for _, de := range entries {
		if !de.IsDir() && strings.HasPrefix(de.Name(), name+".") {
			mine = append(mine, de.Name())
		}
	}
	if len(mine) <= maxBackupsPerName {
		return nil
	}
	sort.Strings(mine) // timestamped names sort chronologically
	for _, old := range mine[:len(mine)-maxBackupsPerName] {
		_ = os.Remove(filepath.Join(dir, old))
	}
	return nil
}

// Template is a suggested starting point for a new config; the user edits
// it as raw text like any other. The listeners line is left commented:
// uncomment it to pin ports, which is required when another EasyTier
// instance already uses the defaults (11010 tcp/udp).
func Template() string {
	return `instance_name = ""
hostname = ""
dhcp = true

# listeners = [ "tcp://0.0.0.0:11010", "udp://0.0.0.0:11010" ]

[network_identity]
network_name = ""
network_secret = ""

[flags]
no_tun = false
`
}

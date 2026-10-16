// Package bundlesig signs and verifies the app bundle's executables, so
// the privileged helper can update itself from a user-writable app
// directory without an administrator prompt: it trusts only files listed
// in a manifest signed with the release key whose public half is embedded
// here (release.pub).
//
//	bundle.json  {"version": "0.1.0", "files": {"easytier-mygo-svc": "<sha256>", …}}
//	bundle.sig   base64 ed25519 signature of bundle.json
package bundlesig

import (
	"crypto/ed25519"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed release.pub
var releasePub string

// Files a bundle manifest covers (when present in the directory).
var Files = []string{"easytier-mygo-svc", "easytier-core", "easytier-cli", "upx",
	"easytier-mygo-svc.exe", "easytier-core.exe", "easytier-cli.exe", "upx.exe"}

// ErrNoKey reports a build without a release public key (development).
var ErrNoKey = errors.New("this build has no release key; bundles cannot be verified")

// Manifest lists the bundle's files and their SHA-256.
type Manifest struct {
	Version string            `json:"version"`
	Files   map[string]string `json:"files"`
}

// PublicKey returns the embedded release key.
func PublicKey() (ed25519.PublicKey, error) {
	s := strings.TrimSpace(releasePub)
	if s == "" {
		return nil, ErrNoKey
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("release.pub is malformed")
	}
	return ed25519.PublicKey(b), nil
}

func fileHash(p string) (string, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// Sign writes bundle.json and bundle.sig for the executables in dir.
func Sign(dir, version string, key ed25519.PrivateKey) error {
	m := Manifest{Version: version, Files: map[string]string{}}
	for _, f := range Files {
		p := filepath.Join(dir, f)
		if _, err := os.Stat(p); err != nil {
			continue
		}
		h, err := fileHash(p)
		if err != nil {
			return err
		}
		m.Files[f] = h
	}
	if len(m.Files) == 0 {
		return fmt.Errorf("no executables in %s", dir)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "bundle.json"), data, 0o644); err != nil {
		return err
	}
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(key, data))
	return os.WriteFile(filepath.Join(dir, "bundle.sig"), []byte(sig+"\n"), 0o644)
}

// Verify checks dir's manifest signature with key and every listed file's
// hash. Returns the manifest when everything matches.
func Verify(dir string, key ed25519.PublicKey) (Manifest, error) {
	var m Manifest
	data, err := os.ReadFile(filepath.Join(dir, "bundle.json"))
	if err != nil {
		return m, fmt.Errorf("unsigned bundle: %w", err)
	}
	sigText, err := os.ReadFile(filepath.Join(dir, "bundle.sig"))
	if err != nil {
		return m, fmt.Errorf("unsigned bundle: %w", err)
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigText)))
	if err != nil || !ed25519.Verify(key, data, sig) {
		return m, errors.New("bundle signature does not verify")
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return m, err
	}
	names := make([]string, 0, len(m.Files))
	for n := range m.Files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if strings.ContainsAny(n, `/\`) {
			return m, fmt.Errorf("bad file name %q in manifest", n)
		}
		h, err := fileHash(filepath.Join(dir, n))
		if err != nil {
			return m, err
		}
		if h != m.Files[n] {
			return m, fmt.Errorf("%s does not match the signed manifest", n)
		}
	}
	return m, nil
}

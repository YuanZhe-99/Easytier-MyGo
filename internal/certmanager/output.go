package certmanager

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Default output file names and modes.
const (
	DefaultCertFile      = "cert.pem"
	DefaultFullchainFile = "fullchain.pem"
	DefaultKeyFile       = "key.pem"
	DefaultCertMode      = 0o644
	DefaultKeyMode       = 0o600
)

// WriteOutputs replaces a job's files with a new certificate.
// Inputs: f names the files (an empty Cert or Fullchain is skipped);
// certMode applies to the certificate files, keyMode to the key.
// Side effects: creates missing parent directories (0755, so a web server
// can reach the files); every file is first written and synced as a
// temporary file in its own directory, and only when all of them are
// complete are they renamed over the targets, key last. A failure before
// the renames leaves the old files untouched.
// Notes: a reader can still see a new certificate next to the old key for
// the instant between two renames; the hook (reload) runs after all of them.
func WriteOutputs(f Files, leaf *Leaf, ca *CA, certMode, keyMode os.FileMode) error {
	type item struct {
		path string
		data []byte
		mode os.FileMode
	}
	var items []item
	if f.Cert != "" {
		items = append(items, item{f.Cert, leaf.CertPEM, certMode})
	}
	if f.Fullchain != "" {
		items = append(items, item{f.Fullchain, leaf.FullchainPEM(ca), certMode})
	}
	if f.Key == "" {
		return errors.New("no key file configured")
	}
	items = append(items, item{f.Key, leaf.KeyPEM, keyMode})

	var temps []string
	cleanup := func() {
		for _, t := range temps {
			_ = os.Remove(t)
		}
	}
	for _, it := range items {
		tmp, err := writeTemp(it.path, it.data, it.mode)
		if err != nil {
			cleanup()
			return err
		}
		temps = append(temps, tmp)
	}
	for i, it := range items {
		if err := os.Rename(temps[i], it.path); err != nil {
			cleanup()
			return fmt.Errorf("replace %s: %w", it.path, err)
		}
		temps[i] = ""
	}
	return nil
}

// writeTemp writes data to a synced temporary file next to path, with
// mode set before any byte is written.
func writeTemp(path string, data []byte, mode os.FileMode) (string, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return "", err
	}
	name := tmp.Name()
	fail := func(err error) (string, error) {
		tmp.Close()
		os.Remove(name)
		return "", err
	}
	if err := tmp.Chmod(mode); err != nil {
		return fail(err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fail(err)
	}
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return "", err
	}
	return name, nil
}

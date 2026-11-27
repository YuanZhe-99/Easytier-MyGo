package certmanager

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// SkipFile as a Job's CertFile or FullchainFile leaves that file out.
const SkipFile = "-"

// Job is one certificate the manager keeps current: which names it
// carries, where its files go and what runs after it changes. Jobs are
// stored in <dir>/jobs.json and cross the local API as this JSON.
type Job struct {
	// ID names the job (letters, digits, '-', '_'; generated when empty).
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
	// Network is the ID of an EasyTier network whose names the
	// certificate carries: its MagicDNS name, hostname and virtual IP,
	// as its running core reports them.
	Network string `json:"network,omitempty"`
	// DNSNames and IPs are added to the network's names; DNS names may be
	// wildcards ("*.et.net").
	DNSNames []string `json:"dns_names,omitempty"`
	IPs      []string `json:"ips,omitempty"`
	// OutDir is an absolute directory ("": <dir>/out/<id>). Created when
	// missing.
	OutDir string `json:"out_dir,omitempty"`
	// File names inside OutDir ("": cert.pem, fullchain.pem, key.pem;
	// SkipFile leaves the certificate or the full chain out).
	CertFile      string `json:"cert_file,omitempty"`
	FullchainFile string `json:"fullchain_file,omitempty"`
	KeyFile       string `json:"key_file,omitempty"`
	// CertMode and KeyMode are octal permissions ("": 0644 and 0600).
	// They apply on Unix; Windows keeps the directory's ACL.
	CertMode string `json:"cert_mode,omitempty"`
	KeyMode  string `json:"key_mode,omitempty"`
	// ValidityDays is the certificate lifetime (0: 90; at most 397).
	ValidityDays int `json:"validity_days,omitempty"`
	// RenewBeforeDays renews when less remains (0: 30); a third of the
	// lifetime counts too, whichever is longer.
	RenewBeforeDays int     `json:"renew_before_days,omitempty"`
	KeyType         KeyType `json:"key_type,omitempty"`
	// Hook runs after a new certificate was written.
	Hook Hook `json:"hook,omitzero"`
}

var jobIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,40}$`)

// NewJobID returns a fresh job ID. Clients fill it in before asking for an
// approval, which is bound to the ID.
func NewJobID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// validate checks a job's shape and fills its ID. The privileged helper's
// approvals are checked separately (Manager.approved).
func (j *Job) validate() error {
	if j.ID == "" {
		j.ID = NewJobID()
	}
	if !jobIDPattern.MatchString(j.ID) {
		return fmt.Errorf("job ID %q: use 1 to 40 letters, digits, '-' or '_'", j.ID)
	}
	sans, err := NormalizeSANs(j.DNSNames, j.IPs)
	if err != nil {
		return err
	}
	if sans.Empty() && j.Network == "" {
		return errors.New("the certificate needs a network or at least one DNS name or IP address")
	}
	if j.OutDir != "" {
		if !filepath.IsAbs(j.OutDir) {
			return fmt.Errorf("the output directory %q must be an absolute path", j.OutDir)
		}
		j.OutDir = filepath.Clean(j.OutDir)
	}
	names := map[string]string{"certificate": j.CertFile, "full chain": j.FullchainFile, "key": j.KeyFile}
	seen := map[string]bool{}
	for what, n := range names {
		if n == "" || n == SkipFile && what != "key" {
			continue
		}
		if n == SkipFile || n != filepath.Base(n) || n == "." || n == ".." || strings.ContainsAny(n, `/\`) {
			return fmt.Errorf("the %s file name %q must be a plain file name", what, n)
		}
	}
	for _, n := range []string{j.certName(), j.fullchainName(), j.keyName()} {
		if n == "" {
			continue
		}
		if seen[n] {
			return fmt.Errorf("two output files are both named %q", n)
		}
		seen[n] = true
	}
	if j.CertFile == SkipFile && j.FullchainFile == SkipFile {
		return errors.New("write at least the certificate or the full chain")
	}
	if _, err := parseMode(j.CertMode, DefaultCertMode); err != nil {
		return err
	}
	if _, err := parseMode(j.KeyMode, DefaultKeyMode); err != nil {
		return err
	}
	if j.ValidityDays < 0 || j.ValidityDays > MaxLeafDays {
		return fmt.Errorf("certificate validity must be 1 to %d days", MaxLeafDays)
	}
	if j.RenewBeforeDays < 0 || j.RenewBeforeDays >= j.days() {
		return fmt.Errorf("renew-before must be shorter than the validity (%d days)", j.days())
	}
	if _, err := newKeyCheck(j.KeyType); err != nil {
		return err
	}
	return j.Hook.Validate()
}

func (j Job) days() int {
	if j.ValidityDays == 0 {
		return DefaultLeafDays
	}
	return j.ValidityDays
}

func (j Job) renewBefore() time.Duration {
	d := j.RenewBeforeDays
	if d == 0 {
		d = min(DefaultRenewBeforeDays, j.days()/3)
	}
	return time.Duration(d) * 24 * time.Hour
}

func (j Job) certName() string      { return fileName(j.CertFile, DefaultCertFile) }
func (j Job) fullchainName() string { return fileName(j.FullchainFile, DefaultFullchainFile) }
func (j Job) keyName() string       { return fileName(j.KeyFile, DefaultKeyFile) }

func fileName(v, def string) string {
	switch v {
	case "":
		return def
	case SkipFile:
		return ""
	}
	return v
}

// files resolves the job's output paths under its directory.
func (j Job) files(defaultDir string) (Files, string) {
	dir := j.OutDir
	if dir == "" {
		dir = filepath.Join(defaultDir, j.ID)
	}
	join := func(n string) string {
		if n == "" {
			return ""
		}
		return filepath.Join(dir, n)
	}
	return Files{Cert: join(j.certName()), Fullchain: join(j.fullchainName()), Key: join(j.keyName())}, dir
}

func (j Job) modes() (cert, key os.FileMode) {
	cert, _ = parseMode(j.CertMode, DefaultCertMode)
	key, _ = parseMode(j.KeyMode, DefaultKeyMode)
	return cert, key
}

// parseMode reads an octal permission ("0640").
func parseMode(s string, def os.FileMode) (os.FileMode, error) {
	if s == "" {
		return def, nil
	}
	v, err := strconv.ParseUint(s, 8, 32)
	if err != nil || v > 0o777 {
		return 0, fmt.Errorf("%q is not a file permission (use octal, like 0640)", s)
	}
	return os.FileMode(v), nil
}

// newKeyCheck validates a key type without generating a key.
func newKeyCheck(t KeyType) (KeyType, error) {
	switch t {
	case "", KeyECDSAP256, KeyRSA2048, KeyRSA4096:
		return t, nil
	}
	return t, fmt.Errorf("unknown key type %q (use %s, %s or %s)", t, KeyECDSAP256, KeyRSA2048, KeyRSA4096)
}

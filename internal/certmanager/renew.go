package certmanager

import (
	"crypto/x509"
	"errors"
	"os"
	"time"
)

// Reason says why a certificate is (or is not) issued. Reasons cross the
// local API as these names.
type Reason string

const (
	ReasonNone       Reason = "none"        // the files on disk are current
	ReasonMissing    Reason = "missing"     // a certificate or key file is absent
	ReasonCorrupt    Reason = "corrupt"     // a file does not parse, or the key does not match
	ReasonExpiring   Reason = "expiring"    // inside the renewal window (or expired)
	ReasonSANChanged Reason = "san-changed" // the wanted names differ from the certificate's
	ReasonCAChanged  Reason = "ca-changed"  // the certificate does not verify against the current CA
	ReasonForced     Reason = "forced"      // the user asked for a new certificate
)

// DefaultRenewBeforeDays is the renewal window: a certificate is renewed
// when less than this, or less than a third of its lifetime, remains.
const DefaultRenewBeforeDays = 30

// Files are the paths of one job's output.
type Files struct {
	Cert      string // the leaf alone ("" when not written)
	Fullchain string // leaf + CA ("" when not written)
	Key       string
}

// OnDisk is what Inspect found in a job's output files.
type OnDisk struct {
	Leaf *x509.Certificate // nil unless the files are complete and consistent
	// Problem is ReasonMissing or ReasonCorrupt when Leaf is nil.
	Problem Reason
}

// Inspect reads a job's output files and checks that they belong
// together: every configured file exists and parses, the key matches the
// leaf, and the full chain starts with the same leaf.
func Inspect(f Files) OnDisk {
	key, err := os.ReadFile(f.Key)
	if err != nil {
		return OnDisk{Problem: problemOf(err)}
	}
	var leaf *x509.Certificate
	for _, path := range []string{f.Cert, f.Fullchain} {
		if path == "" {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return OnDisk{Problem: problemOf(err)}
		}
		c, err := firstCert(data)
		if err != nil {
			return OnDisk{Problem: ReasonCorrupt}
		}
		if leaf != nil && !leaf.Equal(c) {
			return OnDisk{Problem: ReasonCorrupt}
		}
		leaf = c
	}
	if leaf == nil {
		return OnDisk{Problem: ReasonMissing}
	}
	k, err := parseKey(key)
	if err != nil || !publicKeysEqual(leaf.PublicKey, k.Public()) {
		return OnDisk{Problem: ReasonCorrupt}
	}
	return OnDisk{Leaf: leaf}
}

func problemOf(err error) Reason {
	if errors.Is(err, os.ErrNotExist) {
		return ReasonMissing
	}
	return ReasonCorrupt
}

// Decide says whether a job needs a new certificate.
// Inputs: disk from Inspect; ca is the current CA; want the normalized
// names the certificate should carry; renewBefore the renewal window
// (0: DefaultRenewBeforeDays); force a user request.
// Returns: ReasonNone when the files can stay, otherwise the first reason
// found, in the order: forced, missing/corrupt, CA changed, names changed,
// expiring.
func Decide(disk OnDisk, ca *CA, want SANs, renewBefore time.Duration, force bool, now time.Time) Reason {
	if force {
		return ReasonForced
	}
	if disk.Leaf == nil {
		if disk.Problem == "" {
			return ReasonMissing
		}
		return disk.Problem
	}
	leaf := disk.Leaf
	pool := x509.NewCertPool()
	pool.AddCert(ca.Cert)
	// Verify at the leaf's own start so an expired leaf is reported as
	// expiring, not as signed by another CA.
	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots: pool, CurrentTime: leaf.NotBefore.Add(time.Second),
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}); err != nil {
		return ReasonCAChanged
	}
	if !certSANs(leaf).Equal(want) {
		return ReasonSANChanged
	}
	if renewBefore <= 0 {
		renewBefore = DefaultRenewBeforeDays * 24 * time.Hour
	}
	window := max(renewBefore, leaf.NotAfter.Sub(leaf.NotBefore)/3)
	if !now.Before(leaf.NotAfter.Add(-window)) {
		return ReasonExpiring
	}
	return ReasonNone
}

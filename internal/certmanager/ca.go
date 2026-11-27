// Package certmanager is EasyTier MyGo's private certificate authority: it
// generates or imports a root CA, issues TLS server certificates for
// MagicDNS names and virtual addresses, writes them where a web server
// reads them, renews them before they expire and runs a command afterwards.
//
// The package knows nothing about the GUI, the local API or the daemon.
// The daemon owns one Manager and feeds it the names of its networks
// (Options.Names); clients read Status through the local API. Installing
// the CA into the operating system's trust store (Trust) runs in the
// client's process with the client's rights, never in the daemon.
package certmanager

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"
)

// KeyType names a private key algorithm.
type KeyType string

const (
	KeyECDSAP256 KeyType = "ecdsa-p256" // default for CAs and leaves
	KeyRSA2048   KeyType = "rsa-2048"
	KeyRSA4096   KeyType = "rsa-4096"
)

// Defaults for a generated CA.
const (
	DefaultCACommonName   = "EasyTier MyGo Local CA"
	DefaultCAOrganization = "EasyTier MyGo"
	DefaultCAYears        = 10
	maxCAYears            = 30
)

// CAOptions describe a CA to generate. Zero fields take the defaults.
type CAOptions struct {
	CommonName   string  `json:"common_name,omitempty"`
	Organization string  `json:"organization,omitempty"`
	Years        int     `json:"years,omitempty"`
	KeyType      KeyType `json:"key_type,omitempty"`
}

// CA is a usable certificate authority: its certificate and signing key.
type CA struct {
	Cert    *x509.Certificate
	Key     crypto.Signer
	CertPEM []byte
	KeyPEM  []byte
}

// ErrInvalidCA reports a CA certificate or key that cannot be used; the
// message says why.
var ErrInvalidCA = errors.New("invalid CA")

// GenerateCA creates a self-signed root CA.
// Inputs: opts (defaults: DefaultCACommonName, DefaultCAOrganization, 10
// years, ECDSA P-256); now is the issuing time.
// Returns: the CA with PEM encodings (PKCS #8 key).
// Notes: the certificate is a CA (BasicConstraints), may sign certificates
// and CRLs, carries a SubjectKeyId and is limited to one path level (leaves
// only).
func GenerateCA(opts CAOptions, now time.Time) (*CA, error) {
	if opts.CommonName == "" {
		opts.CommonName = DefaultCACommonName
	}
	if opts.Organization == "" {
		opts.Organization = DefaultCAOrganization
	}
	if opts.Years == 0 {
		opts.Years = DefaultCAYears
	}
	if opts.Years < 1 || opts.Years > maxCAYears {
		return nil, fmt.Errorf("CA validity must be 1 to %d years", maxCAYears)
	}
	key, err := newKey(opts.KeyType)
	if err != nil {
		return nil, err
	}
	ski, err := subjectKeyID(key.Public())
	if err != nil {
		return nil, err
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: opts.CommonName, Organization: []string{opts.Organization}},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              now.AddDate(opts.Years, 0, 0),
		IsCA:                  true,
		BasicConstraintsValid: true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		SubjectKeyId:          ski,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, key.Public(), key)
	if err != nil {
		return nil, err
	}
	return assembleCA(der, key)
}

// ParseCA loads a CA from PEM text and checks that it can sign: the first
// certificate is a CA with the certificate-signing key usage, it is valid
// at now, and the key belongs to it.
// Inputs: certPEM (the first CERTIFICATE block is used), keyPEM (PKCS #8,
// PKCS #1 or SEC 1).
// Returns: an error wrapping ErrInvalidCA that names the problem.
func ParseCA(certPEM, keyPEM []byte, now time.Time) (*CA, error) {
	cert, err := firstCert(certPEM)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCA, err)
	}
	if !cert.IsCA || !cert.BasicConstraintsValid {
		return nil, fmt.Errorf("%w: the certificate is not a certificate authority", ErrInvalidCA)
	}
	if cert.KeyUsage != 0 && cert.KeyUsage&x509.KeyUsageCertSign == 0 {
		return nil, fmt.Errorf("%w: the certificate may not sign certificates", ErrInvalidCA)
	}
	if now.Before(cert.NotBefore) {
		return nil, fmt.Errorf("%w: the certificate is not valid before %s", ErrInvalidCA, cert.NotBefore.UTC().Format(time.DateOnly))
	}
	if !now.Before(cert.NotAfter) {
		return nil, fmt.Errorf("%w: the certificate expired on %s", ErrInvalidCA, cert.NotAfter.UTC().Format(time.DateOnly))
	}
	key, err := parseKey(keyPEM)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCA, err)
	}
	if !publicKeysEqual(cert.PublicKey, key.Public()) {
		return nil, fmt.Errorf("%w: the private key does not belong to the certificate", ErrInvalidCA)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCA, err)
	}
	return &CA{
		Cert: cert, Key: key,
		CertPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}),
		KeyPEM:  pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}),
	}, nil
}

// Fingerprint is the SHA-256 of a certificate's DER, upper-case hex with
// colons ("AB:CD:…"), as browsers show it.
func Fingerprint(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.Raw)
	return colonHex(sum[:])
}

// thumbprint is the SHA-1 of a certificate's DER in upper-case hex: how
// Windows and the macOS keychain identify certificates.
func thumbprint(cert *x509.Certificate) string {
	sum := sha1.Sum(cert.Raw)
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

// trustName is the file or label name the CA gets in a system trust store;
// it carries the fingerprint so a replaced CA never collides with the old.
func trustName(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.Raw)
	return "easytier-mygo-ca-" + hex.EncodeToString(sum[:4])
}

func colonHex(b []byte) string {
	s := strings.ToUpper(hex.EncodeToString(b))
	var out strings.Builder
	for i := 0; i < len(s); i += 2 {
		if i > 0 {
			out.WriteByte(':')
		}
		out.WriteString(s[i : i+2])
	}
	return out.String()
}

func assembleCA(der []byte, key crypto.Signer) (*CA, error) {
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	return &CA{
		Cert: cert, Key: key,
		CertPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		KeyPEM:  pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}),
	}, nil
}

// newKey generates a private key of the given type ("" is ECDSA P-256).
func newKey(t KeyType) (crypto.Signer, error) {
	switch t {
	case "", KeyECDSAP256:
		return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	case KeyRSA2048:
		return rsa.GenerateKey(rand.Reader, 2048)
	case KeyRSA4096:
		return rsa.GenerateKey(rand.Reader, 4096)
	}
	return nil, fmt.Errorf("unknown key type %q (use %s, %s or %s)", t, KeyECDSAP256, KeyRSA2048, KeyRSA4096)
}

// subjectKeyID is the SHA-1 of the public key's PKIX encoding (RFC 5280
// method 1 uses the bit string; any stable unique value is allowed).
func subjectKeyID(pub crypto.PublicKey) ([]byte, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, err
	}
	sum := sha1.Sum(der)
	return sum[:], nil
}

// randomSerial is a positive 128-bit serial number.
func randomSerial() (*big.Int, error) {
	return rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
}

// ParseCertificatePEM parses the first certificate of PEM text (a CA file
// a client installs into the trust store).
func ParseCertificatePEM(data []byte) (*x509.Certificate, error) { return firstCert(data) }

// firstCert parses the first CERTIFICATE block of PEM text.
func firstCert(data []byte) (*x509.Certificate, error) {
	certs, err := parseCerts(data)
	if err != nil {
		return nil, err
	}
	return certs[0], nil
}

// parseCerts parses every CERTIFICATE block; other blocks are skipped.
func parseCerts(data []byte) ([]*x509.Certificate, error) {
	var out []*x509.Certificate
	for {
		var b *pem.Block
		b, data = pem.Decode(data)
		if b == nil {
			break
		}
		if b.Type != "CERTIFICATE" {
			continue
		}
		c, err := x509.ParseCertificate(b.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse certificate: %v", err)
		}
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil, errors.New("no PEM certificate found")
	}
	return out, nil
}

// parseKey reads the first private key block in PKCS #8, PKCS #1 or SEC 1
// form. Encrypted keys are refused.
func parseKey(data []byte) (crypto.Signer, error) {
	for {
		var b *pem.Block
		b, data = pem.Decode(data)
		if b == nil {
			return nil, errors.New("no PEM private key found")
		}
		if !strings.HasSuffix(b.Type, "PRIVATE KEY") {
			continue
		}
		if b.Type == "ENCRYPTED PRIVATE KEY" || b.Headers["Proc-Type"] != "" {
			return nil, errors.New("encrypted private keys are not supported; decrypt it first")
		}
		var key any
		var err error
		switch b.Type {
		case "RSA PRIVATE KEY":
			key, err = x509.ParsePKCS1PrivateKey(b.Bytes)
		case "EC PRIVATE KEY":
			key, err = x509.ParseECPrivateKey(b.Bytes)
		default:
			key, err = x509.ParsePKCS8PrivateKey(b.Bytes)
		}
		if err != nil {
			return nil, fmt.Errorf("parse private key: %v", err)
		}
		s, ok := key.(crypto.Signer)
		if !ok {
			return nil, errors.New("unsupported private key type")
		}
		switch s.(type) {
		case *ecdsa.PrivateKey, *rsa.PrivateKey:
			return s, nil
		}
		return nil, errors.New("unsupported private key type (use ECDSA or RSA)")
	}
}

// publicKeysEqual compares two public keys by their PKIX encoding.
func publicKeysEqual(a, b crypto.PublicKey) bool {
	da, err1 := x509.MarshalPKIXPublicKey(a)
	db, err2 := x509.MarshalPKIXPublicKey(b)
	return err1 == nil && err2 == nil && bytes.Equal(da, db)
}

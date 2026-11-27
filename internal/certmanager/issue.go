package certmanager

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strings"
	"time"
)

// Leaf validity limits. 397 days is the longest lifetime Apple platforms
// and Chrome accept for a TLS server certificate.
const (
	DefaultLeafDays = 90
	MaxLeafDays     = 397
)

// SANs are a certificate's subject alternative names, normalized: lower
// case DNS names without a trailing dot, canonical IP strings, each list
// sorted and without duplicates.
type SANs struct {
	DNS []string `json:"dns,omitempty"`
	IPs []string `json:"ips,omitempty"`
}

// Empty reports whether there is no name at all.
func (s SANs) Empty() bool { return len(s.DNS) == 0 && len(s.IPs) == 0 }

// Equal reports whether two normalized sets are the same.
func (s SANs) Equal(o SANs) bool { return slices.Equal(s.DNS, o.DNS) && slices.Equal(s.IPs, o.IPs) }

// All lists every name, DNS names first (the first one becomes the
// certificate's common name).
func (s SANs) All() []string { return append(slices.Clone(s.DNS), s.IPs...) }

// NormalizeSANs validates and normalizes names.
// Inputs: dns may contain one leading wildcard label ("*.et.net"); ips are
// IPv4 or IPv6 addresses (an address with a prefix, "10.0.0.1/24", is
// accepted and the prefix dropped, as EasyTier reports virtual addresses).
// Returns: the normalized set, or an error naming the first invalid entry.
func NormalizeSANs(dns, ips []string) (SANs, error) {
	var out SANs
	for _, d := range dns {
		n, err := normalizeDNS(d)
		if err != nil {
			return SANs{}, err
		}
		if n != "" {
			out.DNS = append(out.DNS, n)
		}
	}
	for _, s := range ips {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if p, err := netip.ParsePrefix(s); err == nil {
			s = p.Addr().String()
		}
		a, err := netip.ParseAddr(s)
		if err != nil || a.Zone() != "" {
			return SANs{}, fmt.Errorf("%q is not an IP address", s)
		}
		out.IPs = append(out.IPs, a.Unmap().String())
	}
	slices.Sort(out.DNS)
	out.DNS = slices.Compact(out.DNS)
	slices.SortFunc(out.IPs, func(a, b string) int {
		return netip.MustParseAddr(a).Compare(netip.MustParseAddr(b))
	})
	out.IPs = slices.Compact(out.IPs)
	return out, nil
}

// normalizeDNS checks one DNS name: ASCII labels of letters, digits, '-'
// and '_' (EasyTier hostnames may carry underscores), at most 63 bytes
// each and 253 in total; only the leftmost label may be '*', and a
// wildcard needs at least two labels after it.
func normalizeDNS(name string) (string, error) {
	n := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(name)), ".")
	if n == "" {
		return "", nil
	}
	if len(n) > 253 {
		return "", fmt.Errorf("%q is too long for a DNS name", name)
	}
	if _, err := netip.ParseAddr(n); err == nil {
		return "", fmt.Errorf("%q is an IP address; list it as an IP", name)
	}
	labels := strings.Split(n, ".")
	for i, l := range labels {
		if l == "*" {
			if i != 0 {
				return "", fmt.Errorf("%q: only the leftmost label may be a wildcard", name)
			}
			if len(labels) < 3 {
				return "", fmt.Errorf("%q: a wildcard needs at least two labels after it", name)
			}
			continue
		}
		if l == "" || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
			return "", fmt.Errorf("%q is not a valid DNS name", name)
		}
		for _, r := range l {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
				return "", fmt.Errorf("%q is not a valid DNS name (use ASCII; punycode for other scripts)", name)
			}
		}
	}
	return n, nil
}

// LeafRequest describes a server certificate to issue.
type LeafRequest struct {
	SANs    SANs
	Days    int     // 0: DefaultLeafDays
	KeyType KeyType // "": ECDSA P-256
}

// Leaf is an issued server certificate with its fresh key.
type Leaf struct {
	Cert    *x509.Certificate
	CertPEM []byte // the leaf alone
	KeyPEM  []byte // PKCS #8
}

// FullchainPEM is the leaf followed by the CA, as web servers expect.
func (l *Leaf) FullchainPEM(ca *CA) []byte {
	return append(append([]byte{}, l.CertPEM...), ca.CertPEM...)
}

// Issue signs a TLS server certificate with a new key.
// Inputs: req.SANs must not be empty; req.Days within 1..MaxLeafDays.
// Returns: the leaf; it never outlives the CA (NotAfter is clamped).
// Notes: names go into the SAN extension (DNSNames, IPAddresses); the
// common name repeats the first one for old clients only. Extended key
// usage is server authentication.
func Issue(ca *CA, req LeafRequest, now time.Time) (*Leaf, error) {
	if ca == nil {
		return nil, errors.New("no CA")
	}
	if req.SANs.Empty() {
		return nil, errors.New("a certificate needs at least one DNS name or IP address")
	}
	days := req.Days
	if days == 0 {
		days = DefaultLeafDays
	}
	if days < 1 || days > MaxLeafDays {
		return nil, fmt.Errorf("certificate validity must be 1 to %d days", MaxLeafDays)
	}
	key, err := newKey(req.KeyType)
	if err != nil {
		return nil, err
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}
	ski, err := subjectKeyID(key.Public())
	if err != nil {
		return nil, err
	}
	notAfter := now.Add(time.Duration(days) * 24 * time.Hour)
	if notAfter.After(ca.Cert.NotAfter) {
		notAfter = ca.Cert.NotAfter
	}
	usage := x509.KeyUsageDigitalSignature
	if _, ok := key.(*rsa.PrivateKey); ok {
		usage |= x509.KeyUsageKeyEncipherment
	}
	ips := make([]net.IP, 0, len(req.SANs.IPs))
	for _, s := range req.SANs.IPs {
		ips = append(ips, net.IP(netip.MustParseAddr(s).AsSlice()))
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: req.SANs.All()[0]},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              notAfter,
		KeyUsage:              usage,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              req.SANs.DNS,
		IPAddresses:           ips,
		SubjectKeyId:          ski,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.Cert, key.Public(), ca.Key)
	if err != nil {
		return nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	return &Leaf{
		Cert:    cert,
		CertPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		KeyPEM:  pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}),
	}, nil
}

// certSANs reads a certificate's names in normalized form.
func certSANs(c *x509.Certificate) SANs {
	ips := make([]string, 0, len(c.IPAddresses))
	for _, ip := range c.IPAddresses {
		ips = append(ips, ip.String())
	}
	s, err := NormalizeSANs(c.DNSNames, ips)
	if err != nil {
		// A name we would not issue: report it raw so the set differs.
		return SANs{DNS: c.DNSNames, IPs: ips}
	}
	return s
}

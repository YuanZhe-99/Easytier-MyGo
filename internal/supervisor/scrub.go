package supervisor

import (
	"regexp"
	"strings"
)

var credentialURL = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://)[^\s/@]+@`)

// ScrubDetail rejects config/credential-bearing lines before storage/transport.
func ScrubDetail(text string) string {
	lower := strings.ToLower(text)
	for _, key := range []string{"network_secret", "private_key", "group_secret", "password", "token=", "token =", "secret=", "secret ="} {
		if strings.Contains(lower, key) {
			return "sensitive diagnostic detail withheld"
		}
	}
	text = credentialURL.ReplaceAllString(text, "${1}[redacted]@")
	text = strings.ReplaceAll(text, "\n", " ")
	if len(text) > 300 {
		text = text[:300] + "…"
	}
	return text
}

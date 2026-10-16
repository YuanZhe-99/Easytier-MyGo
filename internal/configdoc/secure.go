package configdoc

import (
	"crypto/ecdh"
	"encoding/base64"
	"errors"

	"github.com/pelletier/go-toml/v2"
)

// ValidateSecureMode checks prerequisites that the pinned TOML runtime does not
// generate automatically. Errors never include key material or raw TOML.
func ValidateSecureMode(text string) error {
	var doc struct {
		Secure struct {
			Enabled bool   `toml:"enabled"`
			Private string `toml:"local_private_key"`
			Public  string `toml:"local_public_key"`
		} `toml:"secure_mode"`
	}
	if err := toml.Unmarshal([]byte(text), &doc); err != nil {
		return errors.New("cannot validate secure mode in invalid TOML")
	}
	if !doc.Secure.Enabled {
		return nil
	}
	private, err := base64.StdEncoding.DecodeString(doc.Secure.Private)
	if err != nil || len(private) != 32 {
		return errors.New("secure mode requires a base64 32-byte X25519 local_private_key in raw configuration")
	}
	key, err := ecdh.X25519().NewPrivateKey(private)
	if err != nil {
		return errors.New("invalid secure mode private key")
	}
	public, err := base64.StdEncoding.DecodeString(doc.Secure.Public)
	if err != nil || len(public) != 32 {
		return errors.New("secure mode requires a base64 32-byte X25519 local_public_key in raw configuration")
	}
	publicKey, err := ecdh.X25519().NewPublicKey(public)
	if err != nil || !key.PublicKey().Equal(publicKey) {
		return errors.New("secure mode public key does not match private key")
	}
	return nil
}

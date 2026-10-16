package bundlesig

import (
	"crypto/ed25519"
	"os"
	"path/filepath"
	"testing"
)

// TestSignVerify: a signed bundle verifies; a swapped file or another key
// does not.
func TestSignVerify(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	dir := t.TempDir()
	for _, f := range []string{"easytier-mygo-svc", "easytier-core", "easytier-cli"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("binary "+f), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := Sign(dir, "0.1.0", priv); err != nil {
		t.Fatal(err)
	}
	m, err := Verify(dir, pub)
	if err != nil || m.Version != "0.1.0" || len(m.Files) != 3 {
		t.Fatalf("verify: %+v %v", m, err)
	}
	other, _, _ := ed25519.GenerateKey(nil)
	if _, err := Verify(dir, other); err == nil {
		t.Fatal("verified with another key")
	}
	os.WriteFile(filepath.Join(dir, "easytier-core"), []byte("evil"), 0o755)
	if _, err := Verify(dir, pub); err == nil {
		t.Fatal("verified a swapped file")
	}
}

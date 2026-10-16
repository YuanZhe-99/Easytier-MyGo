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

// TestReadKeepsVerifiedBytes: Read returns the contents it hashed, so a file
// swapped on disk afterwards does not change what the caller installs.
func TestReadKeepsVerifiedBytes(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "easytier-mygo-svc"), []byte("signed"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Sign(dir, "0.1.0", priv); err != nil {
		t.Fatal(err)
	}
	b, err := Read(dir, pub)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "easytier-mygo-svc"), []byte("evil"), 0o755)
	if got := string(b.Data["easytier-mygo-svc"]); got != "signed" {
		t.Fatalf("data = %q, want the verified contents", got)
	}
	if names := b.Names(); len(names) != 1 || names[0] != "easytier-mygo-svc" {
		t.Fatalf("names = %v", names)
	}
}

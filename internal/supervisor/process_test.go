package supervisor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCoreLogFiltersInfoAndCapsSize(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "core.log")
	// Legacy backup file is removed on open.
	if err := os.WriteFile(p+".1", []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	w, err := newCoreLog(p, 200)
	if err != nil {
		t.Fatal(err)
	}
	sample := strings.Join([]string{
		"2026-11-27T18:17:10.905Z  INFO CORE::CONN: connect error=TunnelError(",
		"    IOError(",
		"        detail",
		"    ),",
		")",
		"2026-11-27T18:17:11.000Z  WARN CORE: warning ignored",
		"2026-11-27T18:17:11.100Z  ERROR CORE::INSTANCE: first real failure",
		"2026-11-27T18:17:11.200Z  ERROR CORE::INSTANCE: second real failure",
		"2026-11-27T18:17:11.300Z  ERROR CORE::INSTANCE: third real failure",
		"",
	}, "\n")
	if _, err := w.Write([]byte(sample)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(p + ".1"); !os.IsNotExist(err) {
		t.Fatalf("unexpected backup file %s.1: err=%v", p, err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > 200 {
		t.Fatalf("core.log size = %d, exceeds cap 200", len(data))
	}
	text := string(data)
	if strings.Contains(text, "INFO") || strings.Contains(text, "TunnelError") || strings.Contains(text, "WARN") {
		t.Fatalf("non-error lines leaked into core.log:\n%s", text)
	}
	if !strings.Contains(text, "third real failure") {
		t.Fatalf("newest error missing after trim:\n%s", text)
	}
}

func TestCoreLogShrinksOversizedExisting(t *testing.T) {
	p := filepath.Join(t.TempDir(), "core.log")
	old := strings.Repeat("2026-11-27T18:00:00Z  INFO noise line\n", 40) +
		"2026-11-27T18:01:00Z  ERROR kept error\n"
	if err := os.WriteFile(p, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}

	w, err := newCoreLog(p, 200)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > 200 {
		t.Fatalf("shrunk size = %d, exceeds cap 200", len(data))
	}
	text := string(data)
	if strings.Contains(text, "INFO") {
		t.Fatalf("old INFO survived shrink:\n%s", text)
	}
	if !strings.Contains(text, "kept error") {
		t.Fatalf("tail ERROR lost during shrink:\n%s", text)
	}
}

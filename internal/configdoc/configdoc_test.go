package configdoc

import (
	"errors"
	"strings"
	"testing"
)

const sampleDoc = `# my network — comments must survive
instance_name = "home"
ipv4 = "10.144.144.1"   # trailing comment
listeners = ["tcp://0.0.0.0:21110"]
unknown_future_key = { nested = true }

[network_identity]
network_name = "net"
network_secret = "s3cret"

[flags]
no_tun = true
`

// TestFormEditsAreLossless: the form projection rewrites only the changed
// spans; comments, unknown fields and formatting survive, and the result
// still parses.
func TestFormEditsAreLossless(t *testing.T) {
	base, err := Extract(sampleDoc)
	if err != nil {
		t.Fatal(err)
	}
	cur, _ := Extract(sampleDoc)
	host := "laptop"
	ip := "10.144.144.7"
	cur.Hostname = &host
	cur.IPv4 = &ip
	cur.Flags["no_tun"] = false

	out, err := ApplyValues(sampleDoc, Changes(base, cur))
	if err != nil {
		t.Fatal(err)
	}
	for _, keep := range []string{"# my network — comments must survive", "# trailing comment", "unknown_future_key = { nested = true }", `network_secret = "s3cret"`} {
		if !strings.Contains(out, keep) {
			t.Errorf("lost %q:\n%s", keep, out)
		}
	}
	got, err := Extract(out)
	if err != nil {
		t.Fatalf("result does not parse: %v\n%s", err, out)
	}
	if *got.Hostname != host || *got.IPv4 != ip || got.Flags["no_tun"] {
		t.Fatalf("edits not applied:\n%s", out)
	}
	if Changes(base, base).Hostname != nil {
		t.Fatal("unchanged values must not be rewritten")
	}
}

func TestInvalidDocumentIsReported(t *testing.T) {
	_, err := Parse("ipv4 = \n[flags")
	if !IsInvalid(err) {
		t.Fatalf("Parse(broken) = %v, want an invalid-document error", err)
	}
}

// TestStoreConflictAndBackup: saves are conflict-checked against the
// revision the editor started from, and the previous text is backed up.
func TestStoreConflictAndBackup(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	e, err := s.Create("home", sampleDoc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save("home", sampleDoc+"\n# edit 1\n", e.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save("home", sampleDoc+"\n# stale edit\n", e.Revision); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale save = %v, want ErrConflict", err)
	}
	text, _, err := s.Read("home")
	if err != nil || !strings.Contains(text, "# edit 1") {
		t.Fatalf("read after conflict: %q, %v", text, err)
	}
	list, err := s.List()
	if err != nil || len(list) != 1 || list[0].Revision != Revision(text) {
		t.Fatalf("list = %+v, %v", list, err)
	}
}

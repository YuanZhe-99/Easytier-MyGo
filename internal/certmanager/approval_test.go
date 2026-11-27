package certmanager

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestHelperNeedsApproval is the privileged helper's contract: a job with
// its own folder or a command is accepted only while an administrator's
// approval covers exactly those settings; any change, or a folder
// redirected after approval, stops it from writing or running anything.
func TestHelperNeedsApproval(t *testing.T) {
	dir := t.TempDir()
	m, err := New(Options{Dir: dir, Restricted: true, Now: func() time.Time { return t0 }, Logf: t.Logf})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.GenerateCA(CAOptions{}, false); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "srv")
	marker := filepath.Join(out, "hook.log")
	job := Job{ID: "web", DNSNames: []string{"web.et.net"}, OutDir: out,
		Hook: Hook{Enabled: true, Command: sh(`echo run >> "{{OUT_DIR}}/hook.log"`, `echo run>> "{{OUT_DIR}}\hook.log"`)}}

	// A job in the helper's own folder without a command needs nothing.
	if _, err := m.SetJob(Job{ID: "plain", DNSNames: []string{"a.et.net"}}); err != nil {
		t.Fatalf("plain job: %v", err)
	}
	if _, err := m.SetJob(job); !errors.Is(err, ErrNeedsApproval) {
		t.Fatalf("unapproved job: %v", err)
	}
	if _, err := Approve(dir, Job{ID: "plain", DNSNames: []string{"a.et.net"}}, "", t0); err == nil {
		t.Error("approving a job that needs nothing succeeded")
	}
	a, err := Approve(dir, job, "1000", t0)
	if err != nil {
		t.Fatal(err)
	}
	if a.Command == "" || a.Resolved == "" || a.ApprovedBy != "1000" {
		t.Errorf("approval record: %+v", a)
	}
	if _, err := m.SetJob(job); err != nil {
		t.Fatalf("approved job: %v", err)
	}
	if err := m.Check(context.Background(), "web", false); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(marker); !strings.Contains(string(data), "run") {
		js := m.Status().Jobs[1]
		t.Fatalf("approved command did not run: %q %+v", js.LastError, js.Hook)
	}

	// Names may change without a new approval; the command may not.
	job.DNSNames = []string{"web.et.net", "www.et.net"}
	if _, err := m.SetJob(job); err != nil {
		t.Fatalf("names changed: %v", err)
	}
	changed := job
	changed.Hook.Command = "echo other"
	if _, err := m.SetJob(changed); !errors.Is(err, ErrNeedsApproval) {
		t.Fatalf("changed command: %v", err)
	}
	moved := job
	moved.KeyMode = "0644"
	if _, err := m.SetJob(moved); !errors.Is(err, ErrNeedsApproval) {
		t.Fatalf("changed key permissions: %v", err)
	}

	// An approval replaced behind the stored job stops it at the next check.
	other := job
	other.Hook.Command = "echo replaced"
	if _, err := Approve(dir, other, "", t0); err != nil {
		t.Fatal(err)
	}
	os.Remove(marker)
	if err := m.Check(context.Background(), "web", true); err != nil {
		t.Fatal(err)
	}
	if js := m.Status().Jobs[1]; !strings.Contains(js.LastError, "approval") {
		t.Fatalf("job with a stale approval: %q", js.LastError)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a command ran without a matching approval")
	}

	// Removing the job drops its approval.
	if err := m.RemoveJob("web"); err != nil {
		t.Fatal(err)
	}
	if list, _ := readApprovals(dir); len(list) != 0 {
		t.Fatalf("approvals after removal: %+v", list)
	}
}

// TestApprovalFollowsTheFolder: a folder reached through a link that is
// pointed elsewhere after approval is refused (root would otherwise write
// wherever a user aims the link).
func TestApprovalFollowsTheFolder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic links need extra rights on Windows")
	}
	dir, base := t.TempDir(), t.TempDir()
	real, elsewhere := filepath.Join(base, "real"), filepath.Join(base, "elsewhere")
	os.MkdirAll(real, 0o755)
	os.MkdirAll(elsewhere, 0o755)
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	m, err := New(Options{Dir: dir, Restricted: true, Now: func() time.Time { return t0 }})
	if err != nil {
		t.Fatal(err)
	}
	job := Job{ID: "web", DNSNames: []string{"web.et.net"}, OutDir: filepath.Join(link, "certs")}
	if _, err := Approve(dir, job, "", t0); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SetJob(job); err != nil {
		t.Fatalf("approved: %v", err)
	}
	os.Remove(link)
	if err := os.Symlink(elsewhere, link); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SetJob(job); !errors.Is(err, ErrNeedsApproval) {
		t.Fatalf("redirected folder: %v", err)
	}
}

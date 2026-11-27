package certmanager

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/fsutil"
)

// Administrator approvals (doc/en-us/certificates.md, "Privileged helper").
//
// The privileged helper runs as root/SYSTEM and takes requests from every
// member of its group. A certificate that writes to a folder of the
// caller's choice, or runs a command after renewal, would let any of them
// act as root, so the helper accepts such a certificate only while an
// administrator has approved exactly those settings. Approvals live in
// <dir>/approved.json; only `easytier-mygo-svc helper approve-cert`, run
// with administrator rights, adds them (Approve). No local API request can.

// approvalsFile holds the approvals, next to jobs.json.
const approvalsFile = "approved.json"

// ErrNeedsApproval refuses a privileged helper's certificate whose folder
// or command an administrator has not approved (or approved differently).
var ErrNeedsApproval = errors.New("a folder of your choice or a command after renewal needs administrator approval in the privileged helper")

// Approval is an administrator's consent to one certificate's privileged
// settings.
type Approval struct {
	JobID string `json:"job_id"`
	// Digest covers every setting the approval is for (privilegedDigest);
	// changing any of them needs a new approval.
	Digest string `json:"digest"`
	// OutDir and Resolved are the folder as given and as it resolved when
	// approved (symbolic links followed): the helper writes only while it
	// still resolves to the same place.
	OutDir   string `json:"out_dir,omitempty"`
	Resolved string `json:"resolved,omitempty"`
	// Command describes the hook for people reading the file.
	Command    string    `json:"command,omitempty"`
	ApprovedAt time.Time `json:"approved_at"`
	// ApprovedBy names who asked for the elevation (uid or SID), when known.
	ApprovedBy string `json:"approved_by,omitempty"`
}

// NeedsApproval reports whether a job uses what the privileged helper
// allows only with an administrator's approval: its own folder or a
// command after renewal.
func NeedsApproval(j Job) bool { return j.OutDir != "" || j.Hook.Enabled }

// privilegedDigest is a hash of the settings an approval covers: where the
// files go, their names and permissions, and the command.
func privilegedDigest(j Job) string {
	data, _ := json.Marshal(struct {
		ID, OutDir, CertFile, FullchainFile, KeyFile, CertMode, KeyMode string
		Hook                                                            Hook
	}{j.ID, j.OutDir, j.CertFile, j.FullchainFile, j.KeyFile, j.CertMode, j.KeyMode, j.Hook})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// resolveDir is a folder with its symbolic links followed. A folder that
// does not exist yet resolves through its nearest existing parent.
func resolveDir(dir string) (string, error) {
	dir = filepath.Clean(dir)
	var rest []string
	for {
		r, err := filepath.EvalSymlinks(dir)
		if err == nil {
			return filepath.Join(append([]string{r}, rest...)...), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", err
		}
		rest = append([]string{filepath.Base(dir)}, rest...)
		dir = parent
	}
}

// Approve records an administrator's approval of a job's folder, file
// names, permissions and command, replacing any earlier one for its ID.
// Inputs: dir is the privileged helper's certificate directory
// (<state>/certs); by names who asked (uid or SID, may be empty).
// Returns: the approval written; an error for an invalid job or one that
// needs no approval.
// Side effects: writes <dir>/approved.json (0600). Callers must run with
// administrator rights: this is the only way approvals are added.
func Approve(dir string, j Job, by string, now time.Time) (Approval, error) {
	if err := j.validate(); err != nil {
		return Approval{}, err
	}
	if !NeedsApproval(j) {
		return Approval{}, errors.New("this certificate uses neither its own folder nor a command; it needs no approval")
	}
	a := Approval{JobID: j.ID, Digest: privilegedDigest(j), OutDir: j.OutDir, ApprovedAt: now.UTC(), ApprovedBy: by}
	if j.Hook.Enabled {
		a.Command = j.Hook.Command
		if len(j.Hook.Args) > 0 {
			a.Command = fmt.Sprintf("%q", j.Hook.Args)
		}
	}
	if j.OutDir != "" {
		r, err := resolveDir(j.OutDir)
		if err != nil {
			return Approval{}, fmt.Errorf("the folder %s: %w", j.OutDir, err)
		}
		a.Resolved = r
	}
	list, err := readApprovals(dir)
	if err != nil {
		return Approval{}, err
	}
	list = slices.DeleteFunc(list, func(o Approval) bool { return o.JobID == j.ID })
	list = append(list, a)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Approval{}, err
	}
	return a, fsutil.WriteJSON(filepath.Join(dir, approvalsFile), list)
}

// readApprovals loads the approvals; a missing file is an empty list.
func readApprovals(dir string) ([]Approval, error) {
	var list []Approval
	if _, err := fsutil.ReadJSON(filepath.Join(dir, approvalsFile), &list); err != nil {
		return nil, fmt.Errorf("certificate approvals: %w", err)
	}
	return list, nil
}

// approved checks a job against the approvals: one exists for its ID, it
// covers exactly the job's privileged settings, and the folder still
// resolves where it did when approved.
// Returns: nil when the job may write and run its command.
func (m *Manager) approved(j Job) error {
	list, err := readApprovals(m.opts.Dir)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(list, func(a Approval) bool { return a.JobID == j.ID })
	if i < 0 || list[i].Digest != privilegedDigest(j) {
		return ErrNeedsApproval
	}
	if j.OutDir != "" {
		r, err := resolveDir(j.OutDir)
		if err != nil || r != list[i].Resolved {
			return fmt.Errorf("%w: the folder %s no longer leads where it did when it was approved", ErrNeedsApproval, j.OutDir)
		}
	}
	return nil
}

// dropApproval forgets a removed job's approval.
func (m *Manager) dropApproval(id string) {
	list, err := readApprovals(m.opts.Dir)
	if err != nil {
		return
	}
	n := len(list)
	list = slices.DeleteFunc(list, func(a Approval) bool { return a.JobID == id })
	if len(list) != n {
		if err := fsutil.WriteJSON(filepath.Join(m.opts.Dir, approvalsFile), list); err != nil {
			m.opts.Logf("certificates: approvals: %v", err)
		}
	}
}

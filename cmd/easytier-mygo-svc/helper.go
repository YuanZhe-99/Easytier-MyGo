package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/certmanager"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/cores"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/fsutil"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/helper"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/localapi"
)

// helperGroup is the group whose members may control the helper.
const helperGroup = "easytier-mygo"

// helperConfig is written when the helper is installed.
type helperConfig struct {
	// Operator is the uid of the user who installed the helper.
	Operator *uint32 `json:"operator,omitempty"`
	// OperatorSID is the operator on Windows.
	OperatorSID string `json:"operator_sid,omitempty"`
}

// helperAuthorizer grants write access to root, the operator and the
// helper group; everyone else is refused.
// Returns: the authorizer and the group's gid (0 when the group is missing).
func helperAuthorizer(stateDir string) (localapi.Authorizer, int, error) {
	var cfg helperConfig
	if _, err := fsutil.ReadJSON(filepath.Join(stateDir, "helper.json"), &cfg); err != nil {
		return localapi.Authorizer{}, 0, err
	}
	// On Windows the helper runs as SYSTEM; callers are identified by SID.
	a := localapi.Authorizer{OwnerUID: 0, OwnerSID: "S-1-5-18", AdminsWrite: true}
	if cfg.OperatorSID != "" {
		a.WriteSIDs = append(a.WriteSIDs, cfg.OperatorSID)
	}
	if cfg.Operator != nil {
		a.WriteUIDs = append(a.WriteUIDs, *cfg.Operator)
	}
	gid := 0
	if g, err := user.LookupGroup(helperGroup); err == nil {
		if v, err := strconv.Atoi(g.Gid); err == nil {
			gid = v
			a.WriteGIDs = append(a.WriteGIDs, uint32(v))
		}
	}
	return a, gid, nil
}

// helperCmd installs, removes or inspects the privileged helper.
// Notes: install/uninstall run as root through pkexec; the operator is the
// user pkexec reports (PKEXEC_UID) or, under sudo, SUDO_UID.
func helperCmd(args []string) error {
	if len(args) == 0 {
		usage()
	}
	switch args[0] {
	case "install":
		fs := flag.NewFlagSet("helper install", flag.ExitOnError)
		from := fs.String("from", "", "directory with the bundle's binaries (default: this binary's directory)")
		operator := fs.Int("operator", -1, "operator uid when not run through pkexec or sudo (macOS)")
		operatorSID := fs.String("operator-sid", "", "operator SID (Windows)")
		fs.Parse(args[1:])
		var op uint32
		if *operatorSID == "" {
			var err error
			if op, err = invokingUID(); err != nil {
				if *operator < 0 {
					return err
				}
				op = uint32(*operator)
			}
		}
		dir := *from
		if dir == "" {
			exe, err := os.Executable()
			if err != nil {
				return err
			}
			dir = filepath.Dir(exe)
		}
		return helper.Install(helper.InstallOptions{From: dir, Operator: op, OperatorSID: *operatorSID})
	case "uninstall":
		fs := flag.NewFlagSet("helper uninstall", flag.ExitOnError)
		purge := fs.Bool("purge", false, "also delete the helper's networks and state")
		fs.Parse(args[1:])
		return helper.Uninstall(*purge)
	case "status":
		return json.NewEncoder(os.Stdout).Encode(helper.Inspect())
	case "socket":
		fmt.Println(localapi.SystemSocketPath)
		return nil
	case "add-core":
		fs := flag.NewFlagSet("helper add-core", flag.ExitOnError)
		name := fs.String("name", "", "name of the custom core")
		from := fs.String("from", "", "directory with easytier-core and easytier-cli")
		fs.Parse(args[1:])
		if !helper.Elevated() {
			return errors.New("adding a core to the helper needs administrator rights")
		}
		upx := "upx"
		if runtime.GOOS == "windows" {
			upx += ".exe"
		}
		m := cores.Manager{Dir: filepath.Join(helper.StateDir, "cores"), UPX: filepath.Join(helper.BinDir, upx)}
		c, err := m.AddCustom(*name, *from)
		if err != nil {
			return err
		}
		if runtime.GOOS == "windows" {
			return nil // the state directory's ACL already limits writers
		}
		for _, f := range []string{"easytier-core", "easytier-cli"} {
			if err := os.Chown(filepath.Join(m.Dir, c.Tag, f), 0, 0); err != nil {
				return err
			}
		}
		return nil
	case "approve-cert":
		return approveCert(args[1:])
	}
	usage()
	return nil
}

// approveCert records an administrator's approval of one certificate's
// own folder, file names, permissions and command in the privileged
// helper (doc/en-us/certificates.md, "Privileged helper").
// Inputs: -job (the job as base64url JSON, as the GUI passes it) or -file
// (a JSON file, as the CLI suggests).
// Side effects: writes <helper state>/certs/approved.json; prints what was
// approved.
// Notes: refuses to run without administrator rights: this command is the
// only way an approval is added.
func approveCert(args []string) error {
	fs := flag.NewFlagSet("helper approve-cert", flag.ExitOnError)
	encoded := fs.String("job", "", "the certificate job as base64url-encoded JSON")
	file := fs.String("file", "", "the certificate job as a JSON file")
	fs.Parse(args)
	if !helper.Elevated() {
		return errors.New("approving a certificate's folder or command needs administrator rights")
	}
	if helper.StateDir == "" {
		return errors.New("the privileged helper is not supported on this system")
	}
	var data []byte
	var err error
	switch {
	case *encoded != "":
		data, err = base64.RawURLEncoding.DecodeString(strings.TrimRight(*encoded, "="))
	case *file != "":
		data, err = os.ReadFile(*file)
	default:
		return errors.New("pass -job or -file")
	}
	if err != nil {
		return err
	}
	var job certmanager.Job
	if err := json.Unmarshal(data, &job); err != nil {
		return fmt.Errorf("certificate job: %w", err)
	}
	by := ""
	if uid, err := invokingUID(); err == nil {
		by = strconv.FormatUint(uint64(uid), 10)
	} else if u, err := user.Current(); err == nil {
		by = u.Username
	}
	a, err := certmanager.Approve(filepath.Join(helper.StateDir, "certs"), job, by, time.Now())
	if err != nil {
		return err
	}
	fmt.Printf("approved for certificate %s:\n", a.JobID)
	if a.OutDir != "" {
		fmt.Printf("  folder:  %s (resolves to %s)\n", a.OutDir, a.Resolved)
	}
	if a.Command != "" {
		fmt.Printf("  command: %s (runs as administrator)\n", a.Command)
	}
	return nil
}

// invokingUID is the user who asked for the elevation.
func invokingUID() (uint32, error) {
	for _, env := range []string{"PKEXEC_UID", "SUDO_UID"} {
		if v := os.Getenv(env); v != "" {
			n, err := strconv.ParseUint(v, 10, 32)
			if err != nil {
				return 0, err
			}
			return uint32(n), nil
		}
	}
	return 0, errors.New("run through pkexec or sudo so the operator is known")
}

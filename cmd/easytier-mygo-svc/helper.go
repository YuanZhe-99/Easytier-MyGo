package main

import (
	"encoding/json"
	"errors"
	"flag"
	"os"
	"os/user"
	"path/filepath"
	"strconv"

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
}

// helperAuthorizer grants write access to root, the operator and the
// helper group; everyone else is refused.
// Returns: the authorizer and the group's gid (0 when the group is missing).
func helperAuthorizer(stateDir string) (localapi.Authorizer, int, error) {
	var cfg helperConfig
	if _, err := fsutil.ReadJSON(filepath.Join(stateDir, "helper.json"), &cfg); err != nil {
		return localapi.Authorizer{}, 0, err
	}
	a := localapi.Authorizer{OwnerUID: 0}
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
		fs.Parse(args[1:])
		op, err := invokingUID()
		if err != nil {
			return err
		}
		dir := *from
		if dir == "" {
			exe, err := os.Executable()
			if err != nil {
				return err
			}
			dir = filepath.Dir(exe)
		}
		return helper.Install(helper.InstallOptions{From: dir, Operator: op})
	case "uninstall":
		fs := flag.NewFlagSet("helper uninstall", flag.ExitOnError)
		purge := fs.Bool("purge", false, "also delete the helper's networks and state")
		fs.Parse(args[1:])
		return helper.Uninstall(*purge)
	case "status":
		return json.NewEncoder(os.Stdout).Encode(helper.Inspect())
	}
	usage()
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

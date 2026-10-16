package main

import (
	"os/user"
	"path/filepath"
	"strconv"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/fsutil"
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

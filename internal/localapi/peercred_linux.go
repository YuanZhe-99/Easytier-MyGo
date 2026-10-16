//go:build linux

package localapi

import (
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// socketCredentials is Linux's SO_PEERCRED plus the peer's supplementary
// groups from /proc/<pid>/status, so membership added with `gpasswd -a`
// counts even when it is not the caller's primary group.
func socketCredentials(fd int) (Peer, error) {
	cred, err := unix.GetsockoptUcred(fd, unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil {
		return Peer{}, err
	}
	p := Peer{UID: cred.Uid, GID: cred.Gid, PID: uint32(cred.Pid), Known: true}
	p.Groups = procGroups(int(cred.Pid))
	return p, nil
}

// procGroups reads the "Groups:" line of /proc/<pid>/status.
func procGroups(pid int) []uint32 {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/status")
	if err != nil {
		return nil
	}
	for _, line := range strings.Split(string(data), "\n") {
		rest, ok := strings.CutPrefix(line, "Groups:")
		if !ok {
			continue
		}
		var out []uint32
		for _, f := range strings.Fields(rest) {
			if v, err := strconv.ParseUint(f, 10, 32); err == nil {
				out = append(out, uint32(v))
			}
		}
		return out
	}
	return nil
}

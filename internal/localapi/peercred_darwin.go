//go:build darwin

package localapi

import "golang.org/x/sys/unix"

// socketCredentials is macOS's LOCAL_PEERCRED: an xucred carrying the uid
// and the group list. macOS reports no pid here, and the primary gid is
// not part of xucred, so gid is reported as the first supplementary group
// (or 0); callers that need group membership should rely on the uid and
// the socket's own ownership.
func socketCredentials(fd int) (uid, gid, pid uint32, err error) {
	cred, err := unix.GetsockoptXucred(fd, unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
	if err != nil {
		return 0, 0, 0, err
	}
	first := uint32(0)
	if cred.Ngroups > 0 {
		first = cred.Groups[0]
	}
	return cred.Uid, first, 0, nil
}

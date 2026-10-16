//go:build linux

package localapi

import "golang.org/x/sys/unix"

// socketCredentials is Linux's SO_PEERCRED: a ucred with uid, gid and pid.
func socketCredentials(fd int) (uid, gid, pid uint32, err error) {
	cred, err := unix.GetsockoptUcred(fd, unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil {
		return 0, 0, 0, err
	}
	return cred.Uid, cred.Gid, uint32(cred.Pid), nil
}

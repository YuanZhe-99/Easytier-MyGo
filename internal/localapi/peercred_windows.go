//go:build windows

package localapi

import (
	"fmt"
	"net"
)

// peerCredentials is not implemented for a Unix socket on Windows: the
// Windows transport will be a named pipe, whose client token carries the
// identity (GetNamedPipeClientProcessId / an impersonation check). Until
// that backend exists, the caller is told so rather than being trusted.
func peerCredentials(conn net.Conn) (Peer, error) {
	return Peer{}, fmt.Errorf("local API over named pipes is not implemented yet")
}

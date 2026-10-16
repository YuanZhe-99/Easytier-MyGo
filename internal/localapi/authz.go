package localapi

// Authorization for the local API.
//
// The socket's filesystem permissions decide who may connect at all; the
// kernel-reported peer identity decides what they may do. Two tiers are
// enough for the first release:
//
//   - AccessWrite: the authorized group (and the socket owner). Everything
//     the daemon exposes.
//   - AccessRead: any other user who can reach the socket. Snapshots,
//     node/route lists and health events, but never persisted config text
//     (it carries the network secret).
//
// A private (per-user) socket is owned by the user and only that user can
// reach it, so its connections are always AccessWrite. A system socket is
// root-owned with a group; the group is the write set and everyone else
// with read/execute on the socket is read-only.
//
// A connection whose identity cannot be read is refused outright: an
// unknown caller is never assumed to be authorized.

import (
	"fmt"
	"slices"
)

// Access is what a connection is allowed to do.
type Access string

const (
	// AccessDenied means the connection is closed without answering.
	AccessDenied Access = "denied"
	// AccessRead allows snapshots, node/route lists, health events and
	// policy display.
	AccessRead Access = "read"
	// AccessWrite allows every command, including config text and
	// lifecycle changes.
	AccessWrite Access = "write"
)

// Peer is the identity of a connected client, as reported by the kernel.
type Peer struct {
	UID uint32
	GID uint32
	PID uint32
	// Groups are the peer's supplementary groups (Linux: from /proc, macOS:
	// from LOCAL_PEERCRED); membership in any of them counts.
	Groups []uint32
	// Known is false when the platform could not report an identity.
	Known bool
}

// Authorizer decides the access tier of a connection. It is consulted
// once per accepted connection.
type Authorizer struct {
	// OwnerUID is the socket's owner: its own connections are always
	// writable. On a per-user socket this is the user; on a system socket
	// it is root.
	OwnerUID uint32
	// WriteUIDs are users granted write access explicitly (normally empty
	// on a per-user socket).
	WriteUIDs []uint32
	// WriteGIDs are groups granted write access (the service's group on a
	// system socket).
	WriteGIDs []uint32
	// ReadUIDs and ReadGIDs grant read-only access. On a per-user socket
	// they are empty; on a system socket a read group can be added when
	// the socket mode allows it.
	ReadUIDs []uint32
	ReadGIDs []uint32
	// AllowOthersRead makes any local user who can reach a non-private
	// socket read-only. When false, only the owner, the write set and the
	// read set are allowed; anyone else is denied.
	AllowOthersRead bool
	// Private marks a socket that only its owner can reach, so its
	// connections are always writable and group lists are not consulted.
	Private bool
}

// Access reports the tier for one peer, or AccessDenied. It never fails
// open: an unknown identity or a private socket reached by someone else
// is denied.
func (a Authorizer) Access(p Peer) Access {
	if !p.Known {
		return AccessDenied
	}
	if p.UID == a.OwnerUID {
		return AccessWrite
	}
	if a.Private {
		// Someone else reached a socket only the owner should be able to
		// reach: the filesystem should have stopped this, so treat it as
		// a configuration problem and refuse.
		return AccessDenied
	}
	if slices.Contains(a.WriteUIDs, p.UID) || p.inAny(a.WriteGIDs) {
		return AccessWrite
	}
	if slices.Contains(a.ReadUIDs, p.UID) || p.inAny(a.ReadGIDs) {
		return AccessRead
	}
	if a.AllowOthersRead {
		return AccessRead
	}
	// A system socket's mode lets the group in; anyone else who reaches
	// it (a misconfigured mode) is refused rather than silently downgraded.
	return AccessDenied
}

// inAny reports whether the peer's primary or any supplementary group is
// in gids.
func (p Peer) inAny(gids []uint32) bool {
	if slices.Contains(gids, p.GID) {
		return true
	}
	for _, g := range p.Groups {
		if slices.Contains(gids, g) {
			return true
		}
	}
	return false
}

// allows reports whether the tier permits a request type.
func (t Access) allows(req RequestType) bool {
	if t == AccessWrite {
		return true
	}
	if t != AccessRead {
		return false
	}
	return !writeRequests[req]
}

// Allows is shared by transport and handlers so internal dispatch cannot bypass
// the socket's read/write contract.
func (t Access) Allows(req RequestType) bool { return t.allows(req) }

// writeRequests are the requests that change state or expose secrets.
// Everything not listed here is readable by an AccessRead peer.
var writeRequests = map[RequestType]bool{
	RequestConfigGet:     true, // returns the raw text, which holds the secret
	RequestConfigSave:    true,
	RequestNetworkCreate: true,
	RequestNetworkDelete: true,
	RequestNetworkRename: true,
	RequestConnect:       true,
	RequestDisconnect:    true,
	RequestApply:         true,
	RequestRetry:         true,
	RequestSettingsSet:   true,
	RequestSessionAttach: true, // a session can stop networks when it ends
	RequestSessionWatch:  true,
	RequestShutdown:      true,
}

// ErrForbidden is returned when a request exceeds the connection's tier.
type ErrForbidden struct {
	Tier Access
	Type RequestType
}

func (e ErrForbidden) Error() string {
	return fmt.Sprintf("permission denied: %s access cannot run %q", e.Tier, e.Type)
}

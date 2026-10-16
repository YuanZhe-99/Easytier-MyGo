package localapi

import "testing"

// TestAuthorizer covers the tiers, including membership through a
// supplementary group (users added with `gpasswd -a`).
func TestAuthorizer(t *testing.T) {
	helper := Authorizer{OwnerUID: 0, WriteUIDs: []uint32{1000}, WriteGIDs: []uint32{990}}
	cases := []struct {
		name string
		a    Authorizer
		p    Peer
		want Access
	}{
		{"unknown identity", helper, Peer{UID: 1000}, AccessDenied},
		{"root", helper, Peer{UID: 0, Known: true}, AccessWrite},
		{"operator", helper, Peer{UID: 1000, GID: 1000, Known: true}, AccessWrite},
		{"group as primary", helper, Peer{UID: 1001, GID: 990, Known: true}, AccessWrite},
		{"group as supplementary", helper, Peer{UID: 1001, GID: 1001, Groups: []uint32{27, 990}, Known: true}, AccessWrite},
		{"other user", helper, Peer{UID: 1002, GID: 1002, Groups: []uint32{27}, Known: true}, AccessDenied},
		{"private socket owner", Authorizer{OwnerUID: 1000, Private: true}, Peer{UID: 1000, Known: true}, AccessWrite},
		{"private socket other", Authorizer{OwnerUID: 1000, Private: true, WriteGIDs: []uint32{990}}, Peer{UID: 1001, GID: 990, Known: true}, AccessDenied},
	}
	for _, tc := range cases {
		if got := tc.a.Access(tc.p); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
	win := Authorizer{OwnerSID: "S-1-5-18", WriteSIDs: []string{"S-1-5-21-1-1000"}, AdminsWrite: true}
	for _, tc := range []struct {
		p    Peer
		want Access
	}{
		{Peer{SID: "S-1-5-18", Known: true}, AccessWrite},
		{Peer{SID: "S-1-5-21-1-1000", Known: true}, AccessWrite},
		{Peer{SID: "S-1-5-21-1-1001", Admin: true, Known: true}, AccessWrite},
		{Peer{SID: "S-1-5-21-1-1002", Known: true}, AccessDenied},
	} {
		if got := win.Access(tc.p); got != tc.want {
			t.Errorf("windows %+v: %v, want %v", tc.p, got, tc.want)
		}
	}
	if (AccessRead).Allows(RequestConfigGet) || !(AccessRead).Allows(RequestNetworks) {
		t.Error("read tier must see status but never config text")
	}
}

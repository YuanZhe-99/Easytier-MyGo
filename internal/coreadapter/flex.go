package coreadapter

import (
	"bytes"
	"encoding/json"
	"strconv"
)

// Uint64 is a counter that EasyTier prints either as a JSON number
// (v2.6.x) or, following protobuf's canonical JSON, as a quoted string
// (v2.7.0).
type Uint64 uint64

// UnmarshalJSON accepts 42 and "42".
func (u *Uint64) UnmarshalJSON(b []byte) error {
	v, err := strconv.ParseUint(string(bytes.Trim(b, `"`)), 10, 64)
	if err != nil {
		if string(b) == "null" {
			return nil
		}
		return err
	}
	*u = Uint64(v)
	return nil
}

// natNames are the NAT type names v2.7.0 prints instead of numbers.
var natNames = map[string]NatType{
	"Unknown": NatUnknown, "OpenInternet": NatOpenInternet, "NoPAT": NatNoPAT, "FullCone": NatFullCone,
	"Restricted": NatRestricted, "PortRestricted": NatPortRestricted, "Symmetric": NatSymmetric,
	"SymUdpFirewall": NatSymUDPFirewall, "SymmetricEasyInc": NatSymmetricEasyInc, "SymmetricEasyDec": NatSymmetricEasyDec,
}

// UnmarshalJSON accepts the numeric enum (v2.6.x) and the name (v2.7.0);
// an unknown name reads as NatUnknown rather than failing the whole query.
func (n *NatType) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*n = natNames[s]
		return nil
	}
	var v int32
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*n = NatType(v)
	return nil
}

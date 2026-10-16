package daemon

import (
	"fmt"

	"github.com/pelletier/go-toml/v2"
)

// helperUnsafeKeys are configuration options a core running as root must
// not take from a user's document: they name files the core would read or
// write with root's rights.
var helperUnsafeKeys = []string{"credential_file", "file_logger"}

// checkHelperSafety refuses documents with path-valued options when the
// daemon is the privileged helper.
// Returns: nil for a safe document (or one that does not parse; the
// core's own preflight reports parse errors).
func checkHelperSafety(text string) error {
	var doc map[string]any
	if err := toml.Unmarshal([]byte(text), &doc); err != nil {
		return nil
	}
	for _, key := range helperUnsafeKeys {
		if _, ok := doc[key]; ok {
			return fmt.Errorf("%q is not supported for networks run by the privileged helper: it would let the core access files as root", key)
		}
	}
	return nil
}

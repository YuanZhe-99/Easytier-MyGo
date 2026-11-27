//go:build !linux && !darwin && !windows

package certmanager

import "os"

// backend: no trust store support on this system.
func backend() trustBackend { return nil }

// Elevated reports whether this process runs as root.
func Elevated() bool { return os.Geteuid() == 0 }

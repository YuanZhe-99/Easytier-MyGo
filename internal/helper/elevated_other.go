//go:build !windows

package helper

import "os"

// Elevated reports whether this process runs as root.
func Elevated() bool { return os.Geteuid() == 0 }

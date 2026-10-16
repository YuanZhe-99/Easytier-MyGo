//go:build !windows

package supervisor

import "os"

// adoptProcess needs nothing outside Windows (process groups and
// Pdeathsig tie cores to the daemon).
func adoptProcess(*os.Process) {}

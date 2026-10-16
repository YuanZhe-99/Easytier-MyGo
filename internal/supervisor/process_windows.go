package supervisor

import (
	"os"
	"syscall"
)

// managedProcessAttrs has no process-group setup on Windows yet; the
// Windows backend (job objects) is future work.
func managedProcessAttrs() *syscall.SysProcAttr { return &syscall.SysProcAttr{} }

// terminateGroup has no graceful signal on Windows; the core is killed.
// Notes: placeholder until the Windows backend exists.
func terminateGroup(pid int) { killGroup(pid) }

// killGroup force-stops the core process.
func killGroup(pid int) {
	if p, err := os.FindProcess(pid); err == nil {
		_ = p.Kill()
	}
}

//go:build !windows

package supervisor

import "syscall"

// terminateGroup asks the core's process group to stop (SIGTERM), so
// nothing under the core outlives it.
func terminateGroup(pid int) { signalGroup(pid, syscall.SIGTERM) }

// killGroup force-stops the core's process group (SIGKILL).
func killGroup(pid int) { signalGroup(pid, syscall.SIGKILL) }

func signalGroup(pid int, sig syscall.Signal) {
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		pgid = pid
	}
	_ = syscall.Kill(-pgid, sig)
}

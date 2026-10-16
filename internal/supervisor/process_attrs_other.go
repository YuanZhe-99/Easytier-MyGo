//go:build !linux && !windows

package supervisor

import "syscall"

func managedProcessAttrs() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

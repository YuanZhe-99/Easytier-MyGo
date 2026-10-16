//go:build !linux && !windows

package daemon

import "syscall"

func managedProcessAttrs() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

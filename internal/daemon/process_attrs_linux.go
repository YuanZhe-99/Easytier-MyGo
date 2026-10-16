package daemon

import "syscall"

// Parent death must not leave a managed core competing with the restarted daemon.
func managedProcessAttrs() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
}

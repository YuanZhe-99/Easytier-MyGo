//go:build unix

package certmanager

import (
	"os/exec"
	"syscall"
)

// setProcessGroup starts the hook in its own process group and makes a
// timeout kill the whole group, so `sh -c "a; b"` leaves nothing behind.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}

// setNoWindow: console windows exist only on Windows.
func setNoWindow(*exec.Cmd) {}

// rawCmdLine: only cmd.exe needs its command line untouched.
func rawCmdLine(*exec.Cmd, string) {}

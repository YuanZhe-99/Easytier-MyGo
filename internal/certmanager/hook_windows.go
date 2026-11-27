//go:build windows

package certmanager

import (
	"os/exec"
	"strconv"
	"syscall"
)

// setProcessGroup makes a timeout end the hook's whole process tree
// (`cmd /C` starts the real program as a child), falling back to killing
// the process itself.
func setProcessGroup(cmd *exec.Cmd) {
	setNoWindow(cmd)
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		kill := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid))
		setNoWindow(kill)
		if err := kill.Run(); err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
}

// setNoWindow keeps a console program from opening a window when the
// daemon (which has no console) starts it.
func setNoWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= 0x08000000 // CREATE_NO_WINDOW
}

// rawCmdLine hands cmd.exe the line as typed: cmd does not follow the
// quoting rules exec uses for other programs, so "echo x > "a b"" would
// arrive mangled.
func rawCmdLine(cmd *exec.Cmd, line string) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CmdLine = `cmd /S /C "` + line + `"`
}

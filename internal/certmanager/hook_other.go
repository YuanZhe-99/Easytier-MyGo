//go:build !unix && !windows

package certmanager

import "os/exec"

// setProcessGroup keeps exec's default: a timeout kills the hook's
// process, and WaitDelay stops waiting for children holding its output.
func setProcessGroup(cmd *exec.Cmd) {}

// setNoWindow: console windows exist only on Windows.
func setNoWindow(*exec.Cmd) {}

// rawCmdLine: only cmd.exe needs its command line untouched.
func rawCmdLine(*exec.Cmd, string) {}

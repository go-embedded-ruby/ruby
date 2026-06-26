//go:build windows

package vm

import "os/exec"

// runShellCommand runs cmd through the Windows command interpreter (matching
// Ruby's Kernel#backtick / %x{...} on Windows, which shells out via cmd.exe).
// Like MRI, the output is returned verbatim and a non-zero exit status does not
// raise. (Unix uses /bin/sh; see xstr_native.go.)
func (vm *VM) runShellCommand(cmd string) string {
	out, _ := exec.Command("cmd", "/c", cmd).Output()
	return string(out)
}

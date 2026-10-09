//go:build windows

package vm

import "os/exec"

// runShellCommand runs cmd through the Windows command interpreter (matching
// Ruby's Kernel#backtick / %x{...} on Windows, which shells out via cmd.exe).
// Like MRI, the output is returned verbatim and a non-zero exit status does not
// raise. (Unix uses /bin/sh; see xstr_native.go.)
//
// ioBlock for the same reason as the unix build: the wait must not stop every
// other Ruby Thread. This file is the one a POSIX-only fix forgets, so the test
// covering it is written against runShellCommand rather than against /bin/sh.
func (vm *VM) runShellCommand(cmd string) string {
	var out []byte
	ioBlock(vm, func() { out, _ = exec.Command("cmd", "/c", cmd).Output() })
	return string(out)
}

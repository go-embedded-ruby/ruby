//go:build !windows && !wasm

package vm

import "os/exec"

// runShellCommand runs cmd through the system shell (matching Ruby's
// Kernel#backtick / %x{...}) and returns its standard output. Like MRI, the
// output is returned verbatim (including any trailing newline) and a non-zero
// exit status does not raise — the captured output (which may be empty) is
// still returned. (Windows uses cmd.exe; see xstr_windows.go.)
//
// The wait happens inside ioBlock so the rest of the program keeps running.
// Without it every other Ruby Thread stopped until the child exited, which is
// the one thing a program has to be able to rely on NOT happening: the usual
// way to bound a command that might wedge is to watch it from another thread,
// and that thread was asleep for exactly as long as the command it was meant to
// be watching. MRI releases the GVL around a subprocess wait for this reason.
func (vm *VM) runShellCommand(cmd string) string {
	var out []byte
	ioBlock(vm, func() { out, _ = exec.Command("/bin/sh", "-c", cmd).Output() })
	return string(out)
}

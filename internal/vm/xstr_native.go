//go:build !(js && wasm)

package vm

import "os/exec"

// runShellCommand runs cmd through the system shell (matching Ruby's
// Kernel#backtick / %x{...}) and returns its standard output. Like MRI, the
// output is returned verbatim (including any trailing newline) and a non-zero
// exit status does not raise — the captured output (which may be empty) is
// still returned.
func (vm *VM) runShellCommand(cmd string) string {
	out, _ := exec.Command("/bin/sh", "-c", cmd).Output()
	return string(out)
}

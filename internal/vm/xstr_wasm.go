//go:build js && wasm

package vm

// runShellCommand has no meaning under js/wasm (no subprocesses), so `%x{…}` /
// backticks raise NotImplementedError there rather than silently returning "".
func (vm *VM) runShellCommand(cmd string) string {
	raise("NotImplementedError", "`%%x`/backtick command execution is not supported on js/wasm")
	return ""
}

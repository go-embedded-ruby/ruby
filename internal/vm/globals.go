package vm

import (
	"os"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// englishAlias maps the English library's readable global names to the cryptic
// special they alias (require "English"). The aliases are always recognised:
// reading one resolves the underlying special, so $ERROR_INFO and $! name the
// same value. Only names whose target the VM actually models are listed; the
// I/O-formatting specials ($/, $\, $;, …) are not modelled, so their English
// names are left to fall through to the ordinary user-global path (nil until
// set), which matches reading the unset cryptic form.
var englishAlias = map[string]string{
	"$ERROR_INFO":      "$!",
	"$PROGRAM_NAME":    "$0",
	"$PID":             "$$",
	"$PROCESS_ID":      "$$",
	"$LAST_MATCH_INFO": "$~",
	"$MATCH":           "$&",
	"$PREMATCH":        "$`",
	"$POSTMATCH":       "$'",
}

// specialGvar resolves the process/exception special globals and the English
// library's readable aliases for them. It returns (value, true) when it owns the
// name; otherwise the caller continues with match-data and user-global handling.
//
//   - $!  the most recently rescued exception (nil outside a rescue), mirroring
//     the VM's curExc that bare `raise` re-raises.
//   - $0 / $PROGRAM_NAME  the running program's name. Assignable: a stored value
//     wins, otherwise the script path SetScriptPath recorded.
//   - $$  the OS process id.
//
// English aliases ($ERROR_INFO, $MATCH, …) are rewritten to their target here so
// they resolve identically to the cryptic spelling.
func (vm *VM) specialGvar(name string) (object.Value, bool) {
	if target, ok := englishAlias[name]; ok {
		name = target
	}
	switch name {
	case "$!":
		if !object.IsNil(vm.curExc) {
			return vm.curExc, true
		}
		return object.NilV, true
	case "$0", "$PROGRAM_NAME":
		if v, set := vm.globals["$0"]; set {
			return v, true
		}
		return object.NewString(vm.scriptName), true
	case "$$":
		return object.IntValue(int64(os.Getpid())), true
	}
	// $~, $&, $`, $' and $N fall through so the match-data resolver in gvar
	// handles them — englishAlias only rewrote the name to the cryptic form.
	return object.NilVal(), false
}

// setGVar stores a global, normalising the assignable program-name / error-info
// aliases to their target so a write through either spelling is visible through
// both. $! is backed by curExc (not the globals map); the rest are plain slots.
func (vm *VM) setGVar(name string, v object.Value) {
	switch name {
	case "$PROGRAM_NAME":
		name = "$0"
	case "$ERROR_INFO", "$!":
		if _, ok := v.(object.Nil); ok {
			vm.curExc = nil
		} else {
			vm.curExc = v
		}
		return
	}
	vm.globals[name] = v
}

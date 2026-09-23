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
	"$ERROR_POSITION":  "$@",
	"$PROGRAM_NAME":    "$0",
	"$PID":             "$$",
	"$PROCESS_ID":      "$$",
	"$LAST_MATCH_INFO": "$~",
	"$MATCH":           "$&",
	"$PREMATCH":        "$`",
	"$POSTMATCH":       "$'",
}

// gvarStorageAlias maps the alternative spellings MRI binds to ONE storage slot
// onto that slot's canonical name, for reads and writes alike. MRI reaches the
// same effect three different ways — a second rb_define_hooked_variable over the
// same C global ($-0 over rb_rs in io.c v3_4_0, $-F over rb_fs in string.c), an
// explicit rb_alias_variable ($-I for $: in load.c), and a pair of virtual
// variables sharing one getter/setter ($-v/$-w with $VERBOSE and $-d with $DEBUG
// in ruby.c ruby_prog_init, $> with $stdout in io.c) — but what a program can
// observe is identical: a write through either spelling is visible through both.
var gvarStorageAlias = map[string]string{
	"$-0": "$/",
	"$-F": "$;",
	"$:":  "$LOAD_PATH",
	"$-I": "$LOAD_PATH",
	`$"`:  "$LOADED_FEATURES",
	"$-v": "$VERBOSE",
	"$-w": "$VERBOSE",
	"$-d": "$DEBUG",
	"$>":  "$stdout",
}

// readOnlyGvars lists the globals whose assignment raises NameError rather than
// storing. MRI installs rb_gvar_readonly_setter for each of them, either through
// rb_define_readonly_variable, through an explicit setter argument, or — for the
// virtual variables — by passing a NULL setter, which variable.c v3_4_0
// rb_define_hooked_variable turns into rb_gvar_readonly_setter ("if (!setter)
// setter = rb_gvar_readonly_setter"). The message is that setter's:
// rb_name_error(id, "%"PRIsVALUE" is a read-only variable", QUOTE_ID(id)), which
// names the spelling written through, not the slot behind it.
//
// Sources, all at ruby/ruby v3_4_0: eval.c Init_eval ($!); re.c Init_Regexp
// ($&, $`, $', $+); load.c Init_load ($: / $LOAD_PATH / $-I, $" /
// $LOADED_FEATURES); io.c Init_IO ($<, $FILENAME, $*); process.c Init_process
// ($?); ruby.c ruby_prog_init ($-W) and rb_define_readonly_boolean ($-a, $-l,
// $-p).
var readOnlyGvars = map[string]bool{
	"$!":               true,
	"$&":               true,
	"$`":               true,
	"$'":               true,
	"$+":               true,
	"$:":               true,
	"$LOAD_PATH":       true,
	"$-I":              true,
	`$"`:               true,
	"$LOADED_FEATURES": true,
	"$<":               true,
	"$FILENAME":        true,
	"$*":               true,
	"$?":               true,
	"$-a":              true,
	"$-l":              true,
	"$-p":              true,
	"$-W":              true,
}

// canonicalGvar resolves a global's name to the slot that actually stores it:
// first the English library's readable aliases, then the spellings MRI binds to
// a shared slot. Both gvar (read) and setGVar (write) funnel through it, so the
// two sides cannot drift apart.
func canonicalGvar(name string) string {
	if target, ok := englishAlias[name]; ok {
		name = target
	}
	if target, ok := gvarStorageAlias[name]; ok {
		return target
	}
	return name
}

// specialGvar resolves the process/exception special globals and the English
// library's readable aliases for them. It returns (value, true) when it owns the
// name; otherwise the caller continues with match-data and user-global handling.
//
//   - $!  the most recently rescued exception (nil outside a rescue), mirroring
//     the VM's curExc that bare `raise` re-raises.
//   - $@  the rescued exception's backtrace, or nil outside a rescue — eval.c
//     v3_4_0 errat_getter, which is rb_get_backtrace(get_errinfo()).
//   - $0 / $PROGRAM_NAME  the running program's name. Assignable: a stored value
//     wins, otherwise the script path SetScriptPath recorded.
//   - $$  the OS process id.
//   - $VERBOSE / $DEBUG  the warning and debug levels, false until set —
//     ruby.c v3_4_0 gives both a virtual variable over a VM field that starts at
//     Qfalse, so an un-flagged interpreter reads false, NOT nil ($VERBOSE is nil
//     only under -W0).
//   - $-W  the warning level derived from $VERBOSE (nil→0, false→1, true→2),
//     ruby.c opt_W_getter.
//   - $-a / $-l / $-p  the command-line switch flags, false without the switch
//     (ruby.c rb_define_readonly_boolean).
//   - $=  removed in Ruby 1.9: reads as false and warns under the deprecated
//     category (re.c ignorecase_getter).
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
	case "$@":
		if object.IsNil(vm.curExc) {
			return object.NilV, true
		}
		return vm.send(vm.curExc, "backtrace", nil, nil), true
	case "$0", "$PROGRAM_NAME":
		if v, set := vm.globals["$0"]; set {
			return v, true
		}
		return object.NewString(vm.scriptName), true
	case "$$":
		return object.IntValue(int64(os.Getpid())), true
	case "$VERBOSE", "$-v", "$-w", "$DEBUG", "$-d":
		return vm.verboseSlot(canonicalGvar(name)), true
	case "$-W":
		switch v := vm.verboseSlot("$VERBOSE").(type) {
		case object.Bool:
			if bool(v) {
				return object.IntValue(2), true
			}
			return object.IntValue(1), true
		default:
			return object.IntValue(0), true
		}
	case "$-a", "$-l", "$-p":
		return object.Bool(false), true
	case "$=":
		vm.warnDeprecatedGvar("variable $= is no longer effective")
		return object.Bool(false), true
	}
	// $~, $&, $`, $' and $N fall through so the match-data resolver in gvar
	// handles them — englishAlias only rewrote the name to the cryptic form.
	return object.NilVal(), false
}

// verboseSlot reads $VERBOSE / $DEBUG, defaulting an unset slot to false. MRI
// starts both at Qfalse (ruby.c: ruby_verbose and ruby_debug are only set to
// Qnil by -W0 / to Qtrue by -w), so a program that never assigns them still sees
// false — the distinction matters because rb_warn fires whenever $VERBOSE is
// non-nil, and nil is the one value that silences it.
func (vm *VM) verboseSlot(name string) object.Value {
	if v, set := vm.globals[name]; set {
		return v
	}
	return object.Bool(false)
}

// warnDeprecatedGvar emits a special-variable deprecation warning through
// Warning.warn, but only while Warning[:deprecated] is enabled — error.c v3_4_0
// deprecation_warning_enabled() requires both a non-nil $VERBOSE and the
// category, and the category is off by default, so an ordinary run is silent.
func (vm *VM) warnDeprecatedGvar(msg string) {
	if object.IsNil(vm.verboseSlot("$VERBOSE")) {
		return
	}
	w := vm.consts["Warning"]
	if w == nil || !vm.send(w, "[]", []object.Value{object.Symbol("deprecated")}, nil).Truthy() {
		return
	}
	vm.send(w, "warn", []object.Value{object.NewString("warning: " + msg + "\n")}, nil)
}

// setGVar stores a global, applying the checks MRI's setter for that name
// applies: a read-only variable raises NameError, the I/O-formatting specials
// take only a String (or, for $;, a Regexp), $. takes anything #to_int accepts,
// the standard streams must answer #write, $~ takes only a MatchData, and $@
// writes through to the rescued exception's backtrace. Anything else is a plain
// slot, stored under its canonical name so the aliased spellings agree.
func (vm *VM) setGVar(name string, v object.Value) {
	vm.storeGVar(name, v)
	// variable.c v3_4_0 rb_gvar_set_entry runs the entry's trace list after its
	// setter, so a hook sees the value the setter accepted and never runs at all
	// when the setter raised.
	vm.fireGvarTraces(name, v)
}

// storeGVar is setGVar without the Kernel#trace_var hooks: the checks and the
// store itself.
func (vm *VM) storeGVar(name string, v object.Value) {
	target := name
	if t, ok := englishAlias[name]; ok {
		target = t
	}
	if readOnlyGvars[target] {
		// Read-only-ness follows the alias, but the message names the spelling
		// assigned THROUGH — MRI's rb_gvar_readonly_setter quotes the id it was
		// called with, so `require "English"; $ERROR_INFO = nil` reports
		// "$ERROR_INFO is a read-only variable", not "$!".
		vm.raiseNameError(name+" is a read-only variable", name)
	}
	name = target
	switch name {
	case "$@":
		// eval.c v3_4_0 errat_setter: no rescued exception is an ArgumentError,
		// otherwise the value goes through Exception#set_backtrace, which applies
		// rb_check_backtrace's type rules.
		if object.IsNil(vm.curExc) {
			raise("ArgumentError", "$! not set")
		}
		vm.send(vm.curExc, "set_backtrace", []object.Value{v}, nil)
		return
	case "$~":
		// re.c v3_4_0 match_setter: Check_Type(val, T_MATCH) unless nil.
		if !object.IsNil(v) {
			if _, ok := v.(*MatchData); !ok {
				raise("TypeError", "wrong argument type %s (expected MatchData)", classNameOf(v))
			}
			vm.lastMatch = v
			return
		}
		vm.lastMatch = object.NilV
		return
	case "$/", "$-0", `$\`, "$,":
		v = vm.checkStrGvar(name, v)
	case "$;", "$-F":
		v = vm.checkFieldSepGvar(name, v)
	case "$.":
		// io.c v3_4_0 argf_lineno_setter: NUM2INT, so a Float truncates and a
		// non-Integer goes through #to_int or raises TypeError.
		v = object.IntValue(vm.toIntCoerce(v))
	case "$stdout", "$stderr", "$>":
		vm.mustRespondToWrite(name, v)
	case "$VERBOSE", "$-v", "$-w":
		// ruby.c v3_4_0 verbose_setter: RTEST(val) ? Qtrue : val — a truthy value
		// becomes true, false stays false and nil stays nil (the silent level).
		if truthyValue(v) {
			v = object.Bool(true)
		}
	case "$0", "$PROGRAM_NAME":
		// ruby.c v3_4_0 set_arg0 → ruby_setproctitle, whose first act is
		// StringValue(val): a non-String that cannot be coerced is a TypeError.
		v = object.NewString(vm.coerceToString(v))
	}
	vm.globals[canonicalGvar(name)] = v
}

// checkStrGvar applies io.c v3_4_0 deprecated_str_setter to $/ $-0 $\ $,: the
// value must be nil or a String (rb_str_setter in string.c raises "value of $x
// must be String" for anything else, WITHOUT trying #to_str), and a non-nil
// assignment warns that the variable is deprecated.
func (vm *VM) checkStrGvar(name string, v object.Value) object.Value {
	if object.IsNil(v) {
		return object.NilV
	}
	if _, ok := v.(*object.String); !ok {
		raise("TypeError", "value of %s must be String", name)
	}
	vm.warnDeprecatedGvar("non-nil '" + name + "' is deprecated")
	return v
}

// checkFieldSepGvar applies string.c v3_4_0 rb_fs_setter to $; / $-F: nil, a
// String or a Regexp is taken as is, anything else is offered #to_str
// (rb_fs_check's rb_check_string_type) before the TypeError, and a non-nil
// assignment warns that the variable is deprecated.
func (vm *VM) checkFieldSepGvar(name string, v object.Value) object.Value {
	if object.IsNil(v) {
		return object.NilV
	}
	switch v.(type) {
	case *object.String, *Regexp:
	default:
		if vm.respondsToDynamic(v, "to_str") {
			if s, ok := vm.send(v, "to_str", nil, nil).(*object.String); ok {
				v = s
				break
			}
		}
		raise("TypeError", "value of %s must be String or Regexp", name)
	}
	vm.warnDeprecatedGvar("non-nil '" + name + "' is deprecated")
	return v
}

// mustRespondToWrite applies io.c v3_4_0 must_respond_to, the check
// stdout_setter/stderr_setter run before rebinding a standard stream: the new
// value must answer #write, or the assignment is a TypeError naming the global,
// the method and the offending class.
func (vm *VM) mustRespondToWrite(name string, v object.Value) {
	if name == "$>" {
		name = "$stdout"
	}
	if !vm.respondsToDynamic(v, "write") {
		// rb_obj_class, not rb_obj_classname: nil reports as NilClass here.
		raise("TypeError", "%s must have write method, %s given", name, vm.classOf(v).name)
	}
}

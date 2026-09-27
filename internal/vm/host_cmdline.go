// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "github.com/go-embedded-ruby/ruby/internal/object"

// This file is the HOST side of MRI's command line: the two things ruby.c does to
// a fresh interpreter before it runs the program, which no Ruby-level construct
// can do for itself. The CLI front-end (cmd/rbgo) is the only caller; an
// embedding host may use them for the same purpose.

// SetARGV replaces ARGV's contents with args, which is what ruby.c ruby_set_argv
// does (`rb_ary_clear(rb_argv)` then a push per argument).
//
// It mutates the EXISTING Array in place rather than installing a new one,
// because ARGV (the constant) and $* are one object here as in MRI: replacing the
// constant would leave $* pointing at the old, empty Array, and ARGF — which
// shifts filenames off the live ARGV (argf.go) — would then draw from whichever
// of the two it happened to hold.
//
// Each element is frozen, matching ruby_set_argv's OBJ_FREEZE(arg): `ARGV.first
// << "x"` raises FrozenError in MRI, and a test asserting frozen? on ARGV's
// members would otherwise pass for the wrong reason.
func (vm *VM) SetARGV(args []string) {
	argv, ok := vm.consts["ARGV"].(*object.Array)
	if !ok {
		// A host that replaced the constant with something else; install a fresh
		// Array so $* and ARGV are at least the same object again.
		argv = object.NewArray()
		vm.consts["ARGV"] = argv
		vm.globals["$*"] = argv
	}
	elems := argv.Elems[:0]
	for _, a := range args {
		elems = append(elems, object.NewFrozenStringView(a))
	}
	argv.Elems = elems
}

// SetWarningLevel puts $VERBOSE into the state -W<level> selects, which is the
// numeric branch of ruby.c proc_W_option (v3_4_0 ruby.c:1250-1268, moved into
// proc_W_option by ruby_4_0):
//
//	0   ruby_verbose = Qnil    every rb_warn and rb_warning is silent
//	1   ruby_verbose = Qfalse  rb_warn speaks, rb_warning stays quiet (the default)
//	>=2 ruby_verbose = Qtrue   both speak (this is what -w and -W2 select)
//
// A negative level cannot come from proc_W_option (scan_oct reads one octal
// digit) and is treated as 0, the silent end of the same scale.
//
// It writes the slot directly rather than through the $VERBOSE setter because
// there is no Ruby frame to raise in yet; the three values it writes are exactly
// the three verbose_setter can produce, so no second representation is created
// (see warnEnabled/warningEnabled in globals.go).
func (vm *VM) SetWarningLevel(level int) {
	switch {
	case level <= 0:
		vm.globals["$VERBOSE"] = object.NilVal()
	case level == 1:
		vm.globals["$VERBOSE"] = object.Bool(false)
	default:
		vm.globals["$VERBOSE"] = object.Bool(true)
	}
}

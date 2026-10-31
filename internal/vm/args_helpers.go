// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "github.com/go-embedded-ruby/ruby/internal/object"

// The helpers below are used by core classes (IO, Rational, Exception,
// Encoding::Converter, Module, Hash) as much as by gem bindings. Each used to
// live in the first gem binding that needed it (i18n, actionpack, activestorage,
// activesupport, aasm); they moved here when the rbgo_core build stopped
// compiling those files, unchanged.

// lastHash returns the trailing Hash argument (the keyword options) when the
// last positional argument is a Hash, matching how rbgo passes keyword args.
func lastHash(args []object.Value) (*object.Hash, bool) {
	if len(args) == 0 {
		return nil, false
	}
	h, ok := args[len(args)-1].(*object.Hash)
	return h, ok
}

// lastHashOrNil returns the trailing keyword-options Hash of an argument list,
// or nil when the last argument is not a Hash.
func lastHashOrNil(args []object.Value) *object.Hash {
	if h, ok := lastHash(args); ok {
		return h
	}
	return nil
}

// argAt returns the i-th positional argument, or NilV when absent.
func argAt(args []object.Value, i int) object.Value {
	if i < len(args) {
		return args[i]
	}
	return object.NilV
}

// hashArgOrRaise returns v as a *Hash or raises the "wrong argument type X
// (expected Hash)" TypeError that Hash.ruby2_keywords_hash(?) reports.
func hashArgOrRaise(vm *VM, v object.Value) *object.Hash {
	h, ok := v.(*object.Hash)
	if !ok {
		raise("TypeError", "wrong argument type %s (expected Hash)", vm.classOf(v).name)
	}
	return h
}

// buildException constructs a Ruby exception object of the named class (falling
// back to RuntimeError when unknown) with the given message.
func (vm *VM) buildException(class, msg string) object.Value {
	cls, ok := vm.consts[class].(*RClass)
	if !ok {
		cls = vm.consts["RuntimeError"].(*RClass)
	}
	return vm.send(cls, "new", []object.Value{object.NewString(msg)}, nil)
}

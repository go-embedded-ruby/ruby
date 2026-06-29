// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/object"
	libsw "github.com/go-ruby-shellwords/shellwords"
)

// This file is the thin binding between rbgo's Ruby object graph (object.Value)
// and github.com/go-ruby-shellwords/shellwords — an MRI-4.0.5-byte-exact
// reimplementation of Ruby's "shellwords" standard library (require
// "shellwords"), a sibling of go-ruby-uri / go-ruby-csv / go-ruby-strscan. All
// of the Bourne-shell word grammar lives in that library: the splitter
// (Shellwords.shellsplit), the per-argument escaper (Shellwords.shellescape)
// and the array joiner (Shellwords.shelljoin), including the exact unmatched-
// quote / NUL ArgumentError messages. rbgo only re-expresses that surface on
// the Ruby side and re-raises the library's *ArgumentError as Ruby's built-in
// ArgumentError with the identical message.
//
// This is an additive binding (a new require "shellwords" feature). Like MRI,
// nothing it installs exists before the require: the Shellwords module, and the
// String#shellsplit / String#shellescape / Array#shelljoin core extensions, are
// all created on the first `require "shellwords"` (registerShellwords records a
// feature hook that doRequire fires once). Before then `defined?(Shellwords)`
// is nil and the String/Array methods do not respond — matching MRI exactly.

// registerShellwords records the require "shellwords" feature hook. It installs
// nothing eagerly: the hook (run once by doRequire on the first require) creates
// the Shellwords module with its module functions and adds the String / Array
// core extensions, mirroring MRI where lib/shellwords.rb defines them only when
// loaded. It runs during VM construction after String / Array exist so the hook
// can close over them.
func (vm *VM) registerShellwords() {
	if vm.featureHooks == nil {
		vm.featureHooks = map[string]func(){}
	}
	vm.featureHooks["shellwords"] = vm.installShellwords
}

// installShellwords builds the Shellwords module and the core extensions. It is
// the body MRI's lib/shellwords.rb runs on load: the module functions
// shellsplit / shellwords / shellescape / shelljoin (with the split / escape /
// join aliases), then String#shellsplit / String#shellescape and
// Array#shelljoin defined directly on the core classes.
func (vm *VM) installShellwords() {
	mod := newClass("Shellwords", nil)
	mod.isModule = true
	vm.consts["Shellwords"] = mod

	// Module functions. MRI's `module_function` makes each both a module method
	// (Shellwords.shellsplit) and a private instance method on includers; rbgo
	// installs them as module methods (smethods), which is the form every call
	// site and the tests exercise.
	sm := func(name string, fn NativeFn) {
		mod.smethods[name] = &Method{name: name, owner: mod, native: fn}
	}

	splitFn := func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return shellSplit(strArg(args[0]))
	}
	// Shellwords.shellsplit / .shellwords / .split — the splitter (alias trio).
	sm("shellsplit", splitFn)
	sm("shellwords", splitFn)
	sm("split", splitFn)

	escapeFn := func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(libsw.Escape(strArg(args[0])))
	}
	// Shellwords.shellescape / .escape — the per-argument escaper.
	sm("shellescape", escapeFn)
	sm("escape", escapeFn)

	joinFn := func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(libsw.Join(vm.shellWords(args[0])))
	}
	// Shellwords.shelljoin / .join — the array joiner.
	sm("shelljoin", joinFn)
	sm("join", joinFn)

	// Core extensions added on require, MRI-style: String#shellsplit /
	// #shellescape and Array#shelljoin operate on the receiver.
	vm.cString.define("shellsplit", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return shellSplit(strArg(self))
	})
	vm.cString.define("shellescape", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(libsw.Escape(strArg(self)))
	})
	vm.cArray.define("shelljoin", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(libsw.Join(vm.shellWords(self)))
	})
}

// shellSplit runs the library splitter and lifts the result into a Ruby Array of
// Strings, re-raising the library's *ArgumentError (unmatched quote / NUL) as
// Ruby's built-in ArgumentError with the identical message.
func shellSplit(line string) object.Value {
	words, err := libsw.Split(line)
	if err != nil {
		raise("ArgumentError", "%s", err.Error())
	}
	out := make([]object.Value, len(words))
	for i, w := range words {
		out[i] = object.NewString(w)
	}
	return &object.Array{Elems: out}
}

// shellWords coerces an Array receiver/argument to a []string for Join. Each
// element is taken as a String directly, or coerced via Ruby #to_s otherwise —
// MRI's Array#shelljoin escapes `s.to_s` for every element, so [1, nil].shelljoin
// stringifies its members (1 -> "1", nil -> ""). A non-Array raises TypeError,
// mirroring Shellwords.shelljoin's argument check.
func (vm *VM) shellWords(v object.Value) []string {
	arr, ok := v.(*object.Array)
	if !ok {
		raise("TypeError", "no implicit conversion of %s into Array", classNameOf(v))
		return nil
	}
	words := make([]string, len(arr.Elems))
	for i, e := range arr.Elems {
		if s, ok := e.(*object.String); ok {
			words[i] = s.Str()
		} else {
			words[i] = strArg(vm.send(e, "to_s", nil, nil))
		}
	}
	return words
}

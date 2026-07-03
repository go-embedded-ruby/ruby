// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerChronic installs the Chronic module (require "chronic"):
// Chronic.parse(str, options). The natural-language grammar lives in the
// github.com/go-ruby-chronic/chronic library; this module is the thin wiring
// that maps rbgo's arguments and options hash to the library's Parse/ParseSpan
// and the resulting Go time.Time / *Span back into rbgo's Time / range shapes
// (see chronic_bind.go). A Chronic::Span is returned as a two-element [begin,
// end] Array of Time, mirroring how the gem's Span exposes its bounds.
func (vm *VM) registerChronic() {
	mod := newClass("Chronic", nil)
	mod.isModule = true
	vm.consts["Chronic"] = mod

	def := func(name string, fn NativeFn) { mod.smethods[name] = &Method{name: name, owner: mod, native: fn} }

	// Chronic.parse(text, options = {}) parses a natural-language phrase to a Ruby
	// Time (or nil when nothing matched). The options hash maps now:, context:,
	// endian_precedence: and guess: onto the library's Options.
	def("parse", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		return chronicParse(strArg(args[0]), chronicOptsArg(args[1:]))
	})
}

// chronicOptsArg returns the trailing options Hash of Chronic.parse, or nil when
// no hash was supplied.
func chronicOptsArg(rest []object.Value) *object.Hash {
	if len(rest) == 0 {
		return nil
	}
	h, _ := object.KindOK[*object.Hash](rest[len(rest)-1])
	return h
}

// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "github.com/go-embedded-ruby/ruby/internal/object"

// This file installs the DeepMerge module and the Hash#deep_merge! core extension
// (require "deep_merge"). The recursive merge algorithm — the knockout-prefix,
// array unpack / merge / sort, and preserve-unmergeables options of the deep_merge
// gem — lives in github.com/go-ruby-deep-merge/deep-merge; rbgo owns only the
// object-model bridge: DeepMerge.deep_merge!/deep_merge, Hash#deep_merge!, and the
// Ruby⇄Go value conversion. Hash key identity (symbol vs string) is preserved
// across the round trip; a bad option combination (e.g. a knockout prefix without
// overwrite) raises DeepMerge::InvalidParameter, as the gem does.
//
// The plain, non-bang Hash#deep_merge is intentionally left to the ActiveSupport
// core extension (installed at boot): its recursive merge is compatible, and it
// preserves MRI key order. The options-aware merge is reached here through
// Hash#deep_merge! or the DeepMerge.deep_merge / deep_merge! module methods.

// registerDeepMerge installs DeepMerge.deep_merge!(source, dest, opts={}) and its
// non-bang form, plus Hash#deep_merge!(source, opts={}), matching the deep_merge
// gem (require "deep_merge").
func (vm *VM) registerDeepMerge() {
	mod := newClass("DeepMerge", nil)
	mod.isModule = true
	vm.consts["DeepMerge"] = mod

	std := vm.consts["StandardError"].(*RClass)
	invalid := newClass("DeepMerge::InvalidParameter", std)
	mod.consts["InvalidParameter"] = invalid
	vm.consts["DeepMerge::InvalidParameter"] = invalid

	// DeepMerge.deep_merge!(source, dest, opts={}) — the gem's module form takes
	// source first, dest second; the bang form mutates dest in place.
	mod.smethods["deep_merge!"] = &Method{name: "deep_merge!", owner: mod, native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return deepMergeModule(args, true)
	}}
	mod.smethods["deep_merge"] = &Method{name: "deep_merge", owner: mod, native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return deepMergeModule(args, false)
	}}

	// Hash#deep_merge!(source, opts={}) — the receiver is dest, mutated in place.
	vm.cHash.define("deep_merge!", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return deepMergeHashBang(self, args)
	})
}

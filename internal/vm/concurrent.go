// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerConcurrent installs a minimal concurrent-ruby shell (require
// "concurrent"). Under this VM's single-threaded emulated GVL, the concurrent
// collections behave exactly like the built-in ones, so Concurrent::Hash /
// Concurrent::Map / Concurrent::Array alias the core classes (preserving the
// full Hash/Array API Puppet relies on, including Hash.new with a default
// block), and Concurrent::ThreadLocalVar is a real single-slot holder
// (value / value=) that Puppet subclasses for its per-thread context.
func (vm *VM) registerConcurrent() {
	mod := newClass("Concurrent", nil)
	mod.isModule = true
	vm.consts["Concurrent"] = mod

	// The thread-safe collections degrade to the core collections here.
	mod.consts["Hash"] = vm.cHash
	mod.consts["Map"] = vm.cHash
	mod.consts["Array"] = vm.cArray

	// Concurrent::ThreadLocalVar(default): #value reads the current slot (the
	// default until first written), #value= writes it. A single emulated thread
	// makes one shared slot correct for the common path.
	tlv := newClass("Concurrent::ThreadLocalVar", vm.cObject)
	mod.consts["ThreadLocalVar"] = tlv
	tlv.smethods["new"] = &Method{name: "new", owner: tlv,
		native: func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
			o := &RObject{class: tlv, ivars: map[string]object.Value{}}
			vm.send(o, "initialize", args, blk)
			return o
		}}
	// initialize(default = nil) { default_block }: store either an eager default
	// value or a lazy default block (called on first read per the gem's API).
	// "@set" tracks whether a value has been written yet, so a nil default still
	// triggers the block exactly once.
	tlv.define("initialize", func(_ *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		if len(args) > 0 {
			setIvar(self, "@value", args[0])
			setIvar(self, "@set", object.True)
		} else if blk != nil {
			setIvar(self, "@default_block", blk)
			setIvar(self, "@set", object.False)
		} else {
			setIvar(self, "@value", object.NilV)
			setIvar(self, "@set", object.True)
		}
		return object.NilV
	})
	tlv.define("value", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if !getIvar(self, "@set").Truthy() {
			// First read with an unset value and a default block: evaluate the block,
			// memoise the result, and clear the lazy flag (the block runs once).
			if blk, ok := object.KindOK[*Proc](getIvar(self, "@default_block")); ok {
				v := vm.callBlock(blk, nil)
				setIvar(self, "@value", v)
				setIvar(self, "@set", object.True)
				return v
			}
		}
		return getIvar(self, "@value")
	})
	tlv.define("value=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		setIvar(self, "@value", args[0])
		setIvar(self, "@set", object.True)
		return args[0]
	})
}

// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "github.com/go-embedded-ruby/ruby/internal/object"

// registerObjectSpace installs a minimal ObjectSpace module. The VM relies on
// Go's garbage collector and does not expose the live object set, so the
// reflective walkers (each_object / count_objects) report empty and the
// collection trigger is a no-op. The finalizer API is provided because library
// code (e.g. concurrent-ruby's ThreadLocalVar) registers finalizers
// unconditionally at construction; finalizers never actually run (Go reclaims
// memory itself), but define_finalizer must accept the call and return MRI's
// [0, callable] shape so construction proceeds.
func (vm *VM) registerObjectSpace() {
	mod := newClass("ObjectSpace", nil)
	mod.isModule = true
	vm.consts["ObjectSpace"] = mod
	def := func(name string, fn NativeFn) { mod.smethods[name] = &Method{name: name, owner: mod, native: fn} }

	// define_finalizer(obj, callable = block): MRI returns [object_id_flag, callable].
	// We return [0, callable]; the finalizer is recorded nowhere since the Go GC
	// owns reclamation.
	def("define_finalizer", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		var callable object.Value = object.NilV
		if len(args) >= 2 {
			callable = args[1]
		} else if blk != nil {
			callable = blk
		}
		return &object.Array{Elems: []object.Value{object.IntValue(0), callable}}
	})
	// undefine_finalizer(obj): a no-op that returns the object, as in MRI.
	def("undefine_finalizer", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return args[0]
	})
	// each_object([class]) yields nothing (the live set is not exposed) and
	// returns 0 — the count of objects iterated. Without a block it would return
	// an Enumerator in MRI; here it simply reports 0.
	def("each_object", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(0)
	})
	// garbage_collect / start: trigger a collection — a no-op, returning nil.
	gc := func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value { return object.NilV }
	def("garbage_collect", gc)
	def("start", gc)
	// count_objects returns an empty Hash (the live set is not tracked).
	def("count_objects", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewHash()
	})
}

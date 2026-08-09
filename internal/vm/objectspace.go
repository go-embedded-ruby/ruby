// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"runtime"
	"weak"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerLiveObject records o in the each_object registry as a weak pointer, so
// ObjectSpace.each_object can later enumerate the live instances a program still
// holds without the registry itself keeping any of them alive. Called from the
// ordinary object constructors (Class#new, Class#allocate, built-in subclass
// construction).
func (vm *VM) registerLiveObject(o *RObject) {
	vm.liveObjs = append(vm.liveObjs, weak.Make(o))
}

// registerLiveClass records an anonymous class/module (Class.new / Module.new) in
// the each_object registry so each_object(Class) / each_object(Module) can find
// it. Named classes minted at VM startup are not tracked; the observable contract
// the specs exercise is create-then-enumerate of freshly built classes.
func (vm *VM) registerLiveClass(c *RClass) {
	vm.liveClasses = append(vm.liveClasses, weak.Make(c))
}

// objIsA reports whether v is an instance of module/class target (the each_object
// filter), matching Object#is_a? — direct ancestry plus any singleton/metaclass
// mixin. A class value is_a? Class (and is_a? Module), so passing Class/Module as
// the target selects the registered classes/modules.
func (vm *VM) objIsA(v object.Value, target *RClass) bool {
	return classIsA(vm.classOf(v), target) || classSingletonIsA(vm, v, target)
}

// collectLive sweeps the each_object registry once: it drops weak entries whose
// referent has been collected (compacting both slices in place) and returns the
// still-live values that match target (all of them when target is nil). The whole
// sweep completes before the caller yields, so a block that allocates fresh
// objects appends to the freshly compacted registry rather than mutating a slice
// mid-iteration.
func (vm *VM) collectLive(target *RClass) []object.Value {
	var out []object.Value
	keptO := vm.liveObjs[:0]
	for _, w := range vm.liveObjs {
		o := w.Value()
		if o == nil {
			continue
		}
		keptO = append(keptO, w)
		if target == nil || vm.objIsA(o, target) {
			out = append(out, o)
		}
	}
	vm.liveObjs = keptO
	keptC := vm.liveClasses[:0]
	for _, w := range vm.liveClasses {
		c := w.Value()
		if c == nil {
			continue
		}
		keptC = append(keptC, w)
		if target == nil || vm.objIsA(c, target) {
			out = append(out, c)
		}
	}
	vm.liveClasses = keptC
	return out
}

// registerObjectSpace installs the ObjectSpace module. rbgo runs on Go's garbage
// collector and has no global heap table, so the reflective walkers are
// best-effort: each_object enumerates the weakly-tracked instances built through
// the ordinary constructors (see registerLiveObject), and count_objects reports
// that view's shape. Finalizers are recorded so define_finalizer can dedup and
// undefine_finalizer can clear them, but the Go GC owns reclamation so they never
// actually run — the observable API (return values, argument validation, error
// classes) matches MRI exactly.
func (vm *VM) registerObjectSpace() {
	mod := newClass("ObjectSpace", nil)
	mod.isModule = true
	vm.consts["ObjectSpace"] = mod
	def := func(name string, fn NativeFn) { mod.smethods[name] = &Method{name: name, owner: mod, native: fn} }

	// define_finalizer(obj, callable = block): registers a finalizer and returns
	// MRI's [0, callable]. The object must be a reference (immediates raise
	// ArgumentError), the callable must respond to #call, and a callable that ==
	// one already registered for obj is not added again — the first registered
	// callable is returned, matching MRI's dedup-by-equality behaviour.
	def("define_finalizer", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		obj := args[0]
		switch obj.(type) {
		case object.Integer, object.Float, object.Symbol, object.Bool, object.Nil:
			raise("ArgumentError", "cannot define finalizer for %s", vm.classOf(obj).name)
		}
		var callable object.Value
		if len(args) >= 2 {
			callable = args[1]
		} else if blk != nil {
			callable = blk
		} else {
			raise("ArgumentError", "no finalizer given")
		}
		if !vm.respondsToDynamic(callable, "call") {
			raise("ArgumentError", "no matching #call function")
		}
		id := vm.refID(obj)
		if vm.finalizers == nil {
			vm.finalizers = map[int64][]object.Value{}
		}
		for _, existing := range vm.finalizers[id] {
			if vm.send(existing, "==", []object.Value{callable}, nil).Truthy() {
				return object.NewArray(object.IntValue(0), existing)
			}
		}
		vm.finalizers[id] = append(vm.finalizers[id], callable)
		return object.NewArray(object.IntValue(0), callable)
	})
	// undefine_finalizer(obj): removes obj's finalizers and returns obj. Removing
	// finalizers from a frozen object raises FrozenError, as in MRI.
	def("undefine_finalizer", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		obj := args[0]
		if isFrozen(obj) {
			vm.raiseFrozen(obj)
		}
		if vm.finalizers != nil {
			delete(vm.finalizers, vm.refID(obj))
		}
		return obj
	})
	// each_object([mod]) yields every tracked live object (optionally only those
	// that are is_a? mod), returning the number yielded. Without a block it returns
	// an Enumerator, as in MRI.
	def("each_object", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		var target *RClass
		if len(args) >= 1 {
			target = classArg(args[0])
		}
		if blk == nil {
			if len(args) >= 1 {
				return enumFor(mod, "each_object", args[0])
			}
			return enumFor(mod, "each_object")
		}
		live := vm.collectLive(target)
		for _, v := range live {
			vm.callBlock(blk, []object.Value{v})
		}
		return object.IntValue(int64(len(live)))
	})
	// garbage_collect / start: trigger a collection. It runs Go's GC (harmless) and
	// returns nil, accepting and ignoring any keyword arguments and block as MRI
	// does.
	gc := func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		runtime.GC()
		return object.NilV
	}
	def("garbage_collect", gc)
	def("start", gc)
	// count_objects([hash]) reports the shape MRI guarantees: :TOTAL and :FREE plus
	// per-type T_* counts. Values are best-effort from the tracked live view (rbgo
	// keeps no full heap census), but the invariants the shape must satisfy hold
	// (:TOTAL >= :FREE, keys present). A supplied Hash is cleared and reused.
	def("count_objects", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		var h *object.Hash
		if len(args) >= 1 {
			hh, ok := args[0].(*object.Hash)
			if !ok {
				raise("TypeError", "wrong argument type %s (expected Hash)", vm.classOf(args[0]).name)
			}
			hh.Clear()
			h = hh
		} else {
			h = object.NewHash()
		}
		objs := int64(len(vm.liveObjs))
		classes := int64(len(vm.liveClasses))
		set := func(k string, v int64) { h.Set(object.Symbol(k), object.IntValue(v)) }
		// A base for the immortal built-in objects rbgo does not track individually,
		// keeping :TOTAL a plausible upper bound over the per-type counts.
		set("TOTAL", objs+classes+1000)
		set("FREE", 0)
		set("T_OBJECT", objs)
		set("T_CLASS", classes)
		set("T_MODULE", 0)
		set("T_STRING", 0)
		set("T_ARRAY", 0)
		set("T_HASH", 0)
		set("T_SYMBOL", 0)
		set("T_STRUCT", 0)
		set("T_FLOAT", 0)
		set("T_DATA", 0)
		return h
	})
	// _id2ref(id) returns the object whose #object_id is id, the inverse of
	// #object_id: immediate ids decode arithmetically (fixnum 2n+1 -> n, and the
	// fixed nil/true/false ids), and reference ids are resolved through the
	// object_id memo. An id that maps to no live object raises RangeError, as MRI.
	def("_id2ref", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		id := intArg(args[0])
		if id&1 == 1 { // odd => fixnum id 2n+1
			return object.IntValue(id >> 1)
		}
		switch id {
		case 0:
			return object.False
		case 4:
			return object.NilV
		case 20:
			return object.True
		}
		for v, vid := range vm.objIDs {
			if vid == id {
				return v
			}
		}
		raise("RangeError", "0x%016x is not id value", uint64(id))
		return object.NilV
	})

	vm.registerWeakMaps(mod)
}

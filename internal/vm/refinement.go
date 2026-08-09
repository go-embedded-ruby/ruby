// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "github.com/go-embedded-ruby/ruby/internal/object"

// Refinements (Module#refine / using / Module#using, the Refinement class).
//
// A refinement-holder module M records, per refined class C, one anonymous
// Refinement module R (M.refinements[C]) that accumulates the method definitions
// written in M.refine(C) { ... }. `using M` in a lexical scope activates every
// refinement reachable from M (M and its ancestors) for the remainder of that
// scope; a send whose receiver is an instance of a refined class then resolves
// the refinement's method ahead of the class's own.
//
// Scope is keyed on the frame's definee (the enclosing class/module body), which
// equals `self` for a class/module body and for the block of Module.new/Class.new
// — so activation neither leaks to a sibling scope nor to an unrelated file. The
// whole feature is gated by vm.anyRefinements, so a program that never calls
// refine keeps the unmodified dispatch fast path.

func (vm *VM) registerRefinements() {
	// Refinement is a subclass of Module: the anonymous modules #refine mints
	// report their class as Refinement and answer #target.
	vm.cRefinement = newClass("Refinement", vm.cModule)
	vm.consts["Refinement"] = vm.cRefinement

	vm.cModule.define("refine", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		target, ok := args[0].(*RClass)
		if !ok {
			raise("TypeError", "wrong argument type %s (expected Class or Module)", vm.classOf(args[0]).name)
		}
		if blk == nil {
			raise("ArgumentError", "no block given")
		}
		holder := self.(*RClass)
		r := holder.refinementFor(target)
		vm.anyRefinements = true
		// Run the block with the refinement module as both self and definition
		// target, so its `def`s land on R and `self` inside the block is R.
		vm.classEval(r, blk, nil)
		return r
	})

	// Refinement#target reports the class/module the receiver refines. (MRI 3.4+
	// dropped Refinement#refined_class; we deliberately do not define it.)
	vm.cRefinement.define("target", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self.(*RClass).refinedClass
	})

	// Module#refinements returns this module's own refinement modules (not those
	// of included modules).
	vm.cModule.define("refinements", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		holder := self.(*RClass)
		arr := object.NewArray()
		for _, r := range holder.refinements {
			arr.Elems = append(arr.Elems, r)
		}
		return arr
	})

	usingFn := func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		mod, ok := args[0].(*RClass)
		if !ok || !mod.isModule {
			raise("TypeError", "wrong argument type %s (expected Module)", vm.classOf(args[0]).name)
		}
		if vm.inMethodScope() {
			raise("RuntimeError", "Module#using is not permitted in methods")
		}
		scope := vm.refinementScope(self)
		vm.anyRefinements = true
		// Record in call order, de-duplicating so a repeated using is a no-op for
		// used_modules ordering while still being harmless.
		for _, m := range scope.usedModules {
			if m == mod {
				return self
			}
		}
		scope.usedModules = append(scope.usedModules, mod)
		return self
	}
	// `using` is available both at the top level (a private Kernel method on main)
	// and as Module#using inside a module/class body.
	vm.cObject.methods["using"] = &Method{name: "using", native: usingFn, owner: vm.cObject, vis: visPrivate}
	vm.cModule.define("using", usingFn)

	// Module#used_modules lists the modules activated by `using` reachable from
	// this scope. Called as an instance method, self is the scope module.
	vm.cModule.define("used_modules", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		arr := object.NewArray()
		seen := map[*RClass]bool{}
		for c := self.(*RClass); c != nil; c = refinementParent(c) {
			for _, m := range c.usedModules {
				if !seen[m] {
					seen[m] = true
					arr.Elems = append(arr.Elems, m)
				}
			}
		}
		return arr
	})
}

// refinementFor returns holder's anonymous Refinement module for target,
// creating and registering it on first use so repeated refine(target) accumulate
// into one module (MRI: "uses the same anonymous module for future refines").
func (holder *RClass) refinementFor(target *RClass) *RClass {
	if r, ok := holder.refinements[target]; ok {
		return r
	}
	r := newClass("", nil)
	r.isModule = true
	r.isRefinement = true
	r.refinedClass = target
	r.refHolder = holder
	r.defaultVis = visPublic
	if holder.refinements == nil {
		holder.refinements = map[*RClass]*RClass{}
	}
	holder.refinements[target] = r
	return r
}

// refinementScope maps the `using` receiver to the lexical scope that records the
// activation: the module/class itself for a body scope, else the top level
// (Object), since at the top level self is the main object.
func (vm *VM) refinementScope(self object.Value) *RClass {
	if c, ok := self.(*RClass); ok {
		return c
	}
	return vm.cObject
}

// inMethodScope reports whether the innermost interpreted frame (the caller of a
// native such as `using`) is a method body rather than a class/module/top-level
// body or block — those push a non-empty frame name, methods a non-empty one.
func (vm *VM) inMethodScope() bool {
	n := len(vm.frameNames)
	return n > 0 && vm.frameNames[n-1] != ""
}

// refinementParent gives the next enclosing scope to consult for active
// refinements: a metaclass delegates to the class it is the metaclass of (so a
// `def self.m` body sees the module's `using`), otherwise the lexical parent.
func refinementParent(c *RClass) *RClass {
	if c.metaOf != nil {
		return c.metaOf
	}
	return c.lexParent
}

// activeRefinements collects the Refinement modules active in scope, highest
// priority first: inner scopes before outer, later `using` before earlier, and a
// refinement body sees all of its holder's sibling refinements. The result drives
// refinedMethod; an empty slice means dispatch is unchanged.
func (vm *VM) activeRefinements(scope *RClass) []*RClass {
	var out []*RClass
	seen := map[*RClass]bool{}
	add := func(r *RClass) {
		if !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	for c := scope; c != nil; c = refinementParent(c) {
		// Inside a refinement method body every sibling refinement of the same
		// holder module is active (MRI: same-module refinements see each other).
		if c.isRefinement && c.refHolder != nil {
			for _, r := range c.refHolder.refinements {
				add(r)
			}
		}
		// Later `using` wins over earlier, so walk the recorded modules in reverse.
		for i := len(c.usedModules) - 1; i >= 0; i-- {
			vm.collectRefinements(c.usedModules[i], add)
		}
	}
	return out
}

// collectRefinements adds every Refinement reachable from mod — its own and those
// of its ancestors (module inclusion activates a descendant module's refinements
// too), descendants before ancestors so a descendant override wins.
func (vm *VM) collectRefinements(mod *RClass, add func(*RClass)) {
	for _, anc := range vm.ancestors(mod) {
		for _, r := range anc.refinements {
			add(r)
		}
	}
}

// refinedMethod returns the refinement method that overrides name on recv given
// the refinements active in scope, or nil when the receiver's normal method (or
// method_missing) should handle the call. A singleton method and a method defined
// in a subclass of the refined class both take priority over the refinement, so
// the receiver's own ancestor chain is walked and a refinement only wins at the
// exact class it refines, ahead of that class's own definition.
func (vm *VM) refinedMethod(scope *RClass, recv object.Value, name string) *Method {
	if scope == nil {
		return nil
	}
	refs := vm.activeRefinements(scope)
	if len(refs) == 0 {
		return nil
	}
	// A per-object singleton method (def obj.m / extend) is looked up first and
	// always wins over a refinement.
	if sc := vm.objSingleton(recv); sc != nil {
		if m := lookupOwnOrIncluded(sc, name); m != nil && !m.undefined {
			return nil
		}
	}
	for _, k := range vm.ancestors(vm.classOf(recv)) {
		for _, r := range refs {
			if r.refinedClass != k {
				continue
			}
			if m := lookupOwnOrIncluded(r, name); m != nil && !m.undefined {
				return m
			}
		}
		// The class's own definition at this level wins over any lower-priority
		// refinement, so stop as soon as it is found.
		if m, ok := k.methods[name]; ok && !m.undefined {
			return nil
		}
	}
	return nil
}

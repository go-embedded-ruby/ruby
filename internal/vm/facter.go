// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	facter "github.com/go-ruby-facter/facter"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file installs the Facter module and its Ruby-facing API
// (require "facter"). The Ruby-faithful adapter — multiple weighted resolutions
// per fact, the confine family, aggregate facts and the execution seam — lives
// in github.com/go-ruby-facter/facter, which in turn wraps the pure-Go
// system-inventory engine github.com/go-facter/facter. rbgo owns only the
// object-model bridge: the Ruby Facter/Fact/Resolution surface, and the one
// interpreter seam the adapter delegates to — a resolution's setcode block,
// which runs INLINE on the VM goroutine under the GVL when the fact is resolved
// (facter_bind.go). The adapter is per-VM so custom facts never leak across
// interpreters.

// FacterFact is the object Facter[name] / Facter.fact(name) / Facter.add return
// — the Ruby Facter::Util::Fact, a handle over one go-ruby-facter *Fact that
// resolves its value lazily (highest-weight matching resolution wins).
type FacterFact struct {
	vm *VM
	f  *facter.Fact
}

func (f *FacterFact) ToS() string     { return "#<Facter::Util::Fact: " + f.f.Name() + ">" }
func (f *FacterFact) Inspect() string { return f.ToS() }
func (f *FacterFact) Truthy() bool    { return true }

// FacterResolution is the self a Facter.add(name) { … } block runs against
// (Facter::Util::Resolution): it captures the setcode block or command, the
// weight and the confines that define and gate one way of resolving the fact.
// After the block runs, facterRegister turns it into a go-ruby-facter resolution.
type FacterResolution struct {
	vm        *VM
	code      *Proc  // setcode { … } block
	command   string // setcode "cmd" string form
	hasCmd    bool
	weight    int
	hasWeight bool
	confines  []facter.Confine
}

func (r *FacterResolution) ToS() string     { return "#<Facter::Util::Resolution>" }
func (r *FacterResolution) Inspect() string { return r.ToS() }
func (r *FacterResolution) Truthy() bool    { return true }

// registerFacter installs the Facter module (require "facter"): value/[]/fact/
// list/to_hash read the per-VM adapter, add registers a custom fact backed by a
// Ruby setcode block (run under the GVL) with has_weight / confine gating, and
// clear/reset rebuild the adapter.
func (vm *VM) registerFacter() {
	vm.facterFacter = facter.New()

	mod := newClass("Facter", nil)
	mod.isModule = true
	vm.consts["Facter"] = mod

	// Facter::Util::Fact — the handle Facter[] / Facter.fact / Facter.add return.
	factCls := newClass("Facter::Util::Fact", vm.cObject)
	mod.consts["Util::Fact"] = factCls
	vm.consts["Facter::Util::Fact"] = factCls
	factCls.define("value", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return facterFactValue(self.(*FacterFact).f)
	})
	factCls.define("name", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Symbol(self.(*FacterFact).f.Name())
	})
	factCls.define("to_s", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*FacterFact).f.Name())
	})

	// Facter::Util::Resolution — the self a Facter.add block configures.
	resCls := newClass("Facter::Util::Resolution", vm.cObject)
	mod.consts["Util::Resolution"] = resCls
	vm.consts["Facter::Util::Resolution"] = resCls
	resCls.define("setcode", func(_ *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		r := self.(*FacterResolution)
		switch {
		case blk != nil:
			r.code = blk
		case len(args) > 0:
			r.command = nameArg(args[0])
			r.hasCmd = true
		}
		return object.NilV
	})
	resCls.define("has_weight", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		r := self.(*FacterResolution)
		if len(args) > 0 {
			r.weight = facterIntArg(args[0])
			r.hasWeight = true
		}
		return object.NilV
	})
	resCls.define("confine", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		self.(*FacterResolution).addConfine(vm, args, blk)
		return object.NilV
	})

	sm := func(name string, fn NativeFn) {
		mod.smethods[name] = &Method{name: name, owner: mod, native: fn}
	}

	// Facter.value(name) — the resolved value (dotted paths dig structured
	// facts), or nil when the fact is absent.
	sm("value", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.facterValue(facterNameArg(args))
	})

	// Facter[name] / Facter.fact(name) — a Fact handle, or nil for an unknown fact.
	factFn := func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		ft := vm.facterFacter.Fact(facterNameArg(args))
		if ft == nil {
			return object.NilV
		}
		return &FacterFact{vm: vm, f: ft}
	}
	sm("[]", factFn)
	sm("fact", factFn)

	// Facter.add(name) { setcode { … } [; has_weight n; confine …] } — register a
	// custom fact resolution; returns the Fact.
	sm("add", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		name := facterNameArg(args)
		res := &FacterResolution{vm: vm}
		if blk != nil {
			vm.callBlockSelf(blk, res, nil)
		}
		return &FacterFact{vm: vm, f: vm.facterRegister(name, res)}
	})

	// Facter.to_hash — every resolvable fact as a nested Ruby Hash.
	sm("to_hash", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return facterValueToRuby(vm.facterFacter.ToHash())
	})

	// Facter.list — the sorted names of all resolvable facts.
	sm("list", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return facterValueToRuby(vm.facterFacter.List())
	})

	// Facter.clear / Facter.reset — drop custom facts and cached values.
	clearFn := func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		vm.facterFacter.Reset()
		return object.NilV
	}
	sm("clear", clearFn)
	sm("reset", clearFn)
}

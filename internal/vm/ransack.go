// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-ruby-ransack/ransack"
)

// RansackSearch is the Ruby wrapper around a *ransack.Search — the value
// Model.ransack(q) (and Ransack::Search.new(subject, q)) returns. It carries the
// parsed search, the subject it was built over (the model/relation whose rows
// #result filters and sorts) and the original params Hash for #inspect. The
// parsing, predicate and evaluation logic all live in the
// github.com/go-ruby-ransack/ransack engine; this wrapper is the thin shell that
// maps rbgo's object model onto it and wires the record-source seam (see
// ransack_bind.go).
type RansackSearch struct {
	vm      *VM
	search  *ransack.Search
	subject object.Value
	params  *object.Hash
}

func (s *RansackSearch) ToS() string     { return "#<Ransack::Search>" }
func (s *RansackSearch) Inspect() string { return "#<Ransack::Search>" }
func (s *RansackSearch) Truthy() bool    { return true }

// RansackSort is the Ruby wrapper around a ransack.Sort — the ordering directive
// #sorts yields. It answers #name / #dir / #asc? / #desc?, mirroring the
// Ransack::Nodes::Sort surface the gem exposes.
type RansackSort struct {
	name string
	dir  string
}

func (s *RansackSort) ToS() string     { return s.name + " " + s.dir }
func (s *RansackSort) Inspect() string { return "#<Ransack::Sort " + s.name + " " + s.dir + ">" }
func (s *RansackSort) Truthy() bool    { return true }

// registerRansack installs the Ransack module and Ransack::Search surface
// (require "ransack"): the Model.ransack(q) / Model.search(q) class methods on
// ActiveRecord::Base subclasses, the Ransack.search(subject, q) module function
// and the Ransack::Search value they return, whose #result filters and sorts the
// subject's rows, #sorts lists the orderings, #errors the parse errors and
// #distinct? the distinct flag.
//
// Every parsing and evaluation decision — stripping the longest predicate
// suffix, the _or_ / _and_ combinators, g[] groups, s/sorts ordering, the
// attribute allowlist and the in-memory predicate evaluator — belongs to the
// engine. This file only bridges rbgo values to it and back.
func (vm *VM) registerRansack() {
	mod := newClass("Ransack", nil)
	mod.isModule = true
	vm.consts["Ransack"] = mod

	// Ransack.search(subject, params = {}) / Ransack.new(...) — the explicit,
	// ORM-agnostic constructor the gem also exposes.
	sm := func(name string, fn NativeFn) {
		mod.smethods[name] = &Method{name: name, owner: mod, native: fn}
	}
	build := func(vm *VM, args []object.Value) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		var params object.Value
		if len(args) > 1 {
			params = args[1]
		}
		return vm.newRansackSearch(args[0], params)
	}
	sm("search", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return build(vm, args)
	})

	vm.registerRansackSearchClass(mod)
	vm.registerRansackSortClass(mod)
	vm.wireRansackModels()
}

// registerRansackSearchClass installs Ransack::Search: its .new(subject, params)
// constructor and the #result / #sorts / #errors / #distinct? / #distinct
// instance surface.
func (vm *VM) registerRansackSearchClass(mod *RClass) {
	cls := newClass("Ransack::Search", vm.cObject)
	mod.consts["Search"] = cls
	vm.consts["Ransack::Search"] = cls

	cls.smethods["new"] = &Method{name: "new", owner: cls, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		var params object.Value
		if len(args) > 1 {
			params = args[1]
		}
		return vm.newRansackSearch(args[0], params)
	}}

	self := func(v object.Value) *RansackSearch { return v.(*RansackSearch) }

	// result(distinct: false) — the filtered, sorted (and optionally
	// distinct-collapsed) rows of the subject. The distinct: option forces
	// distinct even when the params did not request it, as ActiveRecord's
	// #result(distinct: true) does.
	cls.define("result", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		rs := self(v)
		distinct := rs.search.Distinct
		if len(args) > 0 {
			if h, ok := args[0].(*object.Hash); ok {
				if dv, ok := h.Get(object.Symbol("distinct")); ok {
					distinct = dv.Truthy()
				}
			}
		}
		return vm.ransackResult(rs, distinct)
	})

	cls.define("sorts", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		sorts := self(v).search.Sorts
		out := make([]object.Value, len(sorts))
		for i, s := range sorts {
			out[i] = &RansackSort{name: s.Name, dir: s.Dir}
		}
		return object.NewArrayFromSlice(out)
	})

	cls.define("errors", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		errs := self(v).search.Errors
		out := make([]object.Value, len(errs))
		for i, e := range errs {
			out[i] = object.NewString(e)
		}
		return object.NewArrayFromSlice(out)
	})

	cls.define("distinct?", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).search.Distinct)
	})
	cls.define("distinct", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).search.Distinct)
	})
}

// registerRansackSortClass installs Ransack::Sort — the #name / #dir / #asc? /
// #desc? node #sorts returns.
func (vm *VM) registerRansackSortClass(mod *RClass) {
	cls := newClass("Ransack::Sort", vm.cObject)
	mod.consts["Sort"] = cls
	vm.consts["Ransack::Sort"] = cls

	self := func(v object.Value) *RansackSort { return v.(*RansackSort) }
	cls.define("name", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).name)
	})
	cls.define("dir", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).dir)
	})
	cls.define("asc?", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).dir == "asc")
	})
	cls.define("desc?", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).dir == "desc")
	})
}

// wireRansackModels adds the ransack / search class methods to
// ActiveRecord::Base so every `class Post < ActiveRecord::Base` inherits them,
// exactly as the gem's ActiveRecord adapter does. The methods build a
// Ransack::Search over the receiver class (whose #all seam yields the rows
// #result evaluates). When ActiveRecord is not loaded the wiring is skipped —
// Ransack::Search.new and Ransack.search remain the ORM-agnostic entry points.
func (vm *VM) wireRansackModels() {
	base, ok := vm.consts["ActiveRecord::Base"].(*RClass)
	if !ok {
		return
	}
	fn := func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		var params object.Value
		if len(args) > 0 {
			params = args[0]
		}
		return vm.newRansackSearch(self, params)
	}
	base.smethods["ransack"] = &Method{name: "ransack", owner: base, native: fn}
	base.smethods["search"] = &Method{name: "search", owner: base, native: fn}
}

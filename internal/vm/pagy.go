// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"fmt"

	pagy "github.com/go-ruby-pagy/pagy"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// Pagy wraps a *pagy.Pagy as a Ruby Pagy object. The whole page-set arithmetic —
// clamping the page, deriving the offset/limit and the from/to record window,
// capping the last page, the prev/next links and the navigation series with its
// gaps — lives in the github.com/go-ruby-pagy/pagy library; this shell only
// reports the Ruby class and delegates each reader (see pagy_bind.go). It is pure
// computation, so no ActiveRecord (or any collection) is needed.
type Pagy struct{ p *pagy.Pagy }

func (p *Pagy) ToS() string { return p.Inspect() }
func (p *Pagy) Inspect() string {
	return fmt.Sprintf("#<Pagy count=%d page=%d items=%d>", p.p.Count, p.p.Page, p.p.Items)
}
func (p *Pagy) Truthy() bool { return true }

// registerPagy installs the Pagy class (require "pagy"): Pagy.new(count:, page:,
// items:, …) plus the reader surface (#offset/#limit/#pages/#last/#from/#to/#in
// and the nil-on-edge #prev/#next), #series (with an optional size), the
// Pagy::OverflowError / Pagy::VariableError error tree, and the top-level
// pagy(collection, **vars) helper that slices an Array by the computed window and
// returns [pagy, page_items].
func (vm *VM) registerPagy() {
	cls := newClass("Pagy", vm.cObject)
	vm.consts["Pagy"] = cls
	vm.registerPagyErrors(cls)

	// Pagy.new(count:, page:, items:, outset:, size:, max_pages:, cycle:, ends:)
	// builds a page set. A page past the last raises Pagy::OverflowError.
	cls.smethods["new"] = &Method{name: "new", owner: cls,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return &Pagy{p: pagyNew(pagyVars(args))}
		}}

	vm.registerPagyInstance(cls)

	// Kernel#pagy(collection, **vars) mirrors the controller helper: it paginates
	// an in-memory Array (count defaulting to its length) and returns
	// [pagy, page_items], the slice of the array for the current page.
	vm.cObject.define("pagy", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return pagyHelper(args)
	})
}

// registerPagyInstance installs the Pagy reader surface, mirroring the gem's
// public accessors.
func (vm *VM) registerPagyInstance(cls *RClass) {
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	d("count", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(pagyOf(self).Count))
	})
	d("page", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(pagyOf(self).Page))
	})
	d("items", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(pagyOf(self).Items))
	})
	d("limit", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(pagyOf(self).Limit()))
	})
	d("offset", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(pagyOf(self).Offset))
	})
	d("outset", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(pagyOf(self).Outset))
	})
	d("pages", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(pagyOf(self).Pages()))
	})
	d("last", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(pagyOf(self).Last))
	})
	d("from", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(pagyOf(self).From))
	})
	d("to", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(pagyOf(self).To))
	})
	d("in", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(pagyOf(self).In))
	})
	// #prev / #next return nil at the edges (no previous / next page), matching the
	// gem, where the library reports the absence as 0.
	d("prev", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return pagyPageOrNil(pagyOf(self).Prev)
	})
	d("next", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return pagyPageOrNil(pagyOf(self).Next)
	})
	// #series or #series(size): the navigation array of Integer links, the current
	// page as a String and :gap for elided ranges. A negative size raises
	// Pagy::VariableError, mirroring the gem.
	d("series", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		p := pagyOf(self)
		if len(args) == 0 {
			return pagySeries(p.Series())
		}
		s, err := p.SeriesSize(int(intArg(args[0])))
		if err != nil {
			raise("Pagy::VariableError", "%s", err.Error())
		}
		return pagySeries(s)
	})
	d("inspect", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*Pagy).Inspect())
	})
	d("to_s", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*Pagy).ToS())
	})
}

// registerPagyErrors installs the Pagy error tree mirroring the gem
// (Pagy::OverflowError / Pagy::VariableError < StandardError). Each is registered
// both as a nested constant of Pagy and under its qualified name.
func (vm *VM) registerPagyErrors(cls *RClass) {
	std := vm.consts["StandardError"].(*RClass)
	reg := func(simple, qualified string) {
		c := newClass(qualified, std)
		cls.consts[simple] = c
		vm.consts[qualified] = c
	}
	reg("OverflowError", "Pagy::OverflowError")
	reg("VariableError", "Pagy::VariableError")
}

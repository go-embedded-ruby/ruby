// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	drytypes "github.com/go-ruby-dry-types/dry-types"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file installs the shared Dry::Struct instance surface — to_h / to_hash /
// with / == / eql? / [] / inspect — on the base class, so every subclass's
// instances answer them. Attribute readers are installed per attribute by
// dryStructAttribute; the immutable value semantics come from the
// github.com/go-ruby-dry-struct/dry-struct library.

// registerDryStructInstance installs the value-object methods every DryStruct
// answers, delegating to the wrapped *drystruct.Struct.
func (vm *VM) registerDryStructInstance(base *RClass) {
	d := func(name string, fn NativeFn) { base.define(name, fn) }

	toh := func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return dryMapToHash(vm, object.Kind[*DryStruct](self).s.ToH())
	}
	d("to_h", toh)
	d("to_hash", toh)

	d("[]", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		v, ok := object.Kind[*DryStruct](self).s.Get(drytypes.Symbol(dryKeyName(args[0])))
		if !ok {
			raise("Dry::Struct::Error", "%s", "unknown attribute")
		}
		return dryFromGo(vm, v)
	})

	d("attributes", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return dryMapToHash(vm, object.Kind[*DryStruct](self).s.Attributes())
	})

	// with(changes) returns a new struct of the same type with the given
	// attributes replaced (the gem's Struct#new/#with).
	d("with", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		ds := object.Kind[*DryStruct](self)
		changes := drytypes.NewMap()
		if len(args) > 0 {
			if h, ok := object.KindOK[*object.Hash](args[0]); ok {
				for _, k := range h.Keys {
					v, _ := h.Get(k)
					changes.Set(drytypes.Symbol(dryKeyName(k)), dryToGo(v))
				}
			}
		}
		s, err := ds.s.With(changes)
		if err != nil {
			raise("Dry::Struct::Error", "%s", err.Error())
		}
		return &DryStruct{s: s, cls: ds.cls}
	})

	eq := func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			return object.Bool(false)
		}
		other, ok := object.KindOK[*DryStruct](args[0])
		if !ok {
			return object.Bool(false)
		}
		return object.Bool(object.Kind[*DryStruct](self).s.Eql(other.s))
	}
	d("==", eq)
	d("eql?", eq)

	d("inspect", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(object.Kind[*DryStruct](self).s.Inspect())
	})
	d("to_s", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(object.Kind[*DryStruct](self).s.String())
	})
}

// dryMapToHash maps a *drytypes.Map (a struct's attributes) to a Ruby Hash,
// preserving key order. Reused by the dry-validation binding.
func dryMapToHash(vm *VM, m *drytypes.Map) object.Value {
	h := object.NewHash()
	for _, p := range m.Pairs() {
		h.Set(dryFromGo(vm, p.Key), dryFromGo(vm, p.Val))
	}
	return h
}

// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	drytypes "github.com/go-ruby-dry-types/dry-types"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerDryTypeMethods installs the instance surface shared by every DryType:
// the application methods (call / [] / valid? / try) and the combinators
// (optional / default / constrained / enum / | / constructor / meta), plus the
// ArrayOf and Schema builders on the Types module. Each combinator returns a
// fresh DryType wrapping the library's derived type, so `Types::Strict::Integer |
// Types::Strict::String` composes exactly as the gem does.
func (vm *VM) registerDryTypeMethods(types *RClass) {
	cls := newClass("Dry::Types::Type", vm.cObject)
	types.consts["Type"] = cls
	vm.consts["Dry::Types::Type"] = cls

	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	// call(input) / [] coerce-and-validate, raising the mapped Dry::Types error
	// on failure (the gem's `type[input]`).
	apply := func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return dryCall(vm, self.(*DryType).t, args[0])
	}
	d("call", apply)
	d("[]", apply)

	// valid?(input) reports whether the type accepts input without raising.
	d("valid?", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return object.Bool(drytypes.Valid(self.(*DryType).t, dryToGo(args[0])))
	})

	// try(input) returns a Dry::Types::Result (#success? / #input / #error)
	// instead of raising, matching the gem's `type.try`.
	d("try", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return &DryResult{r: drytypes.Try(self.(*DryType).t, dryToGo(args[0]))}
	})

	d("optional", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return &DryType{t: self.(*DryType).t.Optional()}
	})
	d("default", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		if blk != nil {
			return &DryType{t: self.(*DryType).t.DefaultFn(func() any {
				return dryToGo(vm.callBlock(blk, nil))
			})}
		}
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return &DryType{t: self.(*DryType).t.Default(dryToGo(args[0]))}
	})
	d("constrained", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return &DryType{t: self.(*DryType).t.Constrained(dryConstraints(args)...)}
	})
	d("enum", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		vals := make([]any, len(args))
		for i, a := range args {
			vals[i] = dryToGo(a)
		}
		return &DryType{t: self.(*DryType).t.Enum(vals...)}
	})
	d("constructor", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("ArgumentError", "no block given")
		}
		return &DryType{t: self.(*DryType).t.Constructor(func(in any) any {
			return dryToGo(vm.callBlock(blk, []object.Value{dryFromGo(vm, in)}))
		})}
	})
	d("meta", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			return dryMetaToHash(vm, self.(*DryType).t.GetMeta())
		}
		h, ok := args[0].(*object.Hash)
		if !ok {
			raise("TypeError", "no implicit conversion of %s into Hash", args[0].Inspect())
		}
		m := map[string]any{}
		for _, k := range h.Keys {
			v, _ := h.Get(k)
			m[dryKeyName(k)] = dryToGo(v)
		}
		return &DryType{t: self.(*DryType).t.Meta(m)}
	})
	d("|", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		other, ok := args[0].(*DryType)
		if !ok {
			raise("TypeError", "no implicit conversion of %s into Dry::Types::Type", args[0].Inspect())
		}
		return &DryType{t: self.(*DryType).t.Or(other.t)}
	})

	// Types.Array.of / Types::Array(elem) builds an array type whose members are
	// coerced by elem — the gem's `Types::Array.of(Types::Strict::Integer)`.
	types.smethods["ArrayOf"] = &Method{name: "ArrayOf", owner: types,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
			}
			el, ok := args[0].(*DryType)
			if !ok {
				raise("TypeError", "no implicit conversion of %s into Dry::Types::Type", args[0].Inspect())
			}
			return &DryType{t: drytypes.ArrayOf(el.t)}
		}}

	// Types.Hash(schema) / HashSchema builds a keyed hash type from a Ruby Hash of
	// { name => type }; a trailing "?" on a key name marks it optional.
	types.smethods["Schema"] = &Method{name: "Schema", owner: types,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
			}
			h, ok := args[0].(*object.Hash)
			if !ok {
				raise("TypeError", "no implicit conversion of %s into Hash", args[0].Inspect())
			}
			return &DryType{t: drySchema(h).AsType()}
		}}

	vm.registerDryResultMethods(types)
}

// registerDryResultMethods installs the Dry::Types::Result surface #try returns.
func (vm *VM) registerDryResultMethods(types *RClass) {
	cls := newClass("Dry::Types::Result", vm.cObject)
	types.consts["Result"] = cls
	vm.consts["Dry::Types::Result"] = cls

	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	d("success?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self.(*DryResult).r.Success())
	})
	d("failure?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self.(*DryResult).r.Failure())
	})
	d("input", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return dryFromGo(vm, self.(*DryResult).r.Input())
	})
	d("error", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if err := self.(*DryResult).r.Error(); err != nil {
			return object.NewString(err.Error())
		}
		return object.NilV
	})
}

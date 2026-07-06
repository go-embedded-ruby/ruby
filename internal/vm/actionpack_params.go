// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"github.com/go-ruby-actionpack/actionpack/parameters"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file wires ActionController::Parameters (Rails strong parameters):
// .new(hash), permit/require, [], to_h/to_unsafe_h, permitted?, key?, keys and
// merge. The permit/require filtering semantics are the library's; this shell
// only maps Ruby values in and out.

// registerACParameters installs the ActionController::Parameters surface.
func (vm *VM) registerACParameters(cls *RClass) {
	self := func(v object.Value) *parameters.Parameters { return v.(*ACParams).p }

	cls.smethods["new"] = &Method{name: "new", owner: cls, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return &ACParams{p: parameters.New(acAnyMap(lastHashOrNil(args))), cls: cls}
	}}

	// permit(*filters) — return a new, permitted Parameters filtered by the
	// String/Array/Hash filter list.
	cls.define("permit", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return &ACParams{p: self(v).Permit(acPermitFilters(args)...), cls: vm.cACParameters}
	})

	// require(key) — return the value under key (raising ParameterMissing when
	// absent/blank); require([:a, :b]) requires each in turn.
	cls.define("require", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if arr, ok := apArg(args, 0).(*object.Array); ok {
			keys := make([]string, len(arr.Elems))
			for i, e := range arr.Elems {
				keys[i] = apStr(e)
			}
			vals, err := self(v).RequireAll(keys)
			if err != nil {
				raise("ActionController::ParameterMissing", "%s", err.Error())
			}
			out := make([]object.Value, len(vals))
			for i, val := range vals {
				out[i] = vm.acParamValue(val)
			}
			return object.NewArrayFromSlice(out)
		}
		val, err := self(v).Require(apStr(apArg(args, 0)))
		if err != nil {
			raise("ActionController::ParameterMissing", "%s", err.Error())
		}
		return vm.acParamValue(val)
	})

	// [] — read the value under key (nil when absent), matching Rails' accessor.
	cls.define("[]", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		val, ok := self(v).Get(apStr(apArg(args, 0)))
		if !ok {
			return object.NilV
		}
		return vm.acParamValue(val)
	})

	// to_h — a deep plain-Hash of the permitted params (raising when unpermitted).
	cls.define("to_h", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		m, err := self(v).ToH()
		if err != nil {
			raise("ActionController::UnfilteredParameters", "%s", err.Error())
		}
		return rackFromGo(m)
	})

	// to_unsafe_h — a deep plain-Hash regardless of the permitted flag.
	cls.define("to_unsafe_h", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return rackFromGo(self(v).ToUnsafeH())
	})

	cls.define("permitted?", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).Permitted())
	})

	keyP := func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).Has(apStr(apArg(args, 0))))
	}
	cls.define("key?", keyP)
	cls.define("has_key?", keyP)

	cls.define("keys", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		ks := self(v).Keys()
		out := make([]object.Value, len(ks))
		for i, k := range ks {
			out[i] = object.NewString(k)
		}
		return object.NewArrayFromSlice(out)
	})

	// merge(other) — a new Parameters overlaid by another Parameters' pairs.
	cls.define("merge", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		other, ok := apArg(args, 0).(*ACParams)
		if !ok {
			raise("TypeError", "no implicit conversion into ActionController::Parameters")
		}
		return &ACParams{p: self(v).Merge(other.p), cls: vm.cACParameters}
	})
}

// acParamValue maps a strong-parameters value (as returned by Get/Require) back
// into the rbgo object graph: a nested *parameters.Parameters becomes a wrapped
// ActionController::Parameters, everything else its rackFromGo form.
func (vm *VM) acParamValue(v any) object.Value {
	if p, ok := v.(*parameters.Parameters); ok {
		return &ACParams{p: p, cls: vm.cACParameters}
	}
	return rackFromGo(v)
}

// acPermitFilters maps permit's arguments into the []any filter list the library
// consumes: a String/Symbol is a scalar key, a Hash (key => [] / [inner] / {})
// is a nested/array filter mapped through rackToGo.
func acPermitFilters(args []object.Value) []any {
	out := make([]any, 0, len(args))
	for _, a := range args {
		if h, ok := a.(*object.Hash); ok {
			out = append(out, acAnyMap(h))
			continue
		}
		out = append(out, apStr(a))
	}
	return out
}

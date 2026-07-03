// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/object"
	ostruct "github.com/go-ruby-ostruct/ostruct"
)

// registerOstruct backs the DATA surface of the prelude's OpenStruct class with
// github.com/go-ruby-ostruct/ostruct — a pure-Go (cgo-free) reimplementation of
// the ordered attribute table underneath Ruby's OpenStruct (require "ostruct"),
// matching MRI 4.0.5. The table algebra (to_h / inspect / dig / delete_field /
// == / eql?) lives in the library; the prelude's OpenStruct keeps the @table
// Hash as the source of truth and — out of the library's scope — the dynamic
// accessor glue (method_missing readers and `name=` writers, respond_to_missing?,
// [], []=, each_pair) that turns a method name into a table read or write at call
// time.
//
// The binding is exposed as native class-level helpers named __data_* on the
// OpenStruct class. They take the live @table Hash (whose keys the prelude has
// already interned to Symbols, in insertion order) and a value model so the
// library renders and digs real Ruby objects: each Ruby value handed to the
// library is wrapped in a vmValue, whose Inspect / Dig / RubyClassName call back
// into the VM (respecting a Ruby-defined #inspect or #dig). registerOstruct runs
// after the prelude so it reopens the prelude-defined class rather than creating
// it.
func (vm *VM) registerOstruct() {
	cls, ok := object.KindOK[*RClass](vm.consts["OpenStruct"])
	if !ok {
		// The prelude always defines OpenStruct; if a host stripped it, there is
		// nothing to back, so leave the data helpers unbound.
		return
	}

	sm := func(name string, fn NativeFn) { cls.smethods[name] = &Method{name: name, owner: cls, native: fn} }

	// __data_to_h(table) -> a fresh Hash of the table's Symbol=>value entries in
	// insertion order (MRI's OpenStruct#to_h, whose Hash preserves that order).
	sm("__data_to_h", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		os := vm.ostructFromHash(args[0])
		h := object.NewHash()
		for _, p := range os.ToH() {
			h.Set(object.SymVal(string(object.Symbol(p.Key.(ostruct.Symbol)))), unwrapVMValue(p.Value))
		}
		return object.Wrap(h)
	})

	// __data_inspect(table) -> the "#<OpenStruct k=v, ...>" string (and #to_s),
	// each value rendered through its Ruby #inspect, matching MRI.
	sm("__data_inspect", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(vm.ostructFromHash(args[0]).Inspect()))
	})

	// __data_dig(table, *keys) -> table.dig(*keys): the first key reads the
	// table, remaining keys delegate to that value's #dig. No keys raises
	// ArgumentError; an intermediate value without #dig raises TypeError; a
	// missing intermediate value yields nil — MRI's OpenStruct#dig.
	sm("__data_dig", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		os := vm.ostructFromHash(args[0])
		// The first key indexes THIS table, so it is interned to a Symbol the
		// library's Get matches; any remaining keys are delegated (unchanged) to
		// the field value's Ruby #dig, so they are wrapped to round-trip back.
		keys := make([]any, len(args)-1)
		for i, k := range args[1:] {
			if i == 0 {
				keys[i] = symKey(k)
			} else {
				keys[i] = vm.wrapVMValue(k)
			}
		}
		res, err := os.Dig(keys...)
		if err != nil {
			vm.raiseOstructErr(err)
		}
		return unwrapVMValue(res)
	})

	// __data_delete_field(table, name) -> removes field name from the live table
	// Hash and returns its prior value, raising NameError (with MRI's exact
	// message) when the field is absent — MRI's OpenStruct#delete_field.
	sm("__data_delete_field", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		h := object.Kind[*object.Hash](args[0])
		os := vm.ostructFromHash(object.Wrap(h))
		v, err := os.DeleteField(symKey(args[1]))
		if err != nil {
			vm.raiseOstructErr(err)
		}
		h.Delete(args[1])
		return unwrapVMValue(v)
	})

	// __data_eq(table, other_table_or_nil) -> whether two OpenStructs are equal
	// as Symbol=>value tables (MRI's == / eql?). The prelude has already checked
	// other.is_a?(OpenStruct); a nil second argument (non-OpenStruct) is unequal.
	// The library compares the field SET (same members, same count); each pair of
	// values is then compared with Ruby's #==, so reference values (String, Array,
	// nested OpenStruct, …) compare by content as MRI does.
	sm("__data_eq", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if _, isNil := object.AsNilOK(args[1]); isNil {
			return object.BoolValue(bool(object.Bool(false)))
		}
		a, b := vm.ostructFromHash(args[0]), vm.ostructFromHash(args[1])
		if a.Len() != b.Len() {
			return object.BoolValue(bool(object.Bool(false)))
		}
		eq := true
		a.EachPair(func(k ostruct.Symbol, av any) bool {
			if !b.RespondToField(k) {
				eq = false
				return false
			}
			bv := b.Get(k)
			if !vm.send(unwrapVMValue(av), "==", []object.Value{unwrapVMValue(bv)}, nil).Truthy() {
				eq = false
				return false
			}
			return true
		})
		return object.BoolValue(bool(object.Bool(eq)))
	})
}

// ostructFromHash builds a library *ostruct.OpenStruct from a Ruby @table Hash,
// preserving its insertion order. The Hash's keys are Symbols (the prelude
// interns them on assignment); each value is wrapped in a vmValue so the library
// renders/digs/classifies it through the VM.
func (vm *VM) ostructFromHash(v object.Value) *ostruct.OpenStruct {
	h, ok := object.KindOK[*object.Hash](v)
	if !ok {
		return ostruct.New()
	}
	pairs := make([]ostruct.Pair, 0, len(h.Keys))
	for _, k := range h.Keys {
		val, _ := h.Get(k)
		pairs = append(pairs, ostruct.Pair{Key: symKey(k), Value: vm.wrapVMValue(val)})
	}
	return ostruct.New(pairs...)
}

// symKey converts a Ruby Hash key to a library Symbol. Table keys are Symbols;
// a stray String key is interned by its content, matching MRI's name.to_sym.
func symKey(k object.Value) ostruct.Symbol {
	{
		__sw117 := k
		switch {
		case object.IsKind[object.Symbol](__sw117):
			t := object.Kind[object.Symbol](__sw117)
			_ = t
			return ostruct.Symbol(t)
		case object.IsKind[*object.String](__sw117):
			t := object.Kind[*object.String](__sw117)
			_ = t
			return ostruct.Symbol(t.Str())
		default:
			t := __sw117
			_ = t
			return ostruct.ToSym(k.ToS())
		}
	}
}

// raiseOstructErr re-raises a library error as the MRI exception OpenStruct would
// raise, carrying the library's MRI-exact message.
func (vm *VM) raiseOstructErr(err error) {
	switch err.(type) {
	case *ostruct.NameError:
		raise("NameError", "%s", err.Error())
	case *ostruct.TypeError:
		raise("TypeError", "%s", err.Error())
	case *ostruct.ArgumentError:
		raise("ArgumentError", "%s", err.Error())
	default:
		raise("RuntimeError", "%s", err.Error())
	}
}

// vmValue wraps a Ruby object.Value so the ostruct library can render and
// classify it through the VM — implementing the library's Inspector and Classer
// (RubyClassName) interfaces. It deliberately does NOT implement Digger, so the
// library's #dig TypeError path ("CLASS does not have #dig method") fires for a
// value that cannot be dug into (an Integer, String, …). A value that DOES
// respond to #dig is wrapped in vmDigValue instead. The wrapping is transparent:
// the value round-trips back out via unwrapVMValue.
type vmValue struct {
	vm *VM
	v  object.Value
}

// vmDigValue is a vmValue that additionally implements the library's Digger, so
// the library delegates the remaining dig keys to the value's Ruby #dig. Only
// values that respond to #dig (Array, Hash, Struct, a nested OpenStruct, …) are
// wrapped this way; everything else stays a plain vmValue.
type vmDigValue struct{ vmValue }

// wrapVMValue wraps a Ruby value for the library. Ruby nil (and a Go-nil, an
// absent table entry) maps to a bare Go nil so the library's nil handling — its
// to_h/inspect rendering and its dig short-circuit (a nil intermediate yields
// nil, never a "nil has no #dig" error) — applies unchanged, exactly as MRI. A
// non-nil value that responds to #dig is wrapped as a Digger; every other value
// as a plain vmValue.
func (vm *VM) wrapVMValue(v object.Value) any {
	if object.IsNil(v) {
		return nil
	}
	if _, isNil := object.AsNilOK(v); isNil {
		return nil
	}
	base := vmValue{vm: vm, v: v}
	if vm.respondsTo(v, "dig") {
		return vmDigValue{base}
	}
	return base
}

// unwrapVMValue recovers the Ruby value the library carried through (Get/ToH/Dig
// return it as any). A plain library Symbol (only produced for keys, never
// values, on these paths) maps back to a Ruby Symbol; a bare Go nil maps to nil.
func unwrapVMValue(v any) object.Value {
	switch t := v.(type) {
	case nil:
		return object.NilVal()
	case vmDigValue:
		return t.v
	case vmValue:
		return t.v
	case ostruct.Symbol:
		return object.SymVal(string(object.Symbol(t)))
	default:
		return object.NilVal()
	}
}

// Inspect renders the wrapped value through its Ruby #inspect (honouring a
// user-defined override), so OpenStruct#inspect matches MRI byte-for-byte.
func (w vmValue) Inspect() string { return w.vm.inspectStr(w.v) }

// RubyClassName gives the wrapped value's Ruby class, used by the library when it
// reports a #dig TypeError ("CLASS does not have #dig method").
func (w vmValue) RubyClassName() string { return w.vm.classOf(w.v).name }

// Dig delegates dig into the wrapped value to its Ruby #dig, so a nested Array /
// Hash / Struct / OpenStruct dug through an OpenStruct behaves exactly as in MRI.
func (w vmDigValue) Dig(keys ...any) (any, error) {
	args := make([]object.Value, len(keys))
	for i, k := range keys {
		args[i] = unwrapVMValue(k)
	}
	return w.vm.wrapVMValue(w.vm.send(w.v, "dig", args, nil)), nil
}

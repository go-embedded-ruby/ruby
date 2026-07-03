// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	drystruct "github.com/go-ruby-dry-struct/dry-struct"
	drytypes "github.com/go-ruby-dry-types/dry-types"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// DryStruct wraps a *drystruct.Struct as an instance of a user's Dry::Struct
// subclass. The attribute schema, coercion and immutable value semantics live in
// the github.com/go-ruby-dry-struct/dry-struct library; this shell reports the
// owning Ruby subclass (via classOf) so the generated readers and shared
// instance methods dispatch, and delegates to the wrapped Struct (see
// drystruct_bind.go).
type DryStruct struct {
	s   *drystruct.Struct
	cls *RClass
}

func (s *DryStruct) ToS() string     { return s.s.String() }
func (s *DryStruct) Inspect() string { return s.s.Inspect() }
func (s *DryStruct) Truthy() bool    { return true }

// dryStructMeta is the opaque holder stashed in a Dry::Struct subclass's ivars,
// binding that Ruby class to its accumulating *drystruct.StructType. It satisfies
// object.Value so it can live in the ivar table, but is never surfaced to Ruby.
type dryStructMeta struct{ st *drystruct.StructType }

func (m *dryStructMeta) ToS() string     { return "#<Dry::Struct schema>" }
func (m *dryStructMeta) Inspect() string { return m.ToS() }
func (m *dryStructMeta) Truthy() bool    { return true }

const dryStructMetaIvar = "@__dry_struct_type__"

// registerDryStruct installs the Dry::Struct base class (require "dry/struct").
// A subclass accumulates its attribute schema through the `attribute` /
// `attribute?` / `transform_keys` class methods; `.new(hash)` coerces-and-
// validates the input against that schema (delegating to go-ruby-dry-struct) and
// yields a DryStruct instance with generated readers plus the shared #to_h /
// #with / #== / #inspect surface. Dry::Struct::Error is raised on a coercion
// failure. Dry::Struct pins go-ruby-dry-types, whose Types:: constructors supply
// the attribute types.
func (vm *VM) registerDryStruct() {
	dry := vm.dryModule()

	base := newClass("Dry::Struct", vm.cObject)
	dry.consts["Struct"] = object.Wrap(base)
	vm.consts["Dry::Struct"] = object.Wrap(base)
	vm.registerDryStructError(base)

	// A struct's own value-object shorthand: Dry::Struct::Value is a struct whose
	// instances compare by value (the gem's Dry::Struct::Value); rbgo structs are
	// already value-compared, so Value is an alias of the base for `< Value`.
	base.consts["Value"] = object.Wrap(base)

	sdef := func(name string, fn NativeFn) { base.smethods[name] = &Method{name: name, owner: base, native: fn} }

	// attribute(name, type) / attribute?(name, type) declare a (optionally
	// optional) member on the receiving subclass's schema and install its reader.
	sdef("attribute", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.dryStructAttribute(self, args, false)
	})
	sdef("attribute?", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.dryStructAttribute(self, args, true)
	})

	// transform_keys(:symbolize|:stringify) sets the subclass's key transform,
	// matching `transform_keys(&:to_sym)` / `(&:to_s)`.
	sdef("transform_keys", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		cls := object.Kind[*RClass](self)
		st := vm.dryStructType(cls)
		mode := drystruct.KeyNone
		if len(args) > 0 {
			switch dryKeyName(args[0]) {
			case "symbolize", "to_sym":
				mode = drystruct.KeySymbolize
			case "stringify", "to_s":
				mode = drystruct.KeyStringify
			}
		}
		vm.setDryStructType(cls, st.TransformKeys(mode))
		return object.NilVal()
	})

	// new(hash) coerces the input hash into a struct instance; a coercion failure
	// raises Dry::Struct::Error. new(existing_instance_of_same_type) passes it
	// through, matching the gem.
	sdef("new", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		cls := object.Kind[*RClass](self)
		st := vm.dryStructType(cls)
		var input any
		if len(args) > 0 {
			if ds, ok := object.KindOK[*DryStruct](args[0]); ok {
				input = ds.s
			} else {
				input = dryToGo(args[0])
			}
		} else {
			input = drytypes.NewMap()
		}
		s, err := st.New(input)
		if err != nil {
			raise("Dry::Struct::Error", "%s", err.Error())
		}
		return object.Wrap(&DryStruct{s: s, cls: cls})
	})

	vm.registerDryStructInstance(base)
}

// dryStructAttribute declares an attribute on self's schema, installs its reader
// on self, and returns nil. The type argument is a DryType or a nested Dry::Struct
// subclass (whose StructType coerces the value); a missing name or type raises
// ArgumentError.
func (vm *VM) dryStructAttribute(self object.Value, args []object.Value, optional bool) object.Value {
	cls, ok := object.KindOK[*RClass](self)
	if !ok {
		raise("TypeError", "attribute must be called on a Dry::Struct subclass")
	}
	if len(args) < 2 {
		raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
	}
	name := drytypes.Symbol(dryKeyName(args[0]))
	st := vm.dryStructType(cls)
	{
		__sw49 := args[1]
		switch {
		case object.IsKind[*DryType](__sw49):
			t := object.Kind[*DryType](__sw49)
			_ = t
			if optional {
				st = st.AttributeOpt(name, t.t)
			} else {
				st = st.Attribute(name, t.t)
			}
		case object.IsKind[*RClass](__sw49):
			t := object.Kind[*RClass](__sw49)
			_ = t
			nested := vm.dryStructType(t)
			if optional {
				st = st.AttributeTypeOpt(name, nested)
			} else {
				st = st.AttributeType(name, nested)
			}
		default:
			t := __sw49
			_ = t
			raise("TypeError", "attribute type must be a Dry::Types type or a Dry::Struct subclass")
		}
	}
	vm.setDryStructType(cls, st)
	// Install the reader for this attribute (idempotent on redefinition).
	rd := string(name)
	cls.define(rd, func(vm *VM, recv object.Value, _ []object.Value, _ *Proc) object.Value {
		ds := object.Kind[*DryStruct](recv)
		v, ok := ds.s.Get(name)
		if !ok {
			return object.NilVal()
		}
		return dryFromGo(vm, v)
	})
	return object.NilVal()
}

// dryStructType returns cls's accumulating *drystruct.StructType, creating and
// stashing a fresh one (inheriting a parent subclass's schema) on first use.
func (vm *VM) dryStructType(cls *RClass) *drystruct.StructType {
	if m, ok := object.KindOK[*dryStructMeta](cls.ivars[dryStructMetaIvar]); ok {
		return m.st
	}
	var st *drystruct.StructType
	// A subclass of another struct subclass inherits its parent's attributes.
	if p := cls.super; p != nil {
		if pm, ok := object.KindOK[*dryStructMeta](p.ivars[dryStructMetaIvar]); ok {
			st = pm.st.Inherit(cls.name)
		}
	}
	if st == nil {
		name := cls.name
		if name == "" {
			name = "Dry::Struct"
		}
		st = drystruct.New(name)
	}
	vm.setDryStructType(cls, st)
	return st
}

// setDryStructType stashes st as cls's schema holder.
func (vm *VM) setDryStructType(cls *RClass, st *drystruct.StructType) {
	if cls.ivars == nil {
		cls.ivars = map[string]object.Value{}
	}
	cls.ivars[dryStructMetaIvar] = object.Wrap(&dryStructMeta{st: st})
}

// registerDryStructError installs Dry::Struct::Error < StandardError, registered
// as a nested constant of Dry::Struct and under its qualified name so both the
// Ruby constant and a re-raised library error resolve to the same class.
func (vm *VM) registerDryStructError(base *RClass) {
	std := object.Kind[*RClass](vm.consts["StandardError"])
	c := newClass("Dry::Struct::Error", std)
	base.consts["Error"] = object.Wrap(c)
	vm.consts["Dry::Struct::Error"] = object.Wrap(c)
}

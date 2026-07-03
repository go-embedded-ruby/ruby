// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	drytypes "github.com/go-ruby-dry-types/dry-types"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// DryType wraps a drytypes.Type as a Ruby Dry::Types::Type object. The coercion,
// validation and combinator composition live in the
// github.com/go-ruby-dry-types/dry-types library — the pure-Go core that backs
// the `dry-types` gem — and this shell only reports the Ruby class (via classOf)
// and delegates .call / [] / .valid? / .try and the combinators to the wrapped
// Type (see drytypes_bind.go).
type DryType struct{ t drytypes.Type }

func (t *DryType) ToS() string     { return "#<Dry::Types::Type>" }
func (t *DryType) Inspect() string { return "#<Dry::Types::Type>" }
func (t *DryType) Truthy() bool    { return true }

// DryResult wraps a drytypes.Result as a Ruby Dry::Types::Result object, the
// value #try returns (#success? / #failure? / #input / #error).
type DryResult struct{ r drytypes.Result }

func (r *DryResult) ToS() string     { return "#<Dry::Types::Result>" }
func (r *DryResult) Inspect() string { return "#<Dry::Types::Result>" }
func (r *DryResult) Truthy() bool    { return true }

// registerDryTypes installs the Dry::Types surface (require "dry/types"): the
// Dry::Types[...] name lookup, the Dry.Types() module method, the
// Types::Strict/Coercible/Params/JSON constructor namespaces, ArrayOf / HashSchema
// builders, the combinator methods on a type, and the Dry::Types error tree. The
// coercion and combinators come from the go-ruby-dry-types library; this module is
// the wiring that maps a Ruby type name / options into a DryType and back.
func (vm *VM) registerDryTypes() {
	dry := vm.dryModule()

	types := newClass("Dry::Types", vm.cObject)
	types.isModule = true
	dry.consts["Types"] = object.Wrap(types)
	vm.consts["Dry::Types"] = object.Wrap(types)

	// Dry::Types[name] resolves a registered type by its dotted name
	// ("strict.integer", "coercible.string", "params.bool", …).
	types.smethods["[]"] = &Method{name: "[]", owner: types,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
			}
			return dryLookup(dryTypeName(args[0]))
		}}

	vm.registerDryTypesErrors(types)
	vm.registerDryTypeMethods(types)

	// Dry.Types() returns a module whose constants (Strict, Coercible, Params,
	// JSON, plus the bare Strict names) are the type constructors, matching
	// `include Dry.Types()`. rbgo exposes the same surface as the Types:: module.
	dry.smethods["Types"] = &Method{name: "Types", owner: dry,
		native: func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return object.Wrap(types)
		}}

	// Types::Strict / Coercible / Params / JSON namespaces carry the per-primitive
	// constructors as constants (Types::Strict::Integer, Types::Params::Bool, …)
	// and as methods, so both the constant and the gem's method spelling resolve.
	vm.installDryNamespace(types, "Strict", "strict")
	vm.installDryNamespace(types, "Coercible", "coercible")
	vm.installDryNamespace(types, "Params", "params")
	vm.installDryNamespace(types, "JSON", "json")
	vm.installDryNamespace(types, "Nominal", "nominal")
}

// dryModule returns (creating on first use) the shared Dry module both
// dry-types and dry-struct/dry-validation hang their constants under.
func (vm *VM) dryModule() *RClass {
	if c, ok := object.KindOK[*RClass](vm.consts["Dry"]); ok {
		return c
	}
	dry := newClass("Dry", nil)
	dry.isModule = true
	vm.consts["Dry"] = object.Wrap(dry)
	return dry
}

// installDryNamespace installs a constructor namespace (e.g. Strict) under Types
// as both a nested module with per-primitive constants (Integer, String, …) and
// method accessors, resolving each to the dotted name "<prefix>.<primitive>".
func (vm *VM) installDryNamespace(types *RClass, name, prefix string) {
	ns := newClass("Dry::Types::"+name, vm.cObject)
	ns.isModule = true
	types.consts[name] = object.Wrap(ns)
	// The namespace is also a Types method (Types.Strict) for the gem's
	// `Types::Strict::Integer`-via-method-call spelling.
	for _, prim := range dryPrimitives {
		full := prefix + "." + prim.name
		if _, ok := dryRegistry[full]; !ok {
			// Not every prefix registers every primitive (e.g. coercible has no
			// bool); skip the ones the library does not define.
			continue
		}
		cst := prim.constant
		ns.consts[cst] = dryLookup(full)
		p := prefix
		pr := prim.name
		ns.smethods[cst] = &Method{name: cst, owner: ns,
			native: func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
				return dryLookup(p + "." + pr)
			}}
	}
}

// dryPrimitive names a primitive constructor by its Ruby constant/name.
type dryPrimitive struct {
	name     string // dotted-name suffix, e.g. "integer"
	constant string // Ruby constant, e.g. "Integer"
}

// dryPrimitives is the set of primitives every namespace exposes. Not every
// prefix has every primitive registered (dryLookup gates the real combinations);
// the namespace constants cover the common ones the gem ships.
var dryPrimitives = []dryPrimitive{
	{"integer", "Integer"}, {"float", "Float"}, {"string", "String"},
	{"symbol", "Symbol"}, {"bool", "Bool"}, {"nil", "Nil"},
	{"array", "Array"}, {"hash", "Hash"},
	{"date", "Date"}, {"time", "Time"}, {"date_time", "DateTime"},
}

// registerDryTypesErrors installs the Dry::Types error tree mirroring the gem:
// Dry::Types::CoercionError < StandardError, and ConstraintError / SchemaError <
// CoercionError. Each class is registered as a nested constant of Dry::Types and
// under its qualified name in the top-level table, so both `Dry::Types::X` and a
// re-raised library error resolve to the same class.
func (vm *VM) registerDryTypesErrors(types *RClass) {
	std := object.Kind[*RClass](vm.consts["StandardError"])
	reg := func(simple, qualified string, super *RClass) *RClass {
		c := newClass(qualified, super)
		types.consts[simple] = object.Wrap(c)
		vm.consts[qualified] = object.Wrap(c)
		return c
	}
	coerce := reg("CoercionError", "Dry::Types::CoercionError", std)
	reg("ConstraintError", "Dry::Types::ConstraintError", coerce)
	reg("SchemaError", "Dry::Types::SchemaError", coerce)
}

// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	drytypes "github.com/go-ruby-dry-types/dry-types"
	dryvalidation "github.com/go-ruby-dry-validation/dry-validation"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// DrySchema wraps a *dryvalidation.Schema built by Dry::Schema.Params/JSON. The
// schema's key macros, type coercion and predicate evaluation live in the
// github.com/go-ruby-dry-validation/dry-validation library; rbgo runs the schema
// DSL block against a builder shell and applies the schema to an input hash.
type DrySchema struct{ s *dryvalidation.Schema }

func (s *DrySchema) ToS() string     { return "#<Dry::Schema::Params>" }
func (s *DrySchema) Inspect() string { return s.ToS() }
func (s *DrySchema) Truthy() bool    { return true }

// DrySchemaBuilder is the DSL self a schema block runs against; its required /
// optional methods return a DryKey to attach a value macro.
type DrySchemaBuilder struct{ b *dryvalidation.Builder }

func (b *DrySchemaBuilder) ToS() string     { return "#<Dry::Schema::DSL>" }
func (b *DrySchemaBuilder) Inspect() string { return b.ToS() }
func (b *DrySchemaBuilder) Truthy() bool    { return true }

// DryKey is the macro attachment point required(...)/optional(...) returns.
type DryKey struct{ k *dryvalidation.Key }

func (k *DryKey) ToS() string     { return "#<Dry::Schema::Key>" }
func (k *DryKey) Inspect() string { return k.ToS() }
func (k *DryKey) Truthy() bool    { return true }

// DryContract wraps a Dry::Validation::Contract subclass instance: its schema and
// the custom rule bodies (each a compiled Ruby block run as a RuleContext seam).
type DryContract struct {
	c   *dryvalidation.Contract
	cls *RClass
}

func (c *DryContract) ToS() string     { return "#<Dry::Validation::Contract>" }
func (c *DryContract) Inspect() string { return c.ToS() }
func (c *DryContract) Truthy() bool    { return true }

// DryValidationResult wraps a *dryvalidation.Result (#success? / #errors / #to_h).
type DryValidationResult struct{ r *dryvalidation.Result }

func (r *DryValidationResult) ToS() string     { return "#<Dry::Validation::Result>" }
func (r *DryValidationResult) Inspect() string { return r.ToS() }
func (r *DryValidationResult) Truthy() bool    { return true }

// dryContractMeta stashes a Contract subclass's built schema and pending rules,
// held in the subclass's ivars until an instance is constructed.
type dryContractMeta struct {
	schema *dryvalidation.Schema
	rules  []dryContractRule
}

func (m *dryContractMeta) ToS() string     { return "#<Dry::Validation::Contract schema>" }
func (m *dryContractMeta) Inspect() string { return m.ToS() }
func (m *dryContractMeta) Truthy() bool    { return true }

// dryContractRule is one custom rule: the keys it depends on and its Ruby body.
type dryContractRule struct {
	keys []drytypes.Symbol
	body *Proc
}

const dryContractMetaIvar = "@__dry_contract__"

// registerDryValidation installs Dry::Schema and Dry::Validation (require
// "dry/validation", "dry/schema"). Dry::Schema.Params/JSON builds a standalone
// schema; Dry::Validation::Contract is a subclassable base whose `schema` and
// `rule` class methods accumulate a schema and rule bodies, and whose instances'
// #call validates an input Hash into a Result. Both pin go-ruby-dry-types.
func (vm *VM) registerDryValidation() {
	dry := vm.dryModule()

	// --- Dry::Schema.Params / .JSON ---
	schema := newClass("Dry::Schema", vm.cObject)
	schema.isModule = true
	dry.consts["Schema"] = schema
	vm.consts["Dry::Schema"] = schema

	// The value objects a schema block/result surface report these classes.
	params := newClass("Dry::Schema::Params", vm.cObject)
	schema.consts["Params"] = params
	vm.consts["Dry::Schema::Params"] = params

	schema.smethods["Params"] = &Method{name: "Params", owner: schema,
		native: func(vm *VM, _ object.Value, _ []object.Value, blk *Proc) object.Value {
			return &DrySchema{s: dryvalidation.Params(vm.drySchemaBuild(blk))}
		}}
	schema.smethods["JSON"] = &Method{name: "JSON", owner: schema,
		native: func(vm *VM, _ object.Value, _ []object.Value, blk *Proc) object.Value {
			return &DrySchema{s: dryvalidation.JSON(vm.drySchemaBuild(blk))}
		}}

	vm.registerDrySchemaSurface()

	// --- Dry::Validation ---
	val := newClass("Dry::Validation", vm.cObject)
	val.isModule = true
	dry.consts["Validation"] = val
	vm.consts["Dry::Validation"] = val

	contract := newClass("Dry::Validation::Contract", vm.cObject)
	val.consts["Contract"] = contract
	vm.consts["Dry::Validation::Contract"] = contract

	vm.registerDryContractClass(contract)
	vm.registerDryValidationResult(val)
	vm.registerDryRuleContext(val)
}

// drySchemaBuild runs a schema DSL block against a fresh Builder and returns the
// build closure the library's Params/JSON constructor consumes. A nil block
// yields an empty schema.
func (vm *VM) drySchemaBuild(blk *Proc) func(*dryvalidation.Builder) {
	return func(b *dryvalidation.Builder) {
		if blk == nil {
			return
		}
		vm.callBlockSelf(blk, &DrySchemaBuilder{b: b}, nil)
	}
}

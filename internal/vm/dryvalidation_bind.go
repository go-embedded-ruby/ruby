// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	drytypes "github.com/go-ruby-dry-types/dry-types"
	dryvalidation "github.com/go-ruby-dry-validation/dry-validation"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file wires the Dry::Schema DSL receiver (required/optional + the key
// macros), the Dry::Validation::Contract class methods (schema/rule) and #call,
// and the Result surface, onto the go-ruby-dry-validation library. The schema's
// macro/predicate evaluation is deterministic Go; each contract rule body is a
// compiled Ruby block rbgo runs against a RuleContext seam.

// registerDrySchemaSurface installs the DSL receiver methods (required/optional)
// and the DryKey macro methods (filled/maybe/value/array/hash).
func (vm *VM) registerDrySchemaSurface() {
	dsl := newClass("Dry::Schema::DSL", vm.cObject)
	vm.consts["Dry::Schema::DSL"] = object.Wrap(dsl)
	bd := func(name string, fn NativeFn) { dsl.define(name, fn) }
	bd("required", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Wrap(&DryKey{k: object.Kind[*DrySchemaBuilder](self).b.Required(drySym(args))})
	})
	bd("optional", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Wrap(&DryKey{k: object.Kind[*DrySchemaBuilder](self).b.Optional(drySym(args))})
	})

	key := newClass("Dry::Schema::Key", vm.cObject)
	vm.consts["Dry::Schema::Key"] = object.Wrap(key)
	kd := func(name string, fn NativeFn) { key.define(name, fn) }
	kd("filled", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		tn, preds := dryTypeAndPreds(args)
		object.Kind[*DryKey](self).k.Filled(tn, preds...)
		return self
	})
	kd("maybe", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		tn, preds := dryTypeAndPreds(args)
		object.Kind[*DryKey](self).k.Maybe(tn, preds...)
		return self
	})
	kd("value", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		tn, preds := dryTypeAndPreds(args)
		object.Kind[*DryKey](self).k.Value(tn, preds...)
		return self
	})
	kd("array", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		if blk != nil {
			object.Kind[*DryKey](self).k.ArrayOf(func(b *dryvalidation.Builder) {
				vm.callBlockSelf(blk, object.Wrap(&DrySchemaBuilder{b: b}), nil)
			})
			return self
		}
		tn, preds := dryTypeAndPreds(args)
		object.Kind[*DryKey](self).k.Array(tn, preds...)
		return self
	})
	kd("hash", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("ArgumentError", "no block given")
		}
		object.Kind[*DryKey](self).k.Hash(func(b *dryvalidation.Builder) {
			vm.callBlockSelf(blk, object.Wrap(&DrySchemaBuilder{b: b}), nil)
		})
		return self
	})
	kd("schema", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("ArgumentError", "no block given")
		}
		object.Kind[*DryKey](self).k.Schema(func(b *dryvalidation.Builder) {
			vm.callBlockSelf(blk, object.Wrap(&DrySchemaBuilder{b: b}), nil)
		})
		return self
	})

	// Dry::Schema::Params#call applies the schema to an input hash.
	pd := object.Kind[*RClass](vm.consts["Dry::Schema::Params"])
	pd.define("call", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return object.Wrap(&DryValidationResult{r: object.Kind[*DrySchema](self).s.Call(dryToGo(args[0]))})
	})
}

// registerDryContractClass installs the Contract base: the `schema` and `rule`
// class methods (accumulating into the subclass's meta), `new` (materialising the
// library Contract), and instance `#call`.
func (vm *VM) registerDryContractClass(contract *RClass) {
	sdef := func(name string, fn NativeFn) {
		contract.smethods[name] = &Method{name: name, owner: contract, native: fn}
	}

	// schema { ... } builds the contract's schema from a Params-style block.
	sdef("schema", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		cls := object.Kind[*RClass](self)
		m := vm.dryContractMeta(cls)
		m.schema = dryvalidation.Params(vm.drySchemaBuild(blk))
		return object.NilVal()
	})
	sdef("params", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		cls := object.Kind[*RClass](self)
		m := vm.dryContractMeta(cls)
		m.schema = dryvalidation.Params(vm.drySchemaBuild(blk))
		return object.NilVal()
	})

	// rule(*keys) { ... } records a custom rule body run after the schema.
	sdef("rule", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("ArgumentError", "no block given")
		}
		cls := object.Kind[*RClass](self)
		m := vm.dryContractMeta(cls)
		keys := make([]drytypes.Symbol, 0, len(args))
		for _, a := range args {
			keys = append(keys, drytypes.Symbol(dryKeyName(a)))
		}
		m.rules = append(m.rules, dryContractRule{keys: keys, body: blk})
		return object.NilVal()
	})

	sdef("new", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		cls := object.Kind[*RClass](self)
		m := vm.dryContractMeta(cls)
		sc := m.schema
		if sc == nil {
			sc = dryvalidation.Params(func(*dryvalidation.Builder) {})
		}
		c := dryvalidation.NewContract(sc)
		for _, r := range m.rules {
			rule := r // capture
			body := func(rc *dryvalidation.RuleContext) {
				vm.callBlockSelf(rule.body, vm.dryRuleContext(rc), nil)
			}
			if len(rule.keys) == 0 {
				c.RuleBase(body)
			} else {
				c.Rule(body, rule.keys...)
			}
		}
		return object.Wrap(&DryContract{c: c, cls: cls})
	})

	contract.define("call", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return object.Wrap(&DryValidationResult{r: object.Kind[*DryContract](self).c.Call(dryToGo(args[0]))})
	})
}

// dryContractMeta returns cls's accumulating contract meta, creating it on first
// use (a subclass inherits nothing here — each Contract declares its own schema).
func (vm *VM) dryContractMeta(cls *RClass) *dryContractMeta {
	if m, ok := object.KindOK[*dryContractMeta](cls.ivars[dryContractMetaIvar]); ok {
		return m
	}
	m := &dryContractMeta{}
	if cls.ivars == nil {
		cls.ivars = map[string]object.Value{}
	}
	cls.ivars[dryContractMetaIvar] = object.Wrap(m)
	return m
}

// registerDryValidationResult installs the Result surface both a schema and a
// contract #call return: success?/failure?/errors/to_h/messages.
func (vm *VM) registerDryValidationResult(val *RClass) {
	cls := newClass("Dry::Validation::Result", vm.cObject)
	val.consts["Result"] = object.Wrap(cls)
	vm.consts["Dry::Validation::Result"] = object.Wrap(cls)

	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	d("success?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.BoolValue(bool(object.Bool(object.Kind[*DryValidationResult](self).r.Success())))
	})
	d("failure?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.BoolValue(bool(object.Bool(!object.Kind[*DryValidationResult](self).r.Success())))
	})
	d("errors", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return dryMapToHash(vm, object.Kind[*DryValidationResult](self).r.Errors())
	})
	d("to_h", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return dryMapToHash(vm, object.Kind[*DryValidationResult](self).r.ToH())
	})
	d("output", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return dryMapToHash(vm, object.Kind[*DryValidationResult](self).r.Output())
	})
	d("messages", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		msgs := object.Kind[*DryValidationResult](self).r.Messages()
		arr := object.NewArrayFromSlice(make([]object.Value, len(msgs)))
		for i, m := range msgs {
			arr.Elems[i] = object.Wrap(object.NewString(m.Text))
		}
		return object.Wrap(arr)
	})
}

// dryRuleContext builds the Ruby self a rule body runs against: a shell exposing
// key(...)/value(...) reads and key(...).failure(text) / base.failure(text).
func (vm *VM) dryRuleContext(rc *dryvalidation.RuleContext) object.Value {
	return object.Wrap(&DryRuleCtx{rc: rc})
}

// drySym reads the first argument of required/optional as a Symbol name.
func drySym(args []object.Value) drytypes.Symbol {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
	}
	return drytypes.Symbol(dryKeyName(args[0]))
}

// dryTypeAndPreds splits a macro's arguments into the type name and its
// predicates: the first Symbol/String is the type name (default "" = any), and a
// trailing Hash of { predicate => arg } supplies the predicates (min_size?,
// format?, gt?, …).
func dryTypeAndPreds(args []object.Value) (string, []dryvalidation.Predicate) {
	tn := ""
	var preds []dryvalidation.Predicate
	for _, a := range args {
		{
			__sw53 := a
			switch {
			case object.IsKind[object.Symbol](__sw53):
				v := object.Kind[object.Symbol](__sw53)
				_ = v
				if tn == "" {
					tn = string(v)
				}
			case object.IsKind[*object.String](__sw53):
				v := object.Kind[*object.String](__sw53)
				_ = v
				if tn == "" {
					tn = v.Str()
				}
			case object.IsKind[*object.Hash](__sw53):
				v := object.Kind[*object.Hash](__sw53)
				_ = v
				for _, k := range v.Keys {
					val, _ := v.Get(k)
					name := dryKeyName(k)
					preds = append(preds, dryvalidation.Predicate{Name: dryPredName(name), Arg: dryToGo(val)})
				}
			}
		}
	}
	return tn, preds
}

// dryPredName strips a trailing "?" from a predicate name (the gem writes
// `min_size?: 3`; the library expects "min_size").
func dryPredName(name string) string {
	if n := len(name); n > 0 && name[n-1] == '?' {
		return name[:n-1]
	}
	return name
}

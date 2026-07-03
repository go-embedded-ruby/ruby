// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	drytypes "github.com/go-ruby-dry-types/dry-types"
	dryvalidation "github.com/go-ruby-dry-validation/dry-validation"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// DryRuleCtx is the self a Dry::Validation contract rule body runs against. It
// wraps the library's *RuleContext seam: value(:key) reads a coerced value, and
// key(:key).failure(text) / base.failure(text) records a failure. The library
// evaluates the schema; this shell only lets the Ruby rule inspect values and
// register failures.
type DryRuleCtx struct{ rc *dryvalidation.RuleContext }

func (c *DryRuleCtx) ToS() string     { return "#<Dry::Validation::Rule>" }
func (c *DryRuleCtx) Inspect() string { return c.ToS() }
func (c *DryRuleCtx) Truthy() bool    { return true }

// DryRuleKey is the target key(:name).failure(text) records against. base is set
// for the base(...) target; when path is nil and base is false the target is the
// rule's own default key path (the no-argument key.failure form).
type DryRuleKey struct {
	rc   *dryvalidation.RuleContext
	path []any
	base bool
}

func (k *DryRuleKey) ToS() string     { return "#<Dry::Validation::RuleKey>" }
func (k *DryRuleKey) Inspect() string { return k.ToS() }
func (k *DryRuleKey) Truthy() bool    { return true }

// registerDryRuleContext installs the rule-body surface: value/values reads and
// the key/base failure targets.
func (vm *VM) registerDryRuleContext(val *RClass) {
	ctx := newClass("Dry::Validation::Rule", vm.cObject)
	val.consts["Rule"] = object.Wrap(ctx)
	vm.consts["Dry::Validation::Rule"] = object.Wrap(ctx)
	d := func(name string, fn NativeFn) { ctx.define(name, fn) }

	// value(:key) returns the coerced value for key, or nil when absent.
	d("value", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		v, ok := object.Kind[*DryRuleCtx](self).rc.Value(drytypes.Symbol(dryKeyName(args[0])))
		if !ok {
			return object.NilVal()
		}
		return dryFromGo(vm, v)
	})
	// values returns the full coerced input hash.
	d("values", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return dryMapToHash(vm, object.Kind[*DryRuleCtx](self).rc.Values())
	})
	// key(:name) targets an explicit key for a failure; key with no argument
	// targets the rule's own default key path (the gem's `key.failure`).
	d("key", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		rc := object.Kind[*DryRuleCtx](self).rc
		if len(args) > 0 && !object.IsNil(args[0]) {
			return object.Wrap(&DryRuleKey{rc: rc, path: []any{drytypes.Symbol(dryKeyName(args[0]))}})
		}
		return object.Wrap(&DryRuleKey{rc: rc})
	})
	// base targets a base (whole-input) failure.
	d("base", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Wrap(&DryRuleKey{rc: object.Kind[*DryRuleCtx](self).rc, base: true})
	})
	// failure(text) with no key targets the rule's default key failure directly.
	d("failure", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		object.Kind[*DryRuleCtx](self).rc.KeyFailure(dryFailureText(args))
		return object.NilVal()
	})

	keyCls := newClass("Dry::Validation::RuleKey", vm.cObject)
	val.consts["RuleKey"] = object.Wrap(keyCls)
	vm.consts["Dry::Validation::RuleKey"] = object.Wrap(keyCls)
	keyCls.define("failure", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		k := object.Kind[*DryRuleKey](self)
		text := dryFailureText(args)
		switch {
		case k.base:
			k.rc.BaseFailure(text)
		case k.path == nil:
			k.rc.KeyFailure(text)
		default:
			k.rc.KeyFailureAt(k.path, text)
		}
		return object.NilVal()
	})
}

// dryFailureText reads a failure(...) argument as its message string.
func dryFailureText(args []object.Value) string {
	if len(args) == 0 {
		return ""
	}
	if s, ok := object.KindOK[*object.String](args[0]); ok {
		return s.Str()
	}
	return args[0].ToS()
}

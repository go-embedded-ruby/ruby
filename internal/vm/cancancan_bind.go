// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-ruby-cancancan/cancancan"
)

// cancanAbilityBox stashes the per-including-object *cancancan.Ability plus the
// Ruby condition blocks registered for its rules (keyed by the rule ID the
// engine returns from Can/Cannot). It lives in the receiver's @__cancan_ability
// ivar and is never user-visible.
type cancanAbilityBox struct {
	ab     *cancancan.Ability
	blocks map[int]*Proc
}

func (b *cancanAbilityBox) ToS() string     { return "#<CanCan::Ability state>" }
func (b *cancanAbilityBox) Inspect() string { return "#<CanCan::Ability state>" }
func (b *cancanAbilityBox) Truthy() bool    { return true }

// cancanAbility returns self's rule store, creating (and wiring the AttrGet /
// BlockEval seams onto) it on first use. AttrGet reads an attribute via rbgo
// dispatch; BlockEval runs the stored Ruby condition proc for a rule.
func (vm *VM) cancanAbility(self object.Value) *cancanAbilityBox {
	if b, ok := getIvar(self, "@__cancan_ability").(*cancanAbilityBox); ok {
		return b
	}
	b := &cancanAbilityBox{ab: cancancan.New(), blocks: map[int]*Proc{}}
	b.ab.AttrGet = func(subject any, key string) any {
		return vm.send(cancanUnwrap(subject), key, nil, nil)
	}
	b.ab.BlockEval = func(ruleID int, subject any) bool {
		return vm.callBlock(b.blocks[ruleID], []object.Value{cancanUnwrap(subject)}).Truthy()
	}
	setIvar(self, "@__cancan_ability", b)
	return b
}

// cancanAddRule adds a can (base=true) or cannot (base=false) rule from the Ruby
// arguments: action, subject, an optional trailing conditions Hash, and an
// optional &block. A block is registered under the rule's ID so BlockEval can
// reach it.
func (vm *VM) cancanAddRule(self object.Value, base bool, args []object.Value, blk *Proc) object.Value {
	if len(args) < 2 {
		raise("ArgumentError", "wrong number of arguments (given %d, expected 2+)", len(args))
	}
	b := vm.cancanAbility(self)
	action := cancanAction(args[0])
	subject := vm.cancanSubjectDecl(args[1])
	var conds []any
	if h, ok := lastHash(args); ok && len(args) > 2 {
		conds = append(conds, cancanConditions(h))
	}
	if blk != nil {
		conds = append(conds, cancancan.Block{})
	}
	var id int
	if base {
		id = b.ab.Can(action, subject, conds...)
	} else {
		id = b.ab.Cannot(action, subject, conds...)
	}
	if blk != nil {
		b.blocks[id] = blk
	}
	return object.NilV
}

// cancanQuery decodes a can? / cannot? / authorize! call into a concrete action
// and the subject the engine matches against (a Class token for a class-level
// check, else a Classified wrapper over the Ruby instance).
func (vm *VM) cancanQuery(args []object.Value) (cancancan.Action, any) {
	if len(args) < 2 {
		raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
	}
	return cancanQueryAction(args[0]), vm.cancanSubjectQuery(args[1])
}

// cancanAliasAction implements alias_action(*actions, to: target): the trailing
// Hash carries the :to target, the leading arguments are the aliased actions.
func (vm *VM) cancanAliasAction(self object.Value, args []object.Value) object.Value {
	h, ok := lastHash(args)
	if !ok || len(args) < 2 {
		raise("ArgumentError", "alias_action requires a to: target")
	}
	toV, ok := h.Get(object.Symbol("to"))
	if !ok {
		raise("ArgumentError", "alias_action requires a to: target")
	}
	to := cancanQueryAction(toV)
	acts := make([]cancancan.Action, 0, len(args)-1)
	for _, a := range args[:len(args)-1] {
		acts = append(acts, cancanQueryAction(a))
	}
	vm.cancanAbility(self).ab.AliasAction(to, acts...)
	return object.NilV
}

// cancanAction converts a Ruby can/cannot action argument into the engine's
// action form: :manage is the wildcard, a Symbol/String is a named action, and
// an Array is several of these.
func cancanAction(v object.Value) any {
	switch x := v.(type) {
	case object.Symbol:
		if string(x) == "manage" {
			return cancancan.Manage
		}
		return cancancan.Action(string(x))
	case *object.String:
		return cancancan.Action(x.Str())
	case *object.Array:
		acts := make([]any, len(x.Elems))
		for i, e := range x.Elems {
			acts[i] = cancanAction(e)
		}
		return acts
	default:
		raise("ArgumentError", "action must be a Symbol, String, or Array")
		return nil
	}
}

// cancanQueryAction converts a can? / authorize! / alias_action action into a
// single concrete Action.
func cancanQueryAction(v object.Value) cancancan.Action {
	switch x := v.(type) {
	case object.Symbol:
		return cancancan.Action(string(x))
	case *object.String:
		return cancancan.Action(x.Str())
	default:
		raise("TypeError", "action must be a Symbol or String")
		return ""
	}
}

// cancanSubjectDecl converts a can/cannot subject argument: :all is the wildcard;
// anything else is a class/symbol/instance subject.
func (vm *VM) cancanSubjectDecl(v object.Value) any {
	if s, ok := v.(object.Symbol); ok && string(s) == "all" {
		return cancancan.All
	}
	return vm.cancanSubjectValue(v)
}

// cancanSubjectQuery converts a can? / authorize! subject argument into the form
// the engine matches: a Class token (class-level check) or a Classified wrapper
// over the Ruby instance.
func (vm *VM) cancanSubjectQuery(v object.Value) any {
	return vm.cancanSubjectValue(v)
}

// cancanSubjectValue maps a Ruby subject onto the engine representation: a Class
// is a class token, a Symbol is a class token by its name, an Array is several
// subjects, and any other object is a Classified instance wrapper (matched
// against Class rules by its class ancestry, and against instance rules by
// value).
func (vm *VM) cancanSubjectValue(v object.Value) any {
	switch x := v.(type) {
	case *RClass:
		return cancancan.Class(x.name)
	case object.Symbol:
		return cancancan.Class(string(x))
	case *object.Array:
		subs := make([]any, len(x.Elems))
		for i, e := range x.Elems {
			subs[i] = vm.cancanSubjectValue(e)
		}
		return subs
	default:
		return cancanInstance{vm: vm, val: v}
	}
}

// cancanConditions converts a Ruby conditions Hash into the engine's
// map[string]any: symbol/string keys become names and values are kept as Ruby
// values (nested hashes and arrays recurse) so hash-condition matching compares
// them against AttrGet results.
func cancanConditions(h *object.Hash) map[string]any {
	m := make(map[string]any, len(h.Keys))
	for _, k := range h.Keys {
		val, _ := h.Get(k)
		m[cancanCondKey(k)] = cancanCondVal(val)
	}
	return m
}

// cancanCondKey renders a conditions-hash key (a Symbol or String) as its name.
func cancanCondKey(k object.Value) string {
	switch key := k.(type) {
	case object.Symbol:
		return string(key)
	case *object.String:
		return key.Str()
	default:
		raise("ArgumentError", "condition key must be a Symbol or String")
		return ""
	}
}

// cancanCondVal keeps a condition value as a Ruby value, recursing into nested
// hashes (associated-attribute matching) and arrays (membership matching).
func cancanCondVal(v object.Value) any {
	switch x := v.(type) {
	case *object.Hash:
		return cancanConditions(x)
	case *object.Array:
		arr := make([]any, len(x.Elems))
		for i, e := range x.Elems {
			arr[i] = cancanCondVal(e)
		}
		return arr
	default:
		return v
	}
}

// cancanInstance wraps a Ruby object as a cancancan.Classified subject: it
// reports the object's class name and ancestor class names so a rule declared on
// a class (or a superclass) matches an instance of it, and — being a value with
// the underlying object.Value — matches an instance rule by DeepEqual.
type cancanInstance struct {
	vm  *VM
	val object.Value
}

func (c cancanInstance) CanCanClass() cancancan.Class {
	return cancancan.Class(c.vm.classOf(c.val).name)
}

func (c cancanInstance) CanCanAncestors() []cancancan.Class {
	anc := c.vm.ancestors(c.vm.classOf(c.val))
	out := make([]cancancan.Class, len(anc))
	for i, a := range anc {
		out[i] = cancancan.Class(a.name)
	}
	return out
}

// cancanUnwrap returns the underlying Ruby object for a subject the engine hands
// to the AttrGet / BlockEval seams: the wrapped value of a cancanInstance, or an
// already-bare object.Value (reached when a nested condition recurses on an
// attribute).
func cancanUnwrap(subject any) object.Value {
	if c, ok := subject.(cancanInstance); ok {
		return c.val
	}
	if v, ok := subject.(object.Value); ok {
		return v
	}
	return object.NilV
}

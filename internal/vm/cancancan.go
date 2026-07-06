// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerCanCanCan installs the CanCan::Ability mixin (require "cancancan" /
// "cancan"): the rule-DSL a class picks up with `include CanCan::Ability` —
// can / cannot to declare permission rules, can? / cannot? to query them,
// authorize! to enforce them (raising CanCan::AccessDenied on denial),
// alias_action to expand an action to the actions it grants — plus the
// CanCan::Error hierarchy.
//
// The rule store and the whole matching engine (reverse-definition precedence,
// :manage / :all wildcards, action-alias expansion, hash- and block-condition
// evaluation) live in the github.com/go-ruby-cancancan/cancancan library. This
// file is the thin shell mapping rbgo's Symbol/Class/Hash/Proc model onto that
// engine's *Ability and wiring the two Ruby-specific parts it leaves out as
// injectable seams:
//
//   - AttrGet(subject, key) reads an attribute for hash-condition matching — it
//     stands in for `subject.send(key)` and is wired to rbgo method dispatch.
//   - BlockEval(ruleID, subject) runs a rule's Ruby condition block against an
//     instance — it stands in for `block.call(subject)` and is wired to rbgo
//     calling the stored Proc.
//
// The per-including-object rule store is stashed lazily in the receiver's
// @__cancan_ability ivar, boxed in a cancanAbilityBox so it survives across
// method calls but is never user-visible.
func (vm *VM) registerCanCanCan() {
	canMod := newClass("CanCan", nil)
	canMod.isModule = true
	vm.consts["CanCan"] = canMod

	ability := newClass("CanCan::Ability", nil)
	ability.isModule = true
	canMod.consts["Ability"] = ability
	vm.consts["CanCan::Ability"] = ability

	vm.registerCanCanErrors(canMod)

	// can(action, subject, conditions = nil, &block): add a permission rule.
	ability.define("can", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		return vm.cancanAddRule(self, true, args, blk)
	})
	// cannot(action, subject, conditions = nil, &block): add a revocation rule; a
	// matching cannot overrides an earlier matching can.
	ability.define("cannot", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		return vm.cancanAddRule(self, false, args, blk)
	})

	// can?(action, subject): does the ability permit action on subject?
	ability.define("can?", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		action, subject := vm.cancanQuery(args)
		return object.Bool(vm.cancanAbility(self).ab.CanQ(action, subject))
	})
	// cannot?(action, subject): the negation of can?.
	ability.define("cannot?", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		action, subject := vm.cancanQuery(args)
		return object.Bool(vm.cancanAbility(self).ab.CannotQ(action, subject))
	})

	// authorize!(action, subject): return subject when permitted, else raise
	// CanCan::AccessDenied (mirroring the gem, which raises on denial).
	ability.define("authorize!", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		action, subject := vm.cancanQuery(args)
		if err := vm.cancanAbility(self).ab.AuthorizeBang(action, subject); err != nil {
			return raise("CanCan::AccessDenied", "%s", err.Error())
		}
		return args[1]
	})

	// alias_action(*actions, to: target): declare that granting target also
	// grants actions (the gem's alias_action).
	ability.define("alias_action", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.cancanAliasAction(self, args)
	})
}

// registerCanCanErrors installs the CanCan::Error subtree mirroring the gem:
// CanCan::Error < StandardError, and AccessDenied and AuthorizationNotPerformed
// beneath it. Each is registered both scoped (under CanCan) and flat in
// vm.consts so `raise` can find it by its qualified name and
// `rescue CanCan::AccessDenied` works.
func (vm *VM) registerCanCanErrors(mod *RClass) {
	std := vm.consts["StandardError"].(*RClass)
	base := newClass("CanCan::Error", std)
	mod.consts["Error"] = base
	vm.consts["CanCan::Error"] = base

	def := func(simple, qualified string) {
		c := newClass(qualified, base)
		mod.consts[simple] = c
		vm.consts[qualified] = c
	}
	def("AccessDenied", "CanCan::AccessDenied")
	def("AuthorizationNotPerformed", "CanCan::AuthorizationNotPerformed")
}

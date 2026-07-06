// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerPundit installs the Pundit mixin (require "pundit"): the
// `include Pundit` / `include Pundit::Authorization` authorization surface —
// the instance helpers authorize / policy / policy_scope, the module methods
// Pundit.authorize / Pundit.policy / Pundit.policy! / Pundit.policy_scope /
// Pundit.policy_scope!, and the Pundit::Error hierarchy.
//
// The parts that are pure logic — resolving a record to its `<Record>Policy`
// and `<Record>Policy::Scope` class names, the authorize/policy protocol and
// its return contract, and the error tree — live in the
// github.com/go-ruby-pundit/pundit engine. This file is the thin shell that
// maps rbgo's object model onto that engine and wires the one Ruby-specific
// part the library leaves out as an injectable seam:
//
//   - Dispatch(policyClass, query, user, record) — instantiates the resolved
//     policy class with (user, record) and sends it the query predicate
//     (e.g. `update?`) through rbgo method dispatch, returning its truthiness.
//   - ResolveScope(scopeClass, user, scope) — instantiates the scope class with
//     (user, scope) and sends it `resolve`.
//   - Defined(className) — models Pundit's safe_constantize: a top-level (or
//     namespaced) constant lookup reporting whether a policy/scope *class* of
//     the derived name exists, so a missing policy raises NotDefinedError rather
//     than crashing.
//
// The record the host passes is reflected into a pundit.Subject so the engine's
// pure naming logic (PostPolicy, Admin::PostPolicy, BlogPostPolicy…) needs no
// Ruby runtime; the underlying Ruby object rides along in Subject.Ruby for the
// seams to reach.
func (vm *VM) registerPundit() {
	mod := newClass("Pundit", nil)
	mod.isModule = true
	vm.consts["Pundit"] = mod

	// Pundit::Authorization is the modern include target (Pundit 2.x); point it
	// at the same module so both `include Pundit` and
	// `include Pundit::Authorization` bring in the instance helpers.
	mod.consts["Authorization"] = mod
	vm.consts["Pundit::Authorization"] = mod

	vm.registerPunditErrors(mod)

	// authorize(record, query = nil): resolve record's policy, dispatch the query
	// predicate and — following Pundit#authorize — return the record on success,
	// or raise Pundit::NotAuthorizedError on a false result (Pundit::NotDefinedError
	// when no policy exists). The user is pundit_user (falling back to
	// current_user); when query is omitted it is derived from action_name, exactly
	// as the controller mixin does.
	mod.define("authorize", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		query := vm.punditQuery(self, args)
		user := vm.punditUser(self)
		sub := vm.punditSubject(args[0])
		model, err := vm.punditEngine().Authorize(user, sub, query)
		if err != nil {
			return vm.raisePunditErr(err)
		}
		return punditRubyOf(model)
	})

	// policy(record): the policy instance for record (Pundit#policy), or nil when
	// no policy class is defined.
	mod.define("policy", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return vm.punditPolicy(vm.punditUser(self), args[0], false)
	})

	// policy_scope(scope): the resolved scope for scope (Pundit#policy_scope), or
	// nil when no scope class is defined.
	mod.define("policy_scope", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return vm.punditPolicyScope(vm.punditUser(self), args[0], false)
	})

	vm.registerPunditModuleMethods(mod)
}

// registerPunditModuleMethods installs the Pundit.* module functions — the
// explicit-user forms used outside a controller: authorize, policy, policy!,
// policy_scope and policy_scope!.
func (vm *VM) registerPunditModuleMethods(mod *RClass) {
	sm := func(name string, fn NativeFn) {
		mod.smethods[name] = &Method{name: name, owner: mod, native: fn}
	}

	// Pundit.authorize(user, record, query): the explicit-user authorize,
	// returning the record on success or raising as the instance form does.
	sm("authorize", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 3 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 3)", len(args))
		}
		sub := vm.punditSubject(args[1])
		model, err := vm.punditEngine().Authorize(args[0], sub, punditNameArg(args[2]))
		if err != nil {
			return vm.raisePunditErr(err)
		}
		return punditRubyOf(model)
	})

	// Pundit.policy(user, record): the policy instance, or nil when undefined.
	sm("policy", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		return vm.punditPolicy(args[0], args[1], false)
	})

	// Pundit.policy!(user, record): the policy instance, raising
	// Pundit::NotDefinedError when no policy class exists.
	sm("policy!", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		return vm.punditPolicy(args[0], args[1], true)
	})

	// Pundit.policy_scope(user, scope): the resolved scope, or nil when undefined.
	sm("policy_scope", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		return vm.punditPolicyScope(args[0], args[1], false)
	})

	// Pundit.policy_scope!(user, scope): the resolved scope, raising
	// Pundit::NotDefinedError when no scope class exists.
	sm("policy_scope!", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		return vm.punditPolicyScope(args[0], args[1], true)
	})
}

// registerPunditErrors installs the Pundit::Error subtree mirroring the gem:
// Pundit::Error < StandardError, and NotAuthorizedError, NotDefinedError,
// AuthorizationNotPerformedError and PolicyScopingNotPerformedError beneath it
// (the last a subclass of AuthorizationNotPerformedError, as in the gem). Each
// is registered both scoped (under Pundit) and flat in vm.consts so `raise` can
// find it by its qualified name and `rescue Pundit::NotAuthorizedError` works.
func (vm *VM) registerPunditErrors(mod *RClass) {
	std := vm.consts["StandardError"].(*RClass)
	base := newClass("Pundit::Error", std)
	mod.consts["Error"] = base
	vm.consts["Pundit::Error"] = base

	def := func(simple, qualified string, super *RClass) *RClass {
		c := newClass(qualified, super)
		mod.consts[simple] = c
		vm.consts[qualified] = c
		return c
	}
	def("NotAuthorizedError", "Pundit::NotAuthorizedError", base)
	def("NotDefinedError", "Pundit::NotDefinedError", base)
	anp := def("AuthorizationNotPerformedError", "Pundit::AuthorizationNotPerformedError", base)
	def("PolicyScopingNotPerformedError", "Pundit::PolicyScopingNotPerformedError", anp)
}

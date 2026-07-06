// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"strings"

	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-ruby-pundit/pundit"
)

// punditEngine builds a pundit.Engine whose seams are wired to rbgo dispatch.
// The engine itself is stateless (all state is the Ruby program's), so a fresh
// one per call is cheap and keeps every seam a closure over the live VM.
//
//   - Dispatch instantiates the resolved policy class with (user, record) and
//     sends it the query predicate, returning its truthiness. A Ruby exception
//     raised inside the predicate propagates as a Ruby raise (the panic unwinds
//     through the engine untouched), not a Go error.
//   - ResolveScope instantiates the scope class with (user, scope) and sends it
//     `resolve`.
//   - Defined models safe_constantize: the derived name must resolve to a real
//     Ruby class (a policy/scope class), else the bang resolvers raise
//     NotDefinedError.
func (vm *VM) punditEngine() *pundit.Engine {
	return &pundit.Engine{
		Dispatch: func(policyClass, query string, user, record any) (bool, error) {
			inst := vm.punditNew(policyClass, user, record)
			return vm.send(inst, query, nil, nil).Truthy(), nil
		},
		ResolveScope: func(scopeClass string, user, scope any) (any, error) {
			inst := vm.punditNew(scopeClass, user, scope)
			return vm.send(inst, "resolve", nil, nil), nil
		},
		Defined: func(className string) bool {
			v, ok := vm.constByName(className)
			if !ok {
				return false
			}
			_, isClass := v.(*RClass)
			return isClass
		},
	}
}

// punditNew instantiates className (already confirmed defined by the Defined
// seam) with (user, record) via Ruby's `new`, the policy/scope object Pundit
// dispatches against.
func (vm *VM) punditNew(className string, user, record any) object.Value {
	cls, _ := vm.constByName(className)
	return vm.send(cls.(*RClass), "new", []object.Value{punditVal(user), punditRubyOf(record)}, nil)
}

// punditPolicy returns the policy instance for record under user (Pundit#policy /
// Pundit.policy). When no policy class is defined it returns nil, or — for the
// bang form — raises Pundit::NotDefinedError.
func (vm *VM) punditPolicy(user, record object.Value, bang bool) object.Value {
	sub := vm.punditSubject(record)
	name, err := vm.punditEngine().PolicyBang(sub)
	if err != nil {
		if bang {
			return vm.raisePunditErr(err)
		}
		return object.NilV
	}
	return vm.punditNew(name, user, sub)
}

// punditPolicyScope returns the resolved scope for scope under user
// (Pundit#policy_scope / Pundit.policy_scope). When no scope class is defined it
// returns nil, or — for the bang form — raises Pundit::NotDefinedError.
func (vm *VM) punditPolicyScope(user, scope object.Value, bang bool) object.Value {
	sub := vm.punditSubject(scope)
	e := vm.punditEngine()
	var res any
	var err error
	if bang {
		res, err = e.PolicyScopeBang(user, sub)
	} else {
		res, err = e.PolicyScope(user, sub)
	}
	if err != nil {
		return vm.raisePunditErr(err)
	}
	if v, ok := res.(object.Value); ok {
		return v
	}
	return object.NilV
}

// punditUser derives the current user from the includer: pundit_user when it
// responds, else current_user, else nil — matching Pundit's pundit_user default.
func (vm *VM) punditUser(self object.Value) object.Value {
	if vm.respondsTo(self, "pundit_user") {
		return vm.send(self, "pundit_user", nil, nil)
	}
	if vm.respondsTo(self, "current_user") {
		return vm.send(self, "current_user", nil, nil)
	}
	return object.NilV
}

// punditQuery resolves the predicate to check: the explicit second argument when
// given, otherwise "#{action_name}?" derived from the includer (as Pundit's
// controller mixin does). Without an action_name to derive from, it raises
// ArgumentError.
func (vm *VM) punditQuery(self object.Value, args []object.Value) string {
	if len(args) > 1 && !object.IsNil(args[1]) {
		return punditNameArg(args[1])
	}
	if vm.respondsTo(self, "action_name") {
		return vm.send(self, "action_name", nil, nil).ToS() + "?"
	}
	raise("ArgumentError", "no query given and no action_name to derive one from")
	return ""
}

// punditSubject reflects a Ruby record into the pundit.Subject the engine's
// naming logic inspects, stashing the underlying Ruby object in Subject.Ruby for
// the seams. A Class subject is the class itself, a Symbol is camelized, an Array
// is a namespaced lookup ([:admin, post] -> Admin::PostPolicy), and any other
// object derives its policy from its class name — honouring a `policy_class`
// override when the object responds to it.
func (vm *VM) punditSubject(v object.Value) pundit.Subject {
	var s pundit.Subject
	switch r := v.(type) {
	case *RClass:
		s = pundit.Class(r.name)
	case object.Symbol:
		s = pundit.Symbol(string(r))
	case *object.Array:
		elems := make([]pundit.Subject, len(r.Elems))
		for i, e := range r.Elems {
			elems[i] = vm.punditSubject(e)
		}
		s = pundit.Array(elems...)
	default:
		s = pundit.Object(vm.classOf(v).name)
		if vm.respondsTo(v, "policy_class") {
			s.PolicyClass = vm.punditClassName(vm.send(v, "policy_class", nil, nil))
		}
	}
	s.Ruby = v
	return s
}

// punditClassName renders a policy_class override value (a Class, String or
// Symbol) as its class-name string.
func (vm *VM) punditClassName(v object.Value) string {
	switch c := v.(type) {
	case *RClass:
		return c.name
	case *object.String:
		return c.Str()
	case object.Symbol:
		return string(c)
	default:
		return v.ToS()
	}
}

// raisePunditErr maps an engine error onto the corresponding Pundit::Error and
// raises it as a Ruby exception.
func (vm *VM) raisePunditErr(err error) object.Value {
	switch e := err.(type) {
	case *pundit.NotAuthorizedError:
		return raise("Pundit::NotAuthorizedError", "%s", e.Msg)
	case *pundit.NotDefinedError:
		return raise("Pundit::NotDefinedError", "%s", e.Error())
	default:
		return raise("Pundit::Error", "%s", err.Error())
	}
}

// punditRubyOf extracts the underlying Ruby object from a value the engine hands
// back to a seam: a pundit.Subject (whose Ruby field carries it) or an already
// bare object.Value.
func punditRubyOf(v any) object.Value {
	if s, ok := v.(pundit.Subject); ok {
		if rv, ok := s.Ruby.(object.Value); ok {
			return rv
		}
		return object.NilV
	}
	return punditVal(v)
}

// punditVal narrows an `any` the engine round-trips (a user or record) back to a
// Ruby value, defaulting to nil.
func punditVal(v any) object.Value {
	if rv, ok := v.(object.Value); ok {
		return rv
	}
	return object.NilV
}

// punditNameArg coerces a query/name argument (a Symbol or String) to its text.
func punditNameArg(v object.Value) string {
	switch n := v.(type) {
	case object.Symbol:
		return string(n)
	case *object.String:
		return n.Str()
	default:
		raise("TypeError", "%s is not a symbol nor a string", v.Inspect())
		return ""
	}
}

// constByName resolves a possibly-namespaced constant name ("PostPolicy",
// "Admin::PostPolicy", "PostPolicy::Scope") to its value: the first segment from
// the top-level table, then each further segment from the preceding class's own
// constant table. This models the class lookup Pundit's safe_constantize does.
func (vm *VM) constByName(name string) (object.Value, bool) {
	parts := strings.Split(name, "::")
	cur, ok := vm.consts[parts[0]]
	if !ok {
		return nil, false
	}
	for _, p := range parts[1:] {
		cls, ok := cur.(*RClass)
		if !ok {
			return nil, false
		}
		nv, ok := cls.consts[p]
		if !ok {
			return nil, false
		}
		cur = nv
	}
	return cur, true
}

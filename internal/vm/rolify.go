// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	rolify "github.com/go-ruby-rolify/rolify"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file (with rolify_bind.go) binds github.com/go-ruby-rolify/rolify — the
// pure-Go (CGO=0) reimplementation of the deterministic core of Ruby's rolify
// gem — into rbgo (require "rolify"):
//
//	class User
//	  rolify
//	  attr_reader :id
//	  def initialize(id); @id = id; end
//	end
//
//	class Forum
//	  resourcify
//	  attr_reader :id
//	  def initialize(id); @id = id; end
//	end
//
//	user  = User.new(1)
//	forum = Forum.new(7)
//	user.add_role :admin                  # global role
//	user.add_role :moderator, Forum       # class-scoped role
//	user.add_role :owner, forum           # instance-scoped role
//	user.has_role? :admin                 # => true
//	user.has_role? :owner, :any           # => true
//	user.has_role? :moderator, forum      # => true (class role inherits)
//	forum.roles                           # instance-scoped roles on this forum
//	forum.applied_roles                   # global + class + instance
//	user.remove_role :admin
//
// The library owns everything above the database: the role value model, the
// scope-matching semantics (global/class/instance inheritance, the :any
// wildcard and the strict_rolify exact-match mode), the user-side predicates and
// the resource-side helpers. rbgo supplies the two seams the library leaves
// injectable:
//
//   - The persistence Store (rolify.Store: FindRole/CreateRole/AssignRole/
//     RemoveRole/RolesFor/AllRoles). This binding backs it with the library's
//     reference rolify.MemoryStore, held once per require: the roles table and
//     the users<->roles join live in Go memory for the lifetime of the VM. In
//     the gem the same seam is the `roles` table and the join; wiring it to an
//     ActiveRecord Role model + join table is a drop-in replacement of this one
//     Store value and is the binding's documented AR extension point.
//   - The Ruby<->value mapping: a role holder's identity is its class name plus
//     its `id` (read with send(obj, "id")), and a resource's scope is its class
//     name plus its `id`. A Class argument (add_role :mod, Forum) is a class
//     scope; a record argument (add_role :mod, @forum) is an instance scope;
//     the symbol :any is the wildcard query. Roles handed back to Ruby are
//     wrapped as Rolify::Role objects exposing name/resource_type/resource_id/id.

// rolifyRole wraps a library *rolify.Role for the Ruby side: user.roles /
// resource.applied_roles hand these back, and their instance methods (name /
// resource_type / resource_id / id) read the wrapped triple. cls is the
// Rolify::Role class the wrapper reports to classOf so those methods dispatch.
type rolifyRole struct {
	r   *rolify.Role
	cls *RClass
}

func (rr *rolifyRole) ToS() string {
	return "#<Rolify::Role name=" + rr.r.Name + ">"
}
func (rr *rolifyRole) Inspect() string { return rr.ToS() }
func (rr *rolifyRole) Truthy() bool    { return true }

// wrapRole boxes one library role as a Rolify::Role Ruby object.
func wrapRole(cls *RClass, r *rolify.Role) object.Value { return &rolifyRole{r: r, cls: cls} }

// wrapRoles boxes a slice of library roles as a Ruby Array of Rolify::Role.
func wrapRoles(cls *RClass, roles []*rolify.Role) object.Value {
	out := make([]object.Value, len(roles))
	for i, r := range roles {
		out[i] = wrapRole(cls, r)
	}
	return object.NewArray(out...)
}

// rolifyResourceID reads a role holder's / resource's identity the way the gem
// does — send(obj, "id") — and renders it as the string the Store keys on.
func (vm *VM) rolifyResourceID(obj object.Value) string {
	return vm.send(obj, "id", nil, nil).ToS()
}

// rolifyUserKey is the Store identity for a role holder: its class name plus its
// id, so two Ruby objects for the same record share roles while distinct records
// (or classes) stay separate.
func (vm *VM) rolifyUserKey(obj object.Value) string {
	return vm.classOf(obj).name + "#" + vm.rolifyResourceID(obj)
}

// rolifyScope maps a Ruby resource argument to a library Scope. Absent (handled
// by the caller) is Global; the symbol :any is the wildcard query (only where a
// query allows it); a Class is a class scope; any record is an instance scope
// keyed by its class name and id.
func (vm *VM) rolifyScope(arg object.Value, allowAny bool) rolify.Scope {
	if allowAny {
		if sym, ok := arg.(object.Symbol); ok && string(sym) == "any" {
			return rolify.Any()
		}
	}
	if cls, ok := arg.(*RClass); ok {
		return rolify.ClassScope(cls.ToS())
	}
	return rolify.InstanceScope(vm.classOf(arg).name, vm.rolifyResourceID(arg))
}

// rolifyScopeArgs turns a role method's trailing arguments (everything after the
// role name) into a Scope: none is Global, otherwise the first argument names
// the resource.
func (vm *VM) rolifyScopeArgs(args []object.Value, allowAny bool) rolify.Scope {
	if len(args) == 0 {
		return rolify.Global()
	}
	return vm.rolifyScope(args[0], allowAny)
}

// rolifyRoleName reads the (required) role name from a role method's arguments,
// raising ArgumentError when it is missing, mirroring the gem.
func rolifyRoleName(args []object.Value) string {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
	}
	return nameArg(args[0])
}

// rolifyQueries builds the query list for has_all_of_roles? / has_any_of_roles?.
// Each argument is either a bare role name (a global query) or a Hash with a
// :name and an optional :resource (Class / record / :any), mirroring the gem's
// accepted forms.
func (vm *VM) rolifyQueries(args []object.Value) []rolify.Query {
	qs := make([]rolify.Query, 0, len(args))
	for _, a := range args {
		if h, ok := a.(*object.Hash); ok {
			nameV, _ := h.Get(object.Symbol("name"))
			scope := rolify.Global()
			if res, ok := h.Get(object.Symbol("resource")); ok {
				scope = vm.rolifyScope(res, true)
			}
			qs = append(qs, rolify.Query{Name: nameArg(nameV), Scope: scope})
			continue
		}
		qs = append(qs, rolify.Query{Name: nameArg(a), Scope: rolify.Global()})
	}
	return qs
}

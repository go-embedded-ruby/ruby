// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	rolify "github.com/go-ruby-rolify/rolify"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerRolify records the require "rolify" feature hook. Nothing is installed
// eagerly: the hook (run once by doRequire on the first `require "rolify"`)
// creates the Rolify module, the Rolify::Role class and the rolify / resourcify
// class macros, mirroring the gem where the surface appears only when loaded.
func (vm *VM) registerRolify() {
	vm.featureHooks["rolify"] = vm.installRolify
}

// installRolify installs rolify's Ruby surface. A single library engine over a
// MemoryStore backs the whole VM: the store is the roles table + users<->roles
// join. rolify (a class macro) makes a class a role holder; resourcify makes a
// class a resource. Both close over the shared engine so every model sees the
// same roles.
func (vm *VM) installRolify() {
	engine := rolify.New(rolify.NewMemoryStore(), rolify.Cache(true))

	// User handles are memoised per identity so has_cached_role? keeps a stable
	// snapshot across calls (invalidated by add_role / remove_role), matching the
	// gem's per-record role cache.
	handles := map[string]*rolify.User{}
	handle := func(key string) *rolify.User {
		u, ok := handles[key]
		if !ok {
			u = engine.User(key)
			handles[key] = u
		}
		return u
	}

	mod := newClass("Rolify", nil)
	mod.isModule = true
	vm.consts["Rolify"] = mod

	roleCls := vm.installRolifyRoleClass(mod)

	// rolify — the class macro that turns a class into a role holder, mixing the
	// add_role / has_role? / remove_role / roles predicates into its instances.
	vm.cModule.define("rolify", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		vm.defineRolifyHolder(self.(*RClass), handle, roleCls)
		return object.NilV
	})

	// resourcify — the class macro that turns a class into a rolify resource,
	// mixing roles / applied_roles / roles_to_administrate into its instances.
	vm.cModule.define("resourcify", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		vm.defineRolifyResource(self.(*RClass), engine, roleCls)
		return object.NilV
	})
}

// installRolifyRoleClass creates Rolify::Role — the read-only Ruby view of a
// library role handed back by user.roles / resource.applied_roles.
func (vm *VM) installRolifyRoleClass(mod *RClass) *RClass {
	cls := newClass("Rolify::Role", vm.cObject)
	mod.consts["Role"] = cls
	vm.consts["Rolify::Role"] = cls

	self := func(v object.Value) *rolify.Role { return v.(*rolifyRole).r }

	cls.define("name", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).Name)
	})
	cls.define("resource_type", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		if t := self(v).ResourceType; t != "" {
			return object.NewString(t)
		}
		return object.NilV
	})
	cls.define("resource_id", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		if id := self(v).ResourceID; id != "" {
			return object.NewString(id)
		}
		return object.NilV
	})
	cls.define("id", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(self(v).ID)
	})
	toS := func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(v.(*rolifyRole).ToS())
	}
	cls.define("to_s", toS)
	cls.define("inspect", toS)
	return cls
}

// defineRolifyHolder mixes the role-holder predicates into a class that called
// rolify. self inside each method is the holder record; its identity (class name
// + id) keys the store.
func (vm *VM) defineRolifyHolder(cls *RClass, handle func(string) *rolify.User, roleCls *RClass) {
	cls.define("add_role", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		name := rolifyRoleName(args)
		role := handle(vm.rolifyUserKey(self)).AddRole(name, vm.rolifyScopeArgs(args[1:], false))
		return wrapRole(roleCls, role)
	})
	cls.define("has_role?", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		name := rolifyRoleName(args)
		return object.Bool(handle(vm.rolifyUserKey(self)).HasRole(name, vm.rolifyScopeArgs(args[1:], true)))
	})
	cls.define("has_cached_role?", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		name := rolifyRoleName(args)
		return object.Bool(handle(vm.rolifyUserKey(self)).HasCachedRole(name, vm.rolifyScopeArgs(args[1:], true)))
	})
	cls.define("remove_role", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		name := rolifyRoleName(args)
		return object.Bool(handle(vm.rolifyUserKey(self)).RemoveRole(name, vm.rolifyScopeArgs(args[1:], false)))
	})
	cls.define("roles", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return wrapRoles(roleCls, handle(vm.rolifyUserKey(self)).Roles())
	})

	allFn := func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(handle(vm.rolifyUserKey(self)).HasAllRoles(vm.rolifyQueries(args)...))
	}
	cls.define("has_all_roles?", allFn)
	cls.define("has_all_of_roles?", allFn)

	anyFn := func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(handle(vm.rolifyUserKey(self)).HasAnyRole(vm.rolifyQueries(args)...))
	}
	cls.define("has_any_role?", anyFn)
	cls.define("has_any_of_roles?", anyFn)
}

// defineRolifyResource mixes the resource-side helpers into a class that called
// resourcify. self inside each method is the resource record; its class name +
// id name the scope the helpers report on.
func (vm *VM) defineRolifyResource(cls *RClass, engine *rolify.Rolify, roleCls *RClass) {
	// roles — the roles applied directly to this exact resource instance (the
	// gem's polymorphic has_many :roles association): a subset of applied_roles
	// narrowed to this instance.
	cls.define("roles", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		rt, rid := vm.classOf(self).name, vm.rolifyResourceID(self)
		var own []*rolify.Role
		for _, r := range engine.AppliedRoles(rt, rid) {
			if r.ResourceType == rt && r.ResourceID == rid {
				own = append(own, r)
			}
		}
		return wrapRoles(roleCls, own)
	})
	cls.define("applied_roles", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return wrapRoles(roleCls, engine.AppliedRoles(vm.classOf(self).name, vm.rolifyResourceID(self)))
	})
	cls.define("roles_to_administrate", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return wrapRoles(roleCls, engine.RolesToAdministrate(vm.classOf(self).name, vm.rolifyResourceID(self)))
	})
}

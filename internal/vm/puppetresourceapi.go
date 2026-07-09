// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	resourceapi "github.com/go-ruby-puppet-resource-api/puppet-resource-api"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file installs the Puppet::ResourceApi module and its Ruby-facing API
// (require "puppet/resource_api"). The type/provider engine — Definition
// validation, the Pcore-typed per-attribute checks, title-pattern namevar
// derivation, and the get/set provider protocol with change computation — lives
// in github.com/go-ruby-puppet-resource-api/puppet-resource-api (a pure-Go port
// of the core of the puppet-resource_api gem). rbgo owns only the object-model
// bridge: the Ruby Puppet::ResourceApi.register_type surface, the
// TypeDefinition class (validate / attributes / namevars / title / apply) and
// the provider get/set seams, which run INLINE on the VM goroutine under the
// GVL. The type registry is per-VM (a captured *Registry), so types registered
// in one interpreter never leak into another.
//
// Supported: register_type (name, desc, attributes with type / desc / default /
// behaviour, and features), type lookup, instance validation, title derivation,
// and Apply driven by Ruby get / set provider blocks.
//
// Deferred: the per-attribute munge / validate / canonicalize block seams
// carried on a definition hash, title_patterns, the auto-relation maps, and the
// SimpleProvider CRUD shorthand. A provider is supplied to #apply as get / set
// blocks instead.

// PraType is a Ruby Puppet::ResourceApi::TypeDefinition instance: a handle over
// one compiled resourceapi.Type.
type PraType struct{ t *resourceapi.Type }

func (o *PraType) ToS() string     { return "#<Puppet::ResourceApi::TypeDefinition " + o.t.Name() + ">" }
func (o *PraType) Inspect() string { return o.ToS() }
func (o *PraType) Truthy() bool    { return true }

// registerPuppetResourceAPI installs the Puppet::ResourceApi module (require
// "puppet/resource_api") under the existing Puppet module.
func (vm *VM) registerPuppetResourceAPI() {
	pmod := vm.consts["Puppet"].(*RClass)

	ra := newClass("Puppet::ResourceApi", nil)
	ra.isModule = true
	pmod.consts["ResourceApi"] = ra
	vm.consts["Puppet::ResourceApi"] = ra

	// Puppet::DevError (schema problems) and Puppet::ResourceError (instance
	// validation) both descend from Puppet::Error (installed by registerPuppet).
	perr := vm.consts["Puppet::Error"].(*RClass)
	devErr := newClass("Puppet::DevError", perr)
	pmod.consts["DevError"] = devErr
	vm.consts["Puppet::DevError"] = devErr
	resErr := newClass("Puppet::ResourceError", perr)
	pmod.consts["ResourceError"] = resErr
	vm.consts["Puppet::ResourceError"] = resErr

	// The type registry is captured per-VM so registrations never leak.
	reg := resourceapi.NewRegistry()

	typeCls := newClass("Puppet::ResourceApi::TypeDefinition", vm.cObject)
	ra.consts["TypeDefinition"] = typeCls
	vm.consts["Puppet::ResourceApi::TypeDefinition"] = typeCls

	sm := func(name string, fn NativeFn) {
		ra.smethods[name] = &Method{name: name, owner: ra, native: fn}
	}

	// Puppet::ResourceApi.register_type(definition) — validate and register a
	// type; Puppet::DevError on a malformed schema. Returns the TypeDefinition.
	sm("register_type", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		def := praBuildDefinition(args)
		t, err := reg.Register(def)
		if err != nil {
			raise("Puppet::DevError", "%s", err.Error())
		}
		return &PraType{t: t}
	})

	// Puppet::ResourceApi.type(name) — the registered TypeDefinition, or nil.
	sm("type", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		t, ok := reg.Get(args[0].ToS())
		if !ok {
			return object.NilV
		}
		return &PraType{t: t}
	})

	// Puppet::ResourceApi.definitions — the names of every registered type.
	sm("definitions", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return strSliceToRuby(reg.Names())
	})

	vm.registerPraType(typeCls)
}

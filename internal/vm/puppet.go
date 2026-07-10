// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	puppet "github.com/go-ruby-puppet/puppet"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file installs the Puppet module and its Ruby-facing API (require
// "puppet"). The Puppet language engine — lexer, parser, Puppet::Pops AST,
// evaluator and catalog compiler, with the type system delegated to go-pcore,
// data binding to go-hiera and facts to a FactsProvider — lives in
// github.com/go-puppet/puppet, wrapped by the Ruby-API adapter
// github.com/go-ruby-puppet/puppet. rbgo owns only the object-model bridge: the
// Puppet.parse / Puppet.compile surface and the Ruby Catalog/Resource model.

// PuppetCatalog is a compiled Puppet catalog (Puppet::Resource::Catalog): the
// resources, their containment/ordering edges, the compile logs and a JSON view.
type PuppetCatalog struct {
	vm   *VM
	c    *puppet.Catalog
	logs []puppet.LogEntry
}

func (o *PuppetCatalog) ToS() string     { return "#<Puppet::Resource::Catalog>" }
func (o *PuppetCatalog) Inspect() string { return o.ToS() }
func (o *PuppetCatalog) Truthy() bool    { return true }

// PuppetResource is one compiled catalog resource (Puppet::Resource): its type,
// title, parameters and tags.
type PuppetResource struct{ r *puppet.Resource }

func (o *PuppetResource) ToS() string     { return o.r.Ref() }
func (o *PuppetResource) Inspect() string { return "#<Puppet::Resource " + o.r.Ref() + ">" }
func (o *PuppetResource) Truthy() bool    { return true }

// registerPuppet installs the Puppet module (require "puppet"): Puppet.parse
// syntax-checks a manifest and Puppet.compile compiles one into a catalog, with
// the Catalog/Resource classes the results dispatch on.
func (vm *VM) registerPuppet() {
	mod := newClass("Puppet", nil)
	mod.isModule = true
	vm.consts["Puppet"] = mod

	std := vm.consts["StandardError"].(*RClass)

	// Puppet::Error < StandardError; Puppet::ParseError < Puppet::Error — the
	// errors a syntax problem or a compile failure raises.
	perr := newClass("Puppet::Error", std)
	mod.consts["Error"] = perr
	vm.consts["Puppet::Error"] = perr
	pparse := newClass("Puppet::ParseError", perr)
	mod.consts["ParseError"] = pparse
	vm.consts["Puppet::ParseError"] = pparse

	// Puppet::Resource — one catalog resource.
	resCls := newClass("Puppet::Resource", vm.cObject)
	mod.consts["Resource"] = resCls
	vm.consts["Puppet::Resource"] = resCls

	// Puppet::Resource::Catalog — the compiled catalog.
	catCls := newClass("Puppet::Resource::Catalog", vm.cObject)
	resCls.consts["Catalog"] = catCls
	vm.consts["Puppet::Resource::Catalog"] = catCls

	sm := func(name string, fn NativeFn) {
		mod.smethods[name] = &Method{name: name, owner: mod, native: fn}
	}

	// Puppet.parse(manifest) — syntax-check; true on success, Puppet::ParseError
	// on a syntax problem.
	sm("parse", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		src := puppetStrArg(args)
		if err := puppet.Parse(src); err != nil {
			raise("Puppet::ParseError", "%s", err.Error())
		}
		return object.Bool(true)
	})

	// Puppet.compile(manifest[, facts:, node_name:, hiera_config:, format:]) —
	// compile a catalog; format: selects :puppet (default) or :hcl2 surface
	// syntax; Puppet::Error on an evaluation or hiera-load failure.
	sm("compile", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.puppetCompile(args)
	})

	vm.registerPuppetCatalog(catCls)
	vm.registerPuppetResource(resCls)
}

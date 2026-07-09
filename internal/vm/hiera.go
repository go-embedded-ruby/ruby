// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	hiera "github.com/go-ruby-hiera/hiera"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file installs the Hiera class and its Ruby-facing API (require "hiera").
// The Hiera 5 lookup engine — the hiera.yaml loader, hierarchy walk, merge
// behaviours, %{…} interpolation, lookup_options and dotted-key digging — lives
// in github.com/go-hiera/hiera, wrapped by the Ruby-API adapter
// github.com/go-ruby-hiera/hiera. rbgo owns only the object-model bridge: the
// Ruby Hiera class, its new/lookup surface and the Ruby⇄Go value conversion.
//
// Each Hiera.new builds one adapter bound to a config file and a scope (Hiera 5
// style: the node's variables/facts are fixed for the object's life), so a
// program that resolves data against different scopes constructs one Hiera each.

// HieraObj is a Ruby Hiera instance: one go-ruby-hiera adapter (config + scope)
// behind the #lookup surface.
type HieraObj struct {
	vm *VM
	h  *hiera.Hiera
}

func (o *HieraObj) ToS() string     { return "#<Hiera>" }
func (o *HieraObj) Inspect() string { return o.ToS() }
func (o *HieraObj) Truthy() bool    { return true }

// registerHiera installs the Hiera class (require "hiera"): Hiera.new(config:,
// scope:) builds an adapter over a hiera.yaml, and #lookup resolves a key with an
// optional default and resolution_type / merge behaviour.
func (vm *VM) registerHiera() {
	cls := newClass("Hiera", vm.cObject)
	vm.consts["Hiera"] = cls

	cls.smethods["new"] = &Method{name: "new", owner: cls, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.hieraNew(args)
	}}

	cls.define("lookup", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return self.(*HieraObj).lookup(args)
	})
}

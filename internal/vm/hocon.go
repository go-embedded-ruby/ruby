// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	hocon "github.com/go-ruby-hocon/hocon"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file installs the Hocon module and its Ruby-facing API (require
// "hocon"). The HOCON parser — the JSON-superset grammar, object merging, value
// concatenation, ${...} substitutions, include directives and duration / size
// unit suffixes — lives in github.com/go-hocon/hocon, wrapped by the Ruby-gem
// adapter github.com/go-ruby-hocon/hocon. rbgo owns only the object-model
// bridge: the Ruby Hocon / Hocon::ConfigFactory / Hocon::Config surface and the
// Go⇄Ruby value conversion. Config values are immutable, so the binding is
// stateless (no per-VM field).

// HoconConfig is a Ruby Hocon::Config instance: one resolved config tree behind
// the typed path accessors.
type HoconConfig struct{ c *hocon.Config }

func (o *HoconConfig) ToS() string     { return "#<Hocon::Config>" }
func (o *HoconConfig) Inspect() string { return o.ToS() }
func (o *HoconConfig) Truthy() bool    { return true }

// registerHocon installs the Hocon module (require "hocon"): Hocon.parse and
// Hocon::ConfigFactory.parse_string / parse_file build a Config, whose typed
// accessors read values by dotted path.
func (vm *VM) registerHocon() {
	mod := newClass("Hocon", nil)
	mod.isModule = true
	vm.consts["Hocon"] = mod

	std := vm.consts["StandardError"].(*RClass)
	cfgErr := newClass("Hocon::ConfigError", std)
	mod.consts["ConfigError"] = cfgErr
	vm.consts["Hocon::ConfigError"] = cfgErr

	// Hocon.parse(string) — parse a HOCON document into a Config.
	mod.smethods["parse"] = &Method{name: "parse", owner: mod, native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return hoconParse(hoconStrArg(args))
	}}

	// Hocon::ConfigFactory.parse_string(str) / .parse_file(path).
	factory := newClass("Hocon::ConfigFactory", nil)
	factory.isModule = true
	mod.consts["ConfigFactory"] = factory
	vm.consts["Hocon::ConfigFactory"] = factory
	factory.smethods["parse_string"] = &Method{name: "parse_string", owner: factory, native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return hoconParse(hoconStrArg(args))
	}}
	factory.smethods["parse_file"] = &Method{name: "parse_file", owner: factory, native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		c, err := hocon.ConfigFactory.ParseFile(hoconStrArg(args))
		if err != nil {
			raise("Hocon::ConfigError", "%s", err.Error())
		}
		return &HoconConfig{c: c}
	}}

	cls := newClass("Hocon::Config", vm.cObject)
	mod.consts["Config"] = cls
	vm.consts["Hocon::Config"] = cls
	vm.registerHoconConfig(cls)
}

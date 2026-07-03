// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	rubocop "github.com/go-ruby-rubocop/rubocop"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// RuboCopRunner is the Ruby wrapper around a *rubocop.Runner — the commissioner
// that runs a registry of cops over a source with a configuration
// (RuboCop::Runner). The offense/cop framework, the core cop set and the
// .rubocop.yml model live in the github.com/go-ruby-rubocop/rubocop library
// (built on the go-ruby-parser AST); this shell wires a Ruby String source to
// Runner.Inspect / Runner.Autocorrect and maps the returned Offenses back to
// Ruby value objects. The gem's file-walking is the host seam: this surface runs
// on source strings, exactly the shape the library exposes.
type RuboCopRunner struct {
	r *rubocop.Runner
}

func (r *RuboCopRunner) ToS() string     { return "#<RuboCop::Runner>" }
func (r *RuboCopRunner) Inspect() string { return "#<RuboCop::Runner>" }
func (r *RuboCopRunner) Truthy() bool    { return true }

// RuboCopConfig is the Ruby wrapper around a *rubocop.Config — a whole-run
// configuration parsed from a .rubocop.yml document (RuboCop::Config).
type RuboCopConfig struct {
	c *rubocop.Config
}

func (c *RuboCopConfig) ToS() string     { return "#<RuboCop::Config>" }
func (c *RuboCopConfig) Inspect() string { return "#<RuboCop::Config>" }
func (c *RuboCopConfig) Truthy() bool    { return true }

// RuboCopOffense is the Ruby wrapper around a rubocop.Offense — one reported
// violation (RuboCop::Cop::Offense): its department-qualified cop name, message,
// severity, source location and correctability.
type RuboCopOffense struct {
	o rubocop.Offense
}

func (o *RuboCopOffense) ToS() string     { return o.o.String() }
func (o *RuboCopOffense) Inspect() string { return "#<RuboCop::Cop::Offense " + o.o.CopName + ">" }
func (o *RuboCopOffense) Truthy() bool    { return true }

// RuboCopLocation is the Ruby wrapper around a rubocop.Location — a 1-based
// line/column span (RuboCop::Cop::Offense::Location); #line / #column / #length.
type RuboCopLocation struct {
	l rubocop.Location
}

func (l *RuboCopLocation) ToS() string {
	return "#<RuboCop::Cop::Offense::Location>"
}
func (l *RuboCopLocation) Inspect() string { return l.ToS() }
func (l *RuboCopLocation) Truthy() bool    { return true }

// registerRuboCop installs the RuboCop module and its Runner / Config surface and
// the Cop::Offense / Location value objects (require "rubocop"): RuboCop::Runner
// .new([config]) drives the default core cop set; #inspect(source[, path])
// returns the Offenses (each a RuboCop::Cop::Offense answering #cop_name /
// #message / #severity / #line / #column / #location / #correctable?), and
// #autocorrect(source[, path]) returns the corrected String. RuboCop::Config.new
// / .parse(yml) build a configuration. A malformed .rubocop.yml raises
// RuboCop::Error.
func (vm *VM) registerRuboCop() {
	mod := newClass("RuboCop", nil)
	mod.isModule = true
	vm.consts["RuboCop"] = mod

	// RuboCop::Error < StandardError for a config-parse failure, mirroring the gem.
	std := object.Kind[*RClass](vm.consts["StandardError"])
	rcErr := newClass("RuboCop::Error", std)
	mod.consts["Error"] = rcErr
	vm.consts["RuboCop::Error"] = rcErr

	// RuboCop::Cop and RuboCop::Cop::Offense[::Location] namespace.
	cop := newClass("RuboCop::Cop", nil)
	cop.isModule = true
	mod.consts["Cop"] = cop
	vm.consts["RuboCop::Cop"] = cop

	vm.registerRuboCopConfig(mod)
	vm.registerRuboCopOffense(cop)
	vm.registerRuboCopRunner(mod)
}

// registerRuboCopConfig installs RuboCop::Config.new / .parse(src) and its
// instance shell.
func (vm *VM) registerRuboCopConfig(mod *RClass) {
	cls := newClass("RuboCop::Config", vm.cObject)
	mod.consts["Config"] = cls
	vm.consts["RuboCop::Config"] = cls

	cls.smethods["new"] = &Method{name: "new", owner: cls, native: func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return &RuboCopConfig{c: rubocop.NewConfig()}
	}}
	// RuboCop::Config.parse(yml) parses a .rubocop.yml document.
	cls.smethods["parse"] = &Method{name: "parse", owner: cls, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		c, err := rubocop.ParseConfig(rubocopStr(args[0]))
		if err != nil {
			raise("RuboCop::Error", "%s", err.Error())
		}
		return &RuboCopConfig{c: c}
	}}
}

// registerRuboCopRunner installs RuboCop::Runner.new([config]) and its #inspect /
// #autocorrect instance methods.
func (vm *VM) registerRuboCopRunner(mod *RClass) {
	cls := newClass("RuboCop::Runner", vm.cObject)
	mod.consts["Runner"] = cls
	vm.consts["RuboCop::Runner"] = cls

	cls.smethods["new"] = &Method{name: "new", owner: cls, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		cfg := rubocop.NewConfig()
		if len(args) > 0 {
			if c, ok := object.KindOK[*RuboCopConfig](args[0]); ok {
				cfg = c.c
			}
		}
		return &RuboCopRunner{r: rubocop.NewRunner(rubocop.DefaultRegistry(), cfg)}
	}}

	self := func(v object.Value) *rubocop.Runner { return object.Kind[*RuboCopRunner](v).r }

	// #inspect(source[, path]) returns the Offenses as an Array of value objects.
	cls.define("inspect", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		src, path := rubocopSourceArgs(args)
		offs := self(v).Inspect(path, src)
		arr := object.NewArrayFromSlice(make([]object.Value, len(offs)))
		for i, o := range offs {
			arr.Elems[i] = &RuboCopOffense{o: o}
		}
		return arr
	})

	// #autocorrect(source[, path]) returns the corrected source String.
	cls.define("autocorrect", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		src, path := rubocopSourceArgs(args)
		return object.NewString(self(v).Autocorrect(path, src))
	})
}

// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	thor "github.com/go-ruby-thor/thor"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerThor installs the Thor CLI-framework core (require "thor"): the
// deterministic, interpreter-independent halves of Thor a host embeds — the
// declarative option model (Thor::Option), the option parser (Thor::Options#parse
// → the parsed-options Hash + #remaining), and the command registry with
// argv→command dispatch and byte-faithful help generation (Thor::Base /
// Thor::Command). Running a resolved command's body is the documented host seam
// (user Ruby); this binding resolves argv into (command, parsed-options,
// remaining-args) and renders help exactly as the gem does. The parser, dispatch
// and help/usage rendering live in the github.com/go-ruby-thor/thor library — the
// pure-Go (CGO=0) port of thor 1.5.0; this file is the class + method wiring (see
// thor_bind.go for the wrappers and value conversions). The error tree mirrors
// the gem: Thor::Error < StandardError, Thor::InvocationError beneath it, and the
// argument/command errors the parser raises (each keyed by the library's
// ErrorKind string so a raised *thor.Error maps to its Ruby class).
func (vm *VM) registerThor() {
	mod := newClass("Thor", vm.cObject)
	vm.consts["Thor"] = mod

	vm.registerThorErrors(mod)

	cOption := vm.thorClass(mod, "Option", "Thor::Option")
	cOptions := vm.thorClass(mod, "Options", "Thor::Options")
	cCommand := vm.thorClass(mod, "Command", "Thor::Command")
	cBase := vm.thorClass(mod, "Base", "Thor::Base")

	vm.registerThorOption(cOption)
	vm.registerThorOptions(cOptions)
	vm.registerThorCommand(cCommand)
	vm.registerThorBase(cBase)
}

// thorClass creates a Thor::* class under cObject, records it flat (for classOf)
// and nests it under the Thor namespace by its simple name.
func (vm *VM) thorClass(mod *RClass, simple, qualified string) *RClass {
	c := newClass(qualified, vm.cObject)
	vm.consts[qualified] = c
	mod.consts[simple] = c
	return c
}

// registerThorErrors installs the Thor exception tree mirroring the gem:
// Thor::Error < StandardError, Thor::InvocationError < Thor::Error, and the
// argument/command errors the parser and dispatcher raise. Every class name
// equals a library ErrorKind's String(), so a raised *thor.Error maps to its Ruby
// class by name (KindArgument maps to Ruby's own ArgumentError).
func (vm *VM) registerThorErrors(mod *RClass) {
	defs := []struct{ qualified, parent string }{
		{"Thor::Error", "StandardError"},
		{"Thor::InvocationError", "Thor::Error"},
		{"Thor::RequiredArgumentMissingError", "Thor::InvocationError"},
		{"Thor::MalformattedArgumentError", "Thor::InvocationError"},
		{"Thor::ExclusiveArgumentError", "Thor::InvocationError"},
		{"Thor::AtLeastOneRequiredArgumentError", "Thor::InvocationError"},
		{"Thor::UnknownArgumentError", "Thor::Error"},
		{"Thor::UndefinedCommandError", "Thor::Error"},
		{"Thor::AmbiguousCommandError", "Thor::Error"},
	}
	for _, d := range defs {
		parent := vm.consts[d.parent].(*RClass)
		cls := newClass(d.qualified, parent)
		vm.consts[d.qualified] = cls
		mod.consts[d.qualified[len("Thor::"):]] = cls
	}
}

// registerThorOption installs Thor::Option: the declarative option model. new
// builds a *thor.Option from a name and an options Hash (the gem's
// option/method_option keyword set), applying Thor's defaults and rejecting the
// invalid declarations Thor rejects (boolean + required) as ArgumentError. The
// readers mirror Thor::Option's public surface.
func (vm *VM) registerThorOption(c *RClass) {
	c.smethods["new"] = &Method{name: "new", owner: c,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
			}
			opt, err := vm.thorBuildOption(strArg(args[0]), thorOptHash(args, 1))
			if err != nil {
				raise("ArgumentError", "%s", err.Error())
			}
			return &ThorOption{opt}
		}}

	optOf := func(self object.Value) *thor.Option { return self.(*ThorOption).o }
	c.define("name", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(optOf(self).Name)
	})
	c.define("human_name", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(optOf(self).HumanName())
	})
	c.define("switch_name", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(optOf(self).SwitchName())
	})
	c.define("description", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(optOf(self).Desc)
	})
	c.define("boolean?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(optOf(self).Boolean())
	})
	c.define("string?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(optOf(self).StringType())
	})
	c.define("required?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(optOf(self).Required)
	})
	c.define("aliases", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return thorStrArray(optOf(self).Aliases)
	})
	c.define("default", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return thorValueToRuby(optOf(self).Default)
	})
	c.define("usage", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(optOf(self).Usage(thorIntArg(args, 0)))
	})
	c.define("print_default", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(optOf(self).PrintDefault())
	})
	c.define("enum_to_s", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(optOf(self).EnumToS())
	})
}

// registerThorOptions installs Thor::Options: the option parser. new takes an
// Array of Thor::Option; #parse(argv) parses the argv into the parsed-options
// Hash (option human name → value) and stashes the non-option remainder, which
// #remaining returns. A parse failure raises the matching Thor error.
func (vm *VM) registerThorOptions(c *RClass) {
	c.smethods["new"] = &Method{name: "new", owner: c,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			var opts []*thor.Option
			if len(args) > 0 {
				opts = vm.thorOptionList(args[0])
			}
			return &ThorOptions{parser: thor.NewOptions(opts, nil, false, false, thor.Relations{})}
		}}

	c.define("parse", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		to := self.(*ThorOptions)
		res, err := to.parser.Parse(thorArgv(args, 0))
		if err != nil {
			vm.raiseThorError(err)
		}
		to.remaining = res.Args
		return thorValueMapToHash(res.Options)
	})
	c.define("remaining", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return thorStrArray(self.(*ThorOptions).remaining)
	})
}

// registerThorCommand installs Thor::Command: a registered command's metadata
// (name, description, usage line, declared options). Invoking the command body is
// the host seam; this type carries only the dispatch/help metadata.
func (vm *VM) registerThorCommand(c *RClass) {
	c.smethods["new"] = &Method{name: "new", owner: c,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) < 3 {
				raise("ArgumentError", "wrong number of arguments (given %d, expected 3..4)", len(args))
			}
			var opts []*thor.Option
			if len(args) > 3 {
				opts = vm.thorOptionList(args[3])
			}
			return &ThorCommand{thor.NewCommand(strArg(args[0]), strArg(args[1]), strArg(args[2]), opts)}
		}}

	cmdOf := func(self object.Value) *thor.Command { return self.(*ThorCommand).c }
	c.define("name", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(cmdOf(self).Name)
	})
	c.define("description", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(cmdOf(self).Description)
	})
	c.define("usage", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(cmdOf(self).Usage)
	})
	c.define("options", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		cmd := cmdOf(self)
		elems := make([]object.Value, len(cmd.Options))
		for i, o := range cmd.Options {
			elems[i] = &ThorOption{o}
		}
		return object.NewArrayFromSlice(elems)
	})
}

// registerThorBase installs Thor::Base: the command registry (a Thor subclass's
// Go analogue). new(namespace, config) builds it; add_command registers a
// command; dispatch(argv) resolves argv into [command_name, options_hash,
// remaining_args]; help / command_help render byte-faithful help; commands and
// normalize_command_name expose the registry.
func (vm *VM) registerThorBase(c *RClass) {
	c.smethods["new"] = &Method{name: "new", owner: c,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			var ns string
			if len(args) > 0 {
				ns = strArg(args[0])
			}
			return &ThorBase{thor.NewBase(ns, thorConfig(thorOptHash(args, 1)))}
		}}

	baseOf := func(self object.Value) *thor.Base { return self.(*ThorBase).b }
	c.define("add_command", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		cmd, ok := args[0].(*ThorCommand)
		if !ok {
			raise("TypeError", "wrong argument type %s (expected Thor::Command)", vm.classOf(args[0]).name)
		}
		baseOf(self).AddCommand(cmd.c)
		return self
	})
	c.define("commands", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		cmds := baseOf(self).Commands()
		elems := make([]object.Value, len(cmds))
		for i, cm := range cmds {
			elems[i] = &ThorCommand{cm}
		}
		return object.NewArrayFromSlice(elems)
	})
	c.define("help", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(baseOf(self).Help())
	})
	c.define("command_help", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s, err := baseOf(self).CommandHelp(strArg(args[0]))
		if err != nil {
			vm.raiseThorError(err)
		}
		return object.NewString(s)
	})
	c.define("normalize_command_name", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		var tok string
		if len(args) > 0 && !object.IsNil(args[0]) {
			tok = strArg(args[0])
		}
		name, err := baseOf(self).NormalizeCommandName(tok)
		if err != nil {
			vm.raiseThorError(err)
		}
		return object.NewString(name)
	})
	c.define("dispatch", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		cmd, res, err := baseOf(self).Dispatch(thorArgv(args, 0))
		if err != nil {
			vm.raiseThorError(err)
		}
		return object.NewArrayFromSlice([]object.Value{
			object.NewString(cmd.Name),
			thorValueMapToHash(res.Options),
			thorStrArray(res.Args),
		})
	})
}

// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerYAML installs a loadable shell for the YAML / Psych standard library
// (require "yaml"). A real YAML parser/emitter is a separate large subsystem; for
// now the module and its constant/error tree exist so load-time references
// resolve (Puppet requires yaml at load but only calls it from method bodies),
// while dump / load / safe_load / parse raise NotImplementedError if invoked.
func (vm *VM) registerYAML() {
	psych := newClass("Psych", nil)
	psych.isModule = true
	vm.consts["Psych"] = psych
	// In MRI, YAML is an alias of Psych.
	vm.consts["YAML"] = psych

	std := vm.consts["StandardError"].(*RClass)
	psych.consts["Exception"] = newClass("Psych::Exception", std)
	psych.consts["SyntaxError"] = newClass("Psych::SyntaxError", psych.consts["Exception"].(*RClass))
	psych.consts["DisallowedClass"] = newClass("Psych::DisallowedClass", psych.consts["Exception"].(*RClass))
	psych.consts["VERSION"] = object.NewString("5.0.0")

	nodes := newClass("Psych::Nodes", nil)
	nodes.isModule = true
	psych.consts["Nodes"] = nodes

	notImpl := func(what string) NativeFn {
		return func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return raise("NotImplementedError", "YAML %s not yet supported (pure-Go YAML pending)", what)
		}
	}
	for _, m := range []string{"load", "safe_load", "load_file", "parse", "parse_stream", "dump_tags"} {
		psych.smethods[m] = &Method{name: m, owner: psych, native: notImpl(m)}
	}

	// YAML.dump(obj[, io]) serialises a tree of plain values (Hash/Array/String/
	// Symbol/Integer/Float/true/false/nil/Time) to a Psych-compatible document.
	// With a second IO argument it writes the document there and returns the IO
	// (as Psych does), so Puppet::Util::Yaml.dump(structure, fh) persists state
	// and run-summary files; with one argument it returns the String. A value
	// outside the supported shapes raises TypeError, which the report YAML
	// indirector rescues and logs, leaving puppet apply otherwise clean.
	psych.smethods["dump"] = &Method{name: "dump", owner: psych,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1..)")
			}
			doc := object.NewString(yamlDump(args[0]))
			if len(args) > 1 && args[1] != object.NilV {
				vm.send(args[1], "write", []object.Value{doc}, nil)
				return args[1]
			}
			return doc
		}}
}

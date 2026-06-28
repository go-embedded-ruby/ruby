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
	for _, m := range []string{"dump", "load", "safe_load", "load_file", "parse", "parse_stream", "dump_tags"} {
		psych.smethods[m] = &Method{name: m, owner: psych, native: notImpl(m)}
	}
}

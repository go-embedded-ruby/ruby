// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerYAML installs the YAML / Psych standard library (require "yaml"). The
// module, its constant / error tree, the dump emitter and the load / safe_load /
// load_file parser are pure-Go; parse / parse_stream / dump_tags (the low-level
// node API) still raise NotImplementedError as Puppet does not call them.
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
	// The low-level node API (parse / parse_stream) and dump_tags are not used by
	// Puppet's local persistence and remain unimplemented.
	for _, m := range []string{"parse", "parse_stream", "dump_tags"} {
		psych.smethods[m] = &Method{name: m, owner: psych, native: notImpl(m)}
	}

	// YAML.load(source[, ...]) / Psych.load parse a YAML document string to a tree
	// of Ruby values (Hash / Array / String / Symbol / Integer / Float / true /
	// false / nil / Time, and !ruby/object: instances). Leading keyword/positional
	// options Psych accepts (permitted_classes, aliases, filename, …) are tolerated
	// and ignored: this loader is already safe by construction (it instantiates
	// only the classes named by !ruby/object: tags actually present), so safe_load
	// shares the same implementation.
	loadFn := func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..)")
		}
		return yamlLoad(vm, yamlSourceArg(args[0]))
	}
	psych.smethods["load"] = &Method{name: "load", owner: psych, native: loadFn}
	psych.smethods["safe_load"] = &Method{name: "safe_load", owner: psych, native: loadFn}
	psych.smethods["unsafe_load"] = &Method{name: "unsafe_load", owner: psych, native: loadFn}

	// YAML.load_file(path[, ...]) reads the file and parses its contents.
	loadFileFn := func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..)")
		}
		data, err := fuReadFile(strArg(args[0]))
		if err != nil {
			raise("Errno::ENOENT", "No such file or directory @ rb_sysopen - %s", strArg(args[0]))
		}
		return yamlLoad(vm, string(data))
	}
	psych.smethods["load_file"] = &Method{name: "load_file", owner: psych, native: loadFileFn}
	psych.smethods["safe_load_file"] = &Method{name: "safe_load_file", owner: psych, native: loadFileFn}

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
			doc := object.NewString(yamlDump(vm, args[0]))
			if len(args) > 1 && args[1] != object.NilV {
				vm.send(args[1], "write", []object.Value{doc}, nil)
				return args[1]
			}
			return doc
		}}

	// Object#to_yaml (Psych installs this on Object) returns YAML.dump(self). It is
	// what Puppet's report store terminus calls (`fh.print to_yaml`), so every
	// object — including the full Puppet::Transaction::Report graph — serialises.
	vm.cObject.methods["to_yaml"] = &Method{name: "to_yaml", owner: vm.cObject,
		native: func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
			return object.NewString(yamlDump(vm, self))
		}}
}

// yamlSourceArg coerces YAML.load's first argument to a string: a String yields
// its contents, and any other value its to_s, so an IO-ish or symbol argument
// does not crash the loader (Puppet always passes a String).
func yamlSourceArg(v object.Value) string {
	if s, ok := v.(*object.String); ok {
		return s.Str()
	}
	return v.ToS()
}

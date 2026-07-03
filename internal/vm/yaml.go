// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerYAML installs the YAML / Psych standard library (require "yaml"). The
// module, its constant / error tree, and the dump / load / safe_load / load_file
// methods are pure-Go: the Psych-compatible emitter and loader live in the
// github.com/go-ruby-yaml/yaml library and rbgo binds them here, mapping its own
// object graph to and from that library's value model (see yaml_bind.go). The
// low-level node API (parse / parse_stream / dump_tags) still raises
// NotImplementedError as Puppet does not call it.
func (vm *VM) registerYAML() {
	psych := newClass("Psych", nil)
	psych.isModule = true
	vm.consts["Psych"] = object.Wrap(psych)
	// In MRI, YAML is an alias of Psych.
	vm.consts["YAML"] = object.Wrap(psych)

	std := object.Kind[*RClass](vm.consts["StandardError"])
	psych.consts["Exception"] = object.Wrap(newClass("Psych::Exception", std))
	psych.consts["SyntaxError"] = object.Wrap(newClass("Psych::SyntaxError", object.Kind[*RClass](psych.consts["Exception"])))
	psych.consts["DisallowedClass"] = object.Wrap(newClass("Psych::DisallowedClass", object.Kind[*RClass](psych.consts["Exception"])))
	psych.consts["VERSION"] = object.Wrap(object.NewString("5.0.0"))

	nodes := newClass("Psych::Nodes", nil)
	nodes.isModule = true
	psych.consts["Nodes"] = object.Wrap(nodes)

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
	// of Ruby values. unsafe_load shares the same implementation. Leading
	// keyword/positional options Psych accepts (filename, symbolize_names, …) are
	// tolerated and ignored.
	loadFn := func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..)")
		}
		return yamlLoad(vm, yamlSourceArg(args[0]))
	}
	psych.smethods["load"] = &Method{name: "load", owner: psych, native: loadFn}
	psych.smethods["unsafe_load"] = &Method{name: "unsafe_load", owner: psych, native: loadFn}

	// YAML.safe_load(source[, permitted_classes: [...]]) restricts which
	// !ruby/object: classes materialise; the loader is already safe by construction,
	// so the allow-list is the only observable difference from load.
	safeLoadFn := func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..)")
		}
		return yamlSafeLoad(vm, yamlSourceArg(args[0]), permittedClassesArg(args[1:]))
	}
	psych.smethods["safe_load"] = &Method{name: "safe_load", owner: psych, native: safeLoadFn}

	// YAML.load_file(path[, ...]) reads the file and parses its contents.
	loadFileFn := func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..)")
		}
		return yamlLoad(vm, yamlReadFile(args[0]))
	}
	psych.smethods["load_file"] = &Method{name: "load_file", owner: psych, native: loadFileFn}

	// YAML.safe_load_file(path[, permitted_classes: [...]]) is safe_load over a file.
	safeLoadFileFn := func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..)")
		}
		return yamlSafeLoad(vm, yamlReadFile(args[0]), permittedClassesArg(args[1:]))
	}
	psych.smethods["safe_load_file"] = &Method{name: "safe_load_file", owner: psych, native: safeLoadFileFn}

	// YAML.dump(obj[, io]) serialises a tree of Ruby values to a Psych-compatible
	// document. With a second IO argument it writes the document there and returns
	// the IO (as Psych does), so Puppet::Util::Yaml.dump(structure, fh) persists
	// state and run-summary files; with one argument it returns the String. A value
	// outside the supported shapes raises TypeError, which the report YAML
	// indirector rescues and logs, leaving puppet apply otherwise clean.
	psych.smethods["dump"] = &Method{name: "dump", owner: psych,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1..)")
			}
			doc := object.NewString(yamlDump(vm, args[0]))
			if len(args) > 1 && !object.IsNil(args[1]) {
				vm.send(args[1], "write", []object.Value{object.Wrap(doc)}, nil)
				return args[1]
			}
			return object.Wrap(doc)
		}}

	// Object#to_yaml (Psych installs this on Object) returns YAML.dump(self). It is
	// what Puppet's report store terminus calls (`fh.print to_yaml`), so every
	// object — including the full Puppet::Transaction::Report graph — serialises.
	vm.cObject.methods["to_yaml"] = &Method{name: "to_yaml", owner: vm.cObject,
		native: func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
			return object.Wrap(object.NewString(yamlDump(vm, self)))
		}}
}

// yamlSourceArg coerces YAML.load's first argument to a string: a String yields
// its contents, and any other value its to_s, so an IO-ish or symbol argument
// does not crash the loader (Puppet always passes a String).
func yamlSourceArg(v object.Value) string {
	if s, ok := object.KindOK[*object.String](v); ok {
		return s.Str()
	}
	return v.ToS()
}

// yamlReadFile reads the file named by v (coerced to a path string), raising
// Errno::ENOENT when it cannot be opened (matching Psych.load_file).
func yamlReadFile(v object.Value) string {
	path := strArg(v)
	data, err := fuReadFile(path)
	if err != nil {
		raise("Errno::ENOENT", "No such file or directory @ rb_sysopen - %s", path)
	}
	return string(data)
}

// permittedClassesArg extracts the permitted_classes: keyword from safe_load's
// trailing arguments, returning the class names (so SafeLoad restricts to them),
// or nil when the keyword is absent (permitting all classes). Class / Module
// values list by name; any other element is rendered via to_s, matching how
// Psych accepts a list of class objects or names.
func permittedClassesArg(rest []object.Value) []string {
	if len(rest) == 0 {
		return nil
	}
	h, ok := object.KindOK[*object.Hash](rest[len(rest)-1])
	if !ok {
		return nil
	}
	val, ok := h.Get(object.SymVal(string(object.Symbol("permitted_classes"))))
	if !ok {
		return nil
	}
	arr, ok := object.KindOK[*object.Array](val)
	if !ok {
		return nil
	}
	names := make([]string, 0, len(arr.Elems))
	for _, el := range arr.Elems {
		if c, ok := object.KindOK[*RClass](el); ok {
			names = append(names, c.ToS())
			continue
		}
		names = append(names, el.ToS())
	}
	return names
}

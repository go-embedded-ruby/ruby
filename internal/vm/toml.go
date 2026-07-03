// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerTOML installs the TOML module (require "toml"): TOML.parse /
// TomlRB.parse, TOML.load_file / TomlRB.load_file and TOML.dump / TomlRB.dump.
// The parser and generator live in the github.com/go-ruby-toml/toml library;
// this module is the thin wiring that maps rbgo's object graph to and from the
// library's value model (see toml_bind.go). TOML and TomlRB are the same module
// object, matching the toml-rb gem which exposes both names. The error tree
// (TomlRB::ParseError) is registered so a re-raised library error rescues as the
// right Ruby class.
func (vm *VM) registerTOML() {
	mod := newClass("TomlRB", nil)
	mod.isModule = true
	vm.consts["TomlRB"] = object.Wrap(mod)
	// The toml-rb gem exposes the module under both TomlRB and TOML.
	vm.consts["TOML"] = object.Wrap(mod)
	vm.registerTOMLErrors(mod)

	def := func(name string, fn NativeFn) { mod.smethods[name] = &Method{name: name, owner: mod, native: fn} }

	// TOML.parse(str) / TomlRB.parse(str) parse a document string to a Ruby Hash.
	def("parse", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..)")
		}
		return tomlParse(vm, tomlSourceArg(args[0]))
	})

	// TOML.load_file(path) / TomlRB.load_file(path) read the file and parse it.
	def("load_file", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..)")
		}
		return tomlParse(vm, tomlReadFile(args[0]))
	})

	// TOML.dump(obj) / TomlRB.dump(obj) serialise a Ruby Hash to a TOML document
	// string.
	def("dump", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..)")
		}
		return object.Wrap(object.NewString(tomlDump(args[0])))
	})
}

// registerTOMLErrors installs the TomlRB::ParseError exception tree mirroring the
// gem (ParseError / ValueOverwriteError < StandardError). Each class is
// registered both as a nested constant of TomlRB (so Ruby `TomlRB::ParseError`
// resolves it) and under its qualified name in the top-level table (so a
// re-raised library error's exceptionObject lookup finds the very same class),
// exactly as JSON:: classes are.
func (vm *VM) registerTOMLErrors(mod *RClass) {
	std := object.Kind[*RClass](vm.consts["StandardError"])
	reg := func(simple, qualified string, super *RClass) *RClass {
		c := newClass(qualified, super)
		mod.consts[simple] = object.Wrap(c)
		vm.consts[qualified] = object.Wrap(c)
		return c
	}
	reg("ParseError", "TomlRB::ParseError", std)
	reg("ValueOverwriteError", "TomlRB::ValueOverwriteError", std)
}

// tomlSourceArg coerces TOML.parse's argument to a string: a String yields its
// contents, and any other value its to_s, so a non-String argument does not crash
// the parser.
func tomlSourceArg(v object.Value) string {
	if s, ok := object.KindOK[*object.String](v); ok {
		return s.Str()
	}
	return v.ToS()
}

// tomlReadFile reads the file named by v (coerced to a path string), raising
// Errno::ENOENT when it cannot be opened (matching TomlRB.load_file).
func tomlReadFile(v object.Value) string {
	path := strArg(v)
	data, err := fuReadFile(path)
	if err != nil {
		raise("Errno::ENOENT", "No such file or directory @ rb_sysopen - %s", path)
	}
	return string(data)
}

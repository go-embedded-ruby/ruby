// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerDotenv installs the Dotenv module (require "dotenv"): Dotenv.parse(src)
// and Dotenv::Parser.call(src) return a Hash of the parsed pairs without touching
// the environment, while Dotenv.load(src) / Dotenv.overload(src) additionally set
// each pair into rbgo's ENV. The parser lives in the
// github.com/go-ruby-dotenv/dotenv library — the pure-Go core backing the
// `dotenv` gem — and this module is the thin wiring that binds the library's host
// seams to rbgo: ENV read (envLookup) as the parse Env.Lookup, ENV write
// (envSetenv) as Env.Set, and the shell backtick (runShellCommand) as
// Env.RunCommand so `$(cmd)` command substitutions resolve as in MRI.
func (vm *VM) registerDotenv() {
	mod := newClass("Dotenv", nil)
	mod.isModule = true
	vm.consts["Dotenv"] = mod

	def := func(name string, fn NativeFn) { mod.smethods[name] = &Method{name: name, owner: mod, native: fn} }

	// Dotenv.parse(src) parses one source String to a Hash of String→String,
	// resolving `$VAR` against ENV and `$(cmd)` via the shell, without mutating the
	// environment (the gem's Dotenv.parse / Dotenv::Parser.call).
	parse := func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return dotenvParse(vm, dotenvSourceArg(args[0]), false)
	}
	def("parse", parse)

	// Dotenv.load(src) parses and then sets each pair into ENV, but (like the gem)
	// does not overwrite a key already present in the environment.
	def("load", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return dotenvLoad(vm, dotenvSourceArg(args[0]), false)
	})

	// Dotenv.overload(src) / Dotenv.load(overwrite: true): parse and set every pair
	// into ENV, overwriting existing keys.
	def("overload", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return dotenvLoad(vm, dotenvSourceArg(args[0]), true)
	})

	// Dotenv::Parser is the gem's parser object; Dotenv::Parser.call(src) is the
	// low-level parse entry the module methods build on.
	parser := newClass("Dotenv::Parser", vm.cObject)
	parser.isModule = true
	mod.consts["Parser"] = parser
	vm.consts["Dotenv::Parser"] = parser
	parser.smethods["call"] = &Method{name: "call", owner: parser, native: parse}
}

// dotenvSourceArg coerces a source argument to its string: a String yields its
// contents, and any other value its to_s.
func dotenvSourceArg(v object.Value) string {
	if s, ok := v.(*object.String); ok {
		return s.Str()
	}
	return v.ToS()
}

// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerHCL2 installs the HCL2 module (require "hcl2"): HCL2.parse(src) and
// HCL2.eval(src, ctx). The lexer, parser and evaluator live in the
// github.com/go-ruby-hcl2/hcl2 library — a pure-Go from-scratch HCL2
// native-syntax implementation — and this module is the thin wiring that maps a
// Ruby source String (and, for eval, a variables/functions context Hash) to a
// single hcl2.Parse / hcl2.Eval call, returning a Ruby Hash (see hcl2_bind.go).
// A syntax or evaluation Diagnostics error is registered as HCL2::Error so a
// malformed document rescues as the right Ruby class.
func (vm *VM) registerHCL2() {
	mod := newClass("HCL2", nil)
	mod.isModule = true
	vm.consts["HCL2"] = object.Wrap(mod)
	vm.registerHCL2Errors(mod)

	def := func(name string, fn NativeFn) { mod.smethods[name] = &Method{name: name, owner: mod, native: fn} }

	// HCL2.parse(src) parses a document to a Ruby Hash with every expression
	// evaluated against an empty context (the common "read this config" call). A
	// document that references variables/functions raises HCL2::Error; use eval
	// with a context Hash to supply them.
	def("parse", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return hcl2Eval(vm, hcl2SourceArg(args[0]), nil)
	})

	// HCL2.eval(src, ctx = nil) evaluates a document against a context Hash
	// ({ variables: {..}, functions: {..} }, or a bare Hash read as variables) and
	// returns a Ruby Hash.
	def("eval", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		var ctx object.Value
		if len(args) > 1 {
			ctx = args[1]
		}
		return hcl2Eval(vm, hcl2SourceArg(args[0]), ctx)
	})

	// HCL2.eval_expr(src, ctx = nil) evaluates a single expression string against
	// the context and returns its Ruby value (HCL2's standalone-expression entry).
	def("eval_expr", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		var ctx object.Value
		if len(args) > 1 {
			ctx = args[1]
		}
		return hcl2EvalExpr(vm, hcl2SourceArg(args[0]), ctx)
	})
}

// registerHCL2Errors installs the HCL2::Error exception class (< StandardError),
// registered both as a nested constant of HCL2 (so Ruby `HCL2::Error` resolves
// it) and under its qualified name in the top-level table (so a re-raised library
// Diagnostics error's exceptionObject lookup finds the very same class), exactly
// as the JSON:: and TomlRB:: classes are.
func (vm *VM) registerHCL2Errors(mod *RClass) {
	std := object.Kind[*RClass](vm.consts["StandardError"])
	c := newClass("HCL2::Error", std)
	mod.consts["Error"] = object.Wrap(c)
	vm.consts["HCL2::Error"] = object.Wrap(c)
}

// hcl2SourceArg coerces a source argument to its string: a String yields its
// contents, and any other value its to_s, so a non-String argument does not crash
// the parser.
func hcl2SourceArg(v object.Value) string {
	if s, ok := object.KindOK[*object.String](v); ok {
		return s.Str()
	}
	return v.ToS()
}

// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerRouge installs the Rouge module (require "rouge"): the one-shot
// Rouge.highlight(text, lexer, formatter) convenience, Rouge::Lexer.find(name)
// and Rouge::Formatter.find(tag) for the class-based lookups, and the token
// hierarchy accessor Rouge::Token[qualname]. The lexers, the regex-lexer engine
// and the HTML formatters live in the github.com/go-ruby-rouge/rouge library — the
// pure-Go port of the `rouge` gem — and this module is the thin wiring that maps a
// Ruby source String and lexer/formatter names to a single rouge.Highlight call
// (see rouge_bind.go). The Rouge::Error tree is registered so an unknown
// lexer/formatter rescues as the right Ruby class.
func (vm *VM) registerRouge() {
	mod := newClass("Rouge", nil)
	mod.isModule = true
	vm.consts["Rouge"] = mod
	vm.registerRougeErrors(mod)

	// Rouge.highlight(text, lexer = "text", formatter = "html") tokenizes text with
	// the named lexer and renders it with the named formatter (the gem's
	// Rouge.highlight). An unknown lexer or formatter raises Rouge::Error.
	mod.smethods["highlight"] = &Method{name: "highlight", owner: mod,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1..3)")
			}
			lexer := "text"
			if len(args) > 1 {
				lexer = rougeNameArg(args[1])
			}
			formatter := "html"
			if len(args) > 2 {
				formatter = rougeNameArg(args[2])
			}
			return object.NewString(rougeHighlight(rougeStringArg(args[0]), lexer, formatter))
		}}

	// Rouge::Lexer is the lexer-lookup class: Lexer.find(name) returns a lexer
	// instance (a thin wrapper exposing #tag / #title / #aliases) or nil, mirroring
	// Rouge::Lexer.find.
	lexerCls := newClass("Lexer", vm.cObject)
	mod.consts["Lexer"] = lexerCls
	vm.consts["Rouge::Lexer"] = lexerCls
	lexerCls.smethods["find"] = &Method{name: "find", owner: lexerCls,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
			}
			return rougeFindLexer(lexerCls, rougeNameArg(args[0]))
		}}

	// Rouge::Lexer#tag / #title / #aliases expose the found lexer's metadata,
	// reading the native lexer handle stored on the instance.
	lexerCls.define("tag", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(rougeLexerHandle(self).lx.Tag())
	})
	lexerCls.define("title", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(rougeLexerHandle(self).lx.Title())
	})
	lexerCls.define("aliases", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		al := rougeLexerHandle(self).lx.Aliases()
		out := &object.Array{Elems: make([]object.Value, len(al))}
		for i, a := range al {
			out.Elems[i] = object.NewString(a)
		}
		return out
	})

	// Rouge::Formatter.find(tag) returns whether a formatter is registered under
	// tag: the found formatter's tag String, or nil (mirroring Rouge::Formatter.find
	// resolving a formatter class). rbgo models a formatter by its tag name, which
	// is all Rouge.highlight needs.
	fmtCls := newClass("Formatter", vm.cObject)
	mod.consts["Formatter"] = fmtCls
	vm.consts["Rouge::Formatter"] = fmtCls
	fmtCls.smethods["find"] = &Method{name: "find", owner: fmtCls,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
			}
			return rougeFindFormatter(rougeNameArg(args[0]))
		}}
}

// registerRougeErrors installs the Rouge::Error exception tree
// (Error < StandardError). It is registered both as a nested constant of Rouge (so
// Ruby `Rouge::Error` resolves it) and under its qualified name in the top-level
// table (so a re-raised library error's exceptionObject lookup finds the very same
// class), exactly as the Mustache:: classes are.
func (vm *VM) registerRougeErrors(mod *RClass) {
	std := vm.consts["StandardError"].(*RClass)
	c := newClass("Rouge::Error", std)
	mod.consts["Error"] = c
	vm.consts["Rouge::Error"] = c
}

// rougeStringArg coerces the highlight source argument to its text string: a String
// yields its contents, and any other value its to_s.
func rougeStringArg(v object.Value) string {
	if s, ok := v.(*object.String); ok {
		return s.Str()
	}
	return v.ToS()
}

// rougeNameArg renders a lexer / formatter name argument as its bare name: a
// String verbatim, a Symbol as its text, and any other value its to_s.
func rougeNameArg(v object.Value) string {
	switch n := v.(type) {
	case *object.String:
		return n.Str()
	case object.Symbol:
		return string(n)
	}
	return v.ToS()
}

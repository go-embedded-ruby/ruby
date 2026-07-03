// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerLiquid installs the Liquid module and its Liquid::Template class
// (require "liquid"): Liquid::Template.parse(src, error_mode: …) followed by
// tmpl.render(assigns) / tmpl.render!(assigns), the Shopify `liquid` gem's core
// API. The parser and renderer live in the github.com/go-ruby-liquid/liquid
// library — the pure-Go Liquid engine that backs the gem — and this module is the
// thin wiring that maps a Ruby template String (and an assigns Hash) to a single
// liquid.Parse / Template.Render call (see liquid_bind.go). The Liquid::Error
// tree is registered so a re-raised parse/render error rescues as the right Ruby
// class.
func (vm *VM) registerLiquid() {
	mod := newClass("Liquid", nil)
	mod.isModule = true
	vm.consts["Liquid"] = mod
	vm.registerLiquidErrors(mod)

	// Liquid::Template is the parsed-template class: Template.parse compiles a
	// source String, and #render / #render! evaluate it against an assigns Hash.
	tmplClass := newClass("Template", vm.cObject)
	mod.consts["Template"] = tmplClass
	vm.consts["Liquid::Template"] = tmplClass

	// Liquid::Template.parse(source, error_mode: :lax) parses source into a
	// template instance. The compiled template is stored on the instance as an
	// opaque native handle (@__tmpl); render walks it. A malformed template in
	// :strict mode raises Liquid::SyntaxError at parse time.
	tmplClass.smethods["parse"] = &Method{name: "parse", owner: tmplClass,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
			}
			mode := liquidErrorMode(args)
			t := liquidParse(liquidSourceArg(args[0]), mode)
			inst := &RObject{class: tmplClass, ivars: map[string]object.Value{}}
			inst.ivars["@__tmpl"] = &liquidTemplate{t: t}
			return inst
		}}

	// Liquid::Template#render(assigns = {}) renders the template against the
	// assigns Hash in lax mode: a runtime error renders inline as
	// "Liquid error: …" rather than raising, matching the gem's #render.
	tmplClass.define("render", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(liquidRender(vm, liquidHandle(self), liquidAssignsArg(vm, args), false))
	})

	// Liquid::Template#render!(assigns = {}) renders the template in strict mode:
	// a runtime error raises the corresponding Liquid error, matching #render!.
	tmplClass.define("render!", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(liquidRender(vm, liquidHandle(self), liquidAssignsArg(vm, args), true))
	})
}

// registerLiquidErrors installs the Liquid::Error exception tree
// (Error / SyntaxError / ArgumentError / ZeroDivisionError < StandardError). Each
// class is registered both as a nested constant of Liquid (so Ruby
// `Liquid::SyntaxError` resolves it) and under its qualified name in the top-level
// table (so a re-raised library error's exceptionObject lookup finds the very same
// class), exactly as the Mustache:: classes are.
func (vm *VM) registerLiquidErrors(mod *RClass) {
	std := object.Kind[*RClass](vm.consts["StandardError"])
	reg := func(simple, qualified string, super *RClass) *RClass {
		c := newClass(qualified, super)
		mod.consts[simple] = c
		vm.consts[qualified] = c
		return c
	}
	e := reg("Error", "Liquid::Error", std)
	reg("SyntaxError", "Liquid::SyntaxError", e)
	reg("ArgumentError", "Liquid::ArgumentError", e)
	reg("ZeroDivisionError", "Liquid::ZeroDivisionError", e)
	reg("StackLevelError", "Liquid::StackLevelError", e)
}

// liquidSourceArg coerces the parse argument to its template source string: a
// String yields its contents, and any other value its to_s, so a non-String
// argument does not crash the parser.
func liquidSourceArg(v object.Value) string {
	if s, ok := object.KindOK[*object.String](v); ok {
		return s.Str()
	}
	return v.ToS()
}

// liquidHandle returns the parsed-template handle stored on a Liquid::Template
// instance by parse. A receiver without one (never parsed) raises, matching a
// nil template.
func liquidHandle(self object.Value) *liquidTemplate {
	if h, ok := object.KindOK[*liquidTemplate](getIvar(self, "@__tmpl")); ok {
		return h
	}
	raise("Liquid::Error", "template was not parsed")
	return nil
}

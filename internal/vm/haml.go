// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	haml "github.com/go-ruby-haml/haml"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerHaml installs the native backbone of the Haml template engine
// (require "haml"): the Haml::Template class with a private native compiler hook,
// and the Haml::Engine alias. The template scanner/compiler lives entirely in the
// pure-Go github.com/go-ruby-haml/haml library (which compiles a Haml template to
// the Ruby source that renders it); the interpreter-bound pieces — new / render —
// stay in Ruby (prelude.rb) because they need a Binding and eval, exactly as ERB
// and Slim do. The runtime helpers the compiled source calls (Haml::Util.escape_html
// and Haml::HamlAttributes.render) are also supplied in the prelude.
//
// The class is created here so the native hook __compile can be a private instance
// method; the prelude reopens Haml::Template to add the public compile-to-eval API.
func (vm *VM) registerHaml() {
	mod := newClass("Haml", nil)
	mod.isModule = true
	vm.consts["Haml"] = object.Wrap(mod)
	vm.registerHamlErrors(mod)

	cTmpl := newClass("Template", vm.cObject)
	mod.consts["Template"] = object.Wrap(cTmpl)
	vm.consts["Haml::Template"] = object.Wrap(cTmpl)
	// Haml::Engine names the same class (the gem's Haml::Engine.new(src).render;
	// here the one Template class carries both roles).
	mod.consts["Engine"] = object.Wrap(cTmpl)
	vm.consts["Haml::Engine"] = object.Wrap(cTmpl)

	// __compile(template) -> src. The single bridge into the library: it returns
	// the Ruby source the compiled template evaluates to (assigning the buffer to
	// BufVar, appending fragments and evaluating to the buffer). A malformed
	// template surfaces as a Haml::SyntaxError so the contract stays total on the
	// Ruby side; well-formed templates always compile.
	cTmpl.methods["__compile"] = &Method{name: "__compile", owner: cTmpl,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			src, err := hamlCompile(strArg(args[0]), haml.Options{})
			if err != nil {
				raise("Haml::SyntaxError", "%s", err.Error())
			}
			return object.Wrap(object.NewString(src))
		}}
}

// registerHamlErrors installs the Haml::Error / Haml::SyntaxError exception tree
// (Error / SyntaxError < StandardError). Each class is registered both as a nested
// constant of Haml (so Ruby `Haml::SyntaxError` resolves it) and under its
// qualified name in the top-level table (so a re-raised library SyntaxError's
// exceptionObject lookup finds the very same class), matching the gem's
// Haml::Error / Haml::SyntaxError.
func (vm *VM) registerHamlErrors(mod *RClass) {
	std := object.Kind[*RClass](vm.consts["StandardError"])
	reg := func(simple, qualified string, super *RClass) *RClass {
		c := newClass(qualified, super)
		mod.consts[simple] = object.Wrap(c)
		vm.consts[qualified] = object.Wrap(c)
		return c
	}
	e := reg("Error", "Haml::Error", std)
	reg("SyntaxError", "Haml::SyntaxError", e)
}

// hamlCompile is the seam over the go-ruby-haml compiler. The library fails only on
// a genuinely malformed template (Compile returns a *haml.SyntaxError); the error
// branch is exercised both by a real malformed template and by swapping this var in
// a fault-injection test.
var hamlCompile = haml.Compile

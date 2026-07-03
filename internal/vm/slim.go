// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	slim "github.com/go-ruby-slim/slim"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerSlim installs the native backbone of the Slim template engine
// (require "slim"): the Slim::Template class with a private native compiler hook,
// and the Slim::Engine alias. The template scanner/compiler lives entirely in the
// pure-Go github.com/go-ruby-slim/slim library (which compiles a Slim template to
// the Ruby source that renders it, mirroring the gem's Temple pipeline); the
// interpreter-bound pieces — new (with a block source) / render — stay in Ruby
// (prelude.rb) because they need a Binding and eval, exactly as ERB does. The
// runtime helpers the compiled source calls (Slim::Helpers.escape_html /
// render_attribute / render_attributes) are also supplied in the prelude.
//
// The class is created here so the native hook __compile can be a private instance
// method; the prelude reopens Slim::Template to add the public compile-to-eval API.
func (vm *VM) registerSlim() {
	mod := newClass("Slim", nil)
	mod.isModule = true
	vm.consts["Slim"] = mod
	vm.registerSlimErrors(mod)

	cTmpl := newClass("Template", vm.cObject)
	mod.consts["Template"] = cTmpl
	vm.consts["Slim::Template"] = cTmpl
	// Slim::Engine names the same class (the gem's Slim::Engine is the Temple
	// engine; here the one Template class carries both roles).
	mod.consts["Engine"] = cTmpl
	vm.consts["Slim::Engine"] = cTmpl

	// __compile(template) -> src. The single bridge into the library: it returns
	// the Ruby source the compiled template evaluates to (assigning the buffer to
	// BufVar, appending fragments and evaluating to the buffer). A malformed
	// template surfaces as a Slim::Error so the contract stays total on the Ruby
	// side; well-formed templates always compile.
	cTmpl.methods["__compile"] = &Method{name: "__compile", owner: cTmpl,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			src, err := slimCompile(strArg(args[0]), slim.Options{})
			if err != nil {
				raise("Slim::Error", "%s", err.Error())
			}
			return object.NewString(src)
		}}
}

// registerSlimErrors installs the Slim::Error exception tree (Error < StandardError).
// It is registered both as a nested constant of Slim (so Ruby `Slim::Error`
// resolves it) and under its qualified name in the top-level table (so a re-raised
// library error's exceptionObject lookup finds the very same class).
func (vm *VM) registerSlimErrors(mod *RClass) {
	std := object.Kind[*RClass](vm.consts["StandardError"])
	c := newClass("Slim::Error", std)
	mod.consts["Error"] = c
	vm.consts["Slim::Error"] = c
}

// slimCompile is the seam over the go-ruby-slim compiler. The library never fails
// on a well-formed template (Compile's err is reserved for genuinely malformed
// templates); the error branch is exercised by swapping this var in a
// fault-injection test.
var slimCompile = slim.Compile

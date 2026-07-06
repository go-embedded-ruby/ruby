// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/object"
	erubi "github.com/go-ruby-erubi/erubi"
)

// registerErubi installs the erubi template engine (require "erubi" /
// "erubi/capture_end"): the modern, frozen-string ERB compiler that Rails uses
// by default. Like ERB (erb.go), erubi is a template-to-Ruby-source compiler —
// Erubi::Engine.new(template, **opts).src is the Ruby the host then evals — so
// the scan/compile lives entirely in the pure-Go github.com/go-ruby-erubi/erubi
// library (byte-for-byte with the erubi gem 1.13.1) and rbgo evals the emitted
// #src. That emitted source calls ::Erubi.h to HTML-escape <%= %> output, so the
// Erubi.h module function is wired straight to the library's HTMLEscape here,
// making an eval of the compiled source render correctly inside rbgo.
func (vm *VM) registerErubi() {
	mod := newClass("Erubi", nil)
	mod.isModule = true
	vm.consts["Erubi"] = mod

	// Erubi.h(value) — the escape helper the compiled #src calls (as ::Erubi.h /
	// the hoisted __erubi.h). It HTML-escapes value.to_s through the library's
	// HTMLEscape (MRI-faithful, the same output the gem's Erubi.h produces), so a
	// non-String is coerced with #to_s first (nil -> "", 123 -> "123").
	mod.smethods["h"] = &Method{name: "h", owner: mod, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(erubi.HTMLEscape(erbToS(vm, args[0])))
	}}

	// Erubi::Engine — the compiled template. new(template, **opts) compiles through
	// go-ruby-erubi and returns an engine exposing #src / #filename / #bufvar.
	engine := newClass("Erubi::Engine", vm.cObject)
	mod.consts["Engine"] = engine
	engine.smethods["new"] = &Method{name: "new", owner: engine, native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return buildErubiEngine(engine, erubiEngineCtor, args)
	}}
	vm.defineErubiReaders(engine)

	// Erubi::CaptureEndEngine — the block-capture variant (the <%|= ... %> ... <%|
	// end %> form Rails uses for form_with-style helpers). It subclasses Engine, so
	// it inherits the #src / #filename / #bufvar readers.
	capture := newClass("Erubi::CaptureEndEngine", engine)
	mod.consts["CaptureEndEngine"] = capture
	capture.smethods["new"] = &Method{name: "new", owner: capture, native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return buildErubiEngine(capture, erubiCaptureEndCtor, args)
	}}
}

// defineErubiReaders installs Erubi::Engine's #src / #filename / #bufvar readers,
// each returning the corresponding compiled output the library produced.
func (vm *VM) defineErubiReaders(c *RClass) {
	c.define("src", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*ErubiEngine).src)
	})
	c.define("filename", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if e := self.(*ErubiEngine); e.filename != "" {
			return object.NewString(e.filename)
		}
		return object.NilV
	})
	c.define("bufvar", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*ErubiEngine).bufvar)
	})
}

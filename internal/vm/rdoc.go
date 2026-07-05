// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerRDoc installs the RDoc module (require "rdoc" / "rdoc/markup"): the
// markup-to-output surface the gem is overwhelmingly used for — RDoc::Markup and
// the three output formatters RDoc::Markup::ToHtml / ToMarkdown / ToRdoc, each a
// Formatter whose #convert(text) renders RDoc markup to the target syntax, and
// RDoc::Markup#convert(text, formatter) which drives a formatter over parsed
// markup exactly as the gem's `RDoc::Markup.new.convert(text, formatter)` does.
// The markup parser, the inline attribute manager and the byte-faithful HTML /
// Markdown / RDoc renderers all live in the github.com/go-ruby-rdoc/rdoc library
// — the pure-Go port of RDoc::Markup::Parser / Document / ToHtml from rdoc 7.x;
// this file is the class + method wiring (see rdoc_bind.go for the wrappers).
//
// The gem's file-system walking and on-disk "darkfish" HTML-site generation are
// the documented host seam and out of scope here — the pure, deterministic
// markup->output core is what a host embeds. The error tree mirrors the gem
// (RDoc::Error < StandardError).
func (vm *VM) registerRDoc() {
	mod := newClass("RDoc", nil)
	mod.isModule = true
	vm.consts["RDoc"] = mod

	// RDoc::Error < StandardError, the root of the gem's exception tree.
	std := vm.consts["StandardError"].(*RClass)
	rerr := newClass("RDoc::Error", std)
	mod.consts["Error"] = rerr
	vm.consts["RDoc::Error"] = rerr

	// RDoc::Markup — the front door. new returns a driver; convert(text, formatter)
	// renders parsed markup through the given formatter (its class picks the target
	// syntax), matching the gem's RDoc::Markup#convert.
	markup := newClass("RDoc::Markup", vm.cObject)
	mod.consts["Markup"] = markup
	vm.consts["RDoc::Markup"] = markup
	markup.smethods["new"] = &Method{name: "new", owner: markup,
		native: func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return &RDocMarkup{}
		}}
	markup.define("convert", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		f, ok := args[1].(*RDocFormatter)
		if !ok {
			raise("TypeError", "wrong argument type %s (expected RDoc::Markup formatter)", vm.classOf(args[1]).name)
		}
		return object.NewString(rdocRender(f.kind, strArg(args[0])))
	})

	vm.registerRDocFormatters(markup)
}

// registerRDocFormatters installs the RDoc::Markup::ToHtml / ToMarkdown / ToRdoc
// formatter classes. Each takes an optional options argument in new (the gem
// passes an RDoc::Options; the deterministic library uses its own defaults, so
// the argument is accepted and ignored) and exposes #convert(text) rendering
// markup to that formatter's target syntax through the library's convenience
// entry points.
func (vm *VM) registerRDocFormatters(markup *RClass) {
	defs := []struct{ simple, qualified, kind string }{
		{"ToHtml", "RDoc::Markup::ToHtml", "html"},
		{"ToMarkdown", "RDoc::Markup::ToMarkdown", "markdown"},
		{"ToRdoc", "RDoc::Markup::ToRdoc", "rdoc"},
	}
	for _, d := range defs {
		kind := d.kind
		qualified := d.qualified
		c := newClass(qualified, vm.cObject)
		markup.consts[d.simple] = c
		vm.consts[qualified] = c
		c.smethods["new"] = &Method{name: "new", owner: c,
			native: func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
				return &RDocFormatter{kind: kind, clsName: qualified}
			}}
		c.define("convert", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
			}
			return object.NewString(rdocRender(self.(*RDocFormatter).kind, strArg(args[0])))
		})
	}
}

// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerCommonmark installs the Commonmark module (require "commonmark"):
// Commonmark.render_html(md, opts) and the commonmarker-style String#to_html /
// Commonmark.to_html. The parser and HTML renderer live in the
// github.com/go-ruby-commonmark/commonmark library — the pure-Go CommonMark core
// that backs the `commonmarker` gem — and this module is the thin wiring that maps
// a Ruby Markdown String (and an options Hash / Array of extension symbols) to a
// single commonmark.ToHTML call (see commonmark_bind.go). Both Commonmark and
// CommonMarker name the same module object, matching the gem exposing render_*
// under CommonMarker.
func (vm *VM) registerCommonmark() {
	mod := newClass("Commonmark", nil)
	mod.isModule = true
	vm.consts["Commonmark"] = object.Wrap(mod)
	// The commonmarker gem exposes the same API under the CommonMarker name.
	vm.consts["CommonMarker"] = object.Wrap(mod)

	def := func(name string, fn NativeFn) { mod.smethods[name] = &Method{name: name, owner: mod, native: fn} }

	// render_html(md, options = nil) / to_html(md, options = nil) render a Markdown
	// source String to an HTML fragment String. options selects the GFM extensions
	// and renderer flags (see commonmarkOptions).
	render := func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		var opt object.Value
		if len(args) > 1 {
			opt = args[1]
		}
		return object.Wrap(object.NewString(commonmarkRender(vm, commonmarkSourceArg(args[0]), opt)))
	}
	def("render_html", render)
	def("to_html", render)

	// String#to_html renders the receiver as CommonMark (commonmarker installs a
	// String#to_html-style shortcut); an options argument is accepted as for the
	// module method.
	vm.cString.define("to_html", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		var opt object.Value
		if len(args) > 0 {
			opt = args[0]
		}
		return object.Wrap(object.NewString(commonmarkRender(vm, object.Kind[*object.String](self).Str(), opt)))
	})
}

// commonmarkSourceArg coerces the render argument to its Markdown source string: a
// String yields its contents, and any other value its to_s, so a non-String
// argument does not crash the renderer.
func commonmarkSourceArg(v object.Value) string {
	if s, ok := object.KindOK[*object.String](v); ok {
		return s.Str()
	}
	return v.ToS()
}

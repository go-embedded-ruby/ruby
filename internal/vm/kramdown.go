// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerKramdown installs the Kramdown module (require "kramdown"):
// Kramdown::Document.new(src, options).to_html and the one-shot
// Kramdown.to_html(src, options). The parser and HTML renderer live in the
// github.com/go-ruby-kramdown/kramdown library — the pure-Go kramdown core that
// backs the `kramdown` gem — and this module is the thin wiring that maps a Ruby
// Markdown String (and an options Hash) to a single kramdown.ToHTML call (see
// kramdown_bind.go). The Kramdown::Document class is the gem's public entry
// point; a Document wraps a *Kramdown builder holding the source and options so
// #to_html renders on demand.
func (vm *VM) registerKramdown() {
	mod := newClass("Kramdown", nil)
	mod.isModule = true
	vm.consts["Kramdown"] = mod

	// Kramdown.to_html(src, options = nil) is a convenience one-shot the gem does
	// not itself expose but a host commonly wants; it renders a Markdown source
	// String to an HTML String in one call.
	mod.smethods["to_html"] = &Method{name: "to_html", owner: mod,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
			}
			var opt object.Value
			if len(args) > 1 {
				opt = args[1]
			}
			return object.NewString(kramdownRender(kramdownSourceArg(args[0]), opt))
		}}

	// Kramdown::Document is the gem's document class; new(src, options = {}) parses
	// (lazily — rendering happens in #to_html) and #to_html returns the HTML.
	doc := newClass("Kramdown::Document", vm.cObject)
	mod.consts["Document"] = doc
	vm.consts["Kramdown::Document"] = doc

	doc.smethods["new"] = &Method{name: "new", owner: doc,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
			}
			var opt object.Value
			if len(args) > 1 {
				opt = args[1]
			}
			return &KramdownDoc{src: kramdownSourceArg(args[0]), opt: opt}
		}}

	// Kramdown::Document#to_html renders the held source under the held options.
	doc.define("to_html", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		d := self.(*KramdownDoc)
		return object.NewString(kramdownRender(d.src, d.opt))
	})

	// String#to_kramdown_html renders the receiver as kramdown Markdown (a host
	// convenience mirroring the commonmark String#to_html shortcut, kept under a
	// distinct name so it does not collide with the CommonMark one).
	vm.cString.define("to_kramdown_html", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		var opt object.Value
		if len(args) > 0 {
			opt = args[0]
		}
		return object.NewString(kramdownRender(self.(*object.String).Str(), opt))
	})
}

// KramdownDoc is the Ruby wrapper around a parsed kramdown source: it holds the
// source String and the options value so Kramdown::Document#to_html renders on
// demand through the library.
type KramdownDoc struct {
	src string
	opt object.Value
}

func (d *KramdownDoc) ToS() string     { return "#<Kramdown::Document>" }
func (d *KramdownDoc) Inspect() string { return "#<Kramdown::Document>" }
func (d *KramdownDoc) Truthy() bool    { return true }

// kramdownSourceArg coerces a source argument to its Markdown source string: a
// String yields its contents, and any other value its to_s, so a non-String
// argument does not crash the renderer.
func kramdownSourceArg(v object.Value) string {
	if s, ok := v.(*object.String); ok {
		return s.Str()
	}
	return v.ToS()
}

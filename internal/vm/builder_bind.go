// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	xmlbuilder "github.com/go-ruby-builder/builder"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph and the
// interpreter-independent github.com/go-ruby-builder/builder emitter. The markup
// emitter (indentation, escaping, attribute order, PI/DOCTYPE bytes) lives in that
// library; rbgo maps a Ruby method_missing/tag! call's arguments — text, an
// attribute Hash, and a block — into the library's Tag argument shapes, evaluating
// a Ruby block against a child callback as it goes.

// builderOptions maps Builder::XmlMarkup.new's keyword Hash to the library's
// Option list. Only indent: and margin: are honoured (target: is rbgo-owned);
// unknown keys are ignored.
func builderOptions(args []object.Value) []xmlbuilder.Option {
	var opts []xmlbuilder.Option
	for _, a := range args {
		h, ok := object.KindOK[*object.Hash](a)
		if !ok {
			continue
		}
		for _, k := range h.Keys {
			val, _ := h.Get(k)
			switch builderName(k) {
			case "indent":
				opts = append(opts, xmlbuilder.WithIndent(int(intArg(val))))
			case "margin":
				opts = append(opts, xmlbuilder.WithMargin(int(intArg(val))))
			}
		}
	}
	return opts
}

// builderTagArgs maps a method_missing/tag! argument tail (after the element
// name) plus an optional block into the library's Tag variadic args: a Hash
// becomes xmlbuilder.Attrs, a block becomes a func(*XmlMarkup) child callback,
// and any other value becomes the element's text content.
func (vm *VM) builderTagArgs(rest []object.Value, blk *Proc) []any {
	var out []any
	for _, a := range rest {
		if h, ok := object.KindOK[*object.Hash](a); ok {
			out = append(out, builderAttrs(vm, []object.Value{object.Wrap(h)}))
			continue
		}
		out = append(out, builderValueOf(a))
	}
	if blk != nil {
		out = append(out, vm.builderBlockFn(blk))
	}
	return out
}

// builderBlockFn adapts a Ruby block to the library's func(*XmlMarkup) child
// callback: it wraps the child emitter in an XmlMarkup shell and yields that to
// the block, so the block body emits nested elements against the same document.
func (vm *VM) builderBlockFn(blk *Proc) func(*xmlbuilder.XmlMarkup) {
	return func(child *xmlbuilder.XmlMarkup) {
		vm.callBlock(blk, []object.Value{object.Wrap(&XmlMarkup{x: child})})
	}
}

// builderDeclareArgs maps declare!'s argument tail into the library's Declare
// args: a Ruby Symbol becomes an xmlbuilder.Symbol (a bare identifier), a String
// stays a string (double-quoted by the library), a block becomes the internal
// subset callback, and any other value becomes its to_s string.
func (vm *VM) builderDeclareArgs(rest []object.Value, blk *Proc) []any {
	var out []any
	for _, a := range rest {
		{
			__sw17 := a
			switch {
			case object.IsKind[object.Symbol](__sw17):
				n := object.Kind[object.Symbol](__sw17)
				_ = n
				out = append(out, xmlbuilder.Symbol(string(n)))
			case object.IsKind[*object.String](__sw17):
				n := object.Kind[*object.String](__sw17)
				_ = n
				out = append(out, n.Str())
			default:
				n := __sw17
				_ = n
				out = append(out, a.ToS())
			}
		}
	}
	if blk != nil {
		out = append(out, vm.builderBlockFn(blk))
	}
	return out
}

// builderAttrs maps a Ruby attribute Hash (the keyword arguments of an element or
// PI) into the library's ordered Attrs, preserving insertion order and rendering
// each key by its bare name. A nil/absent value stays nil (an empty attribute).
func builderAttrs(vm *VM, args []object.Value) xmlbuilder.Attrs {
	var attrs xmlbuilder.Attrs
	for _, a := range args {
		h, ok := object.KindOK[*object.Hash](a)
		if !ok {
			continue
		}
		for _, k := range h.Keys {
			val, _ := h.Get(k)
			attrs = append(attrs, xmlbuilder.Attr{Key: builderName(k), Value: builderValueOf(val)})
		}
	}
	return attrs
}

// builderValueOf renders a Ruby value to the plain Go value the library
// attribute/text renderer stringifies with to_s: nil for Ruby nil, the Go string
// for a String, the bare name for a Symbol, and the Ruby to_s text otherwise (so
// numbers and booleans print as Ruby would).
func builderValueOf(v object.Value) any {
	{
		__sw18 := v
		switch {
		case object.IsNil(__sw18):
			n := __sw18
			_ = n
			return nil
		case object.IsNilObj(__sw18):
			n := object.NilObj()
			_ = n
			return nil
		case object.IsKind[*object.String](__sw18):
			n := object.Kind[*object.String](__sw18)
			_ = n
			return n.Str()
		case object.IsKind[object.Symbol](__sw18):
			n := object.Kind[object.Symbol](__sw18)
			_ = n
			return string(n)
		}
	}
	return v.ToS()
}

// builderName renders an element name or attribute key (a Symbol, a String, or
// any value) as its bare string name.
func builderName(v object.Value) string {
	{
		__sw19 := v
		switch {
		case object.IsKind[object.Symbol](__sw19):
			n := object.Kind[object.Symbol](__sw19)
			_ = n
			return string(n)
		case object.IsKind[*object.String](__sw19):
			n := object.Kind[*object.String](__sw19)
			_ = n
			return n.Str()
		}
	}
	return v.ToS()
}

// builderContent reads the first argument of text!/<< as its string content (a
// String verbatim, any other value its to_s); no argument yields the empty string.
func builderContent(args []object.Value) string {
	if len(args) == 0 {
		return ""
	}
	if s, ok := object.KindOK[*object.String](args[0]); ok {
		return s.Str()
	}
	return args[0].ToS()
}

// builderString reads the first argument of cdata!/comment! as its string
// content, matching builderContent (the library does its own CDATA/comment
// wrapping).
func builderString(args []object.Value) string { return builderContent(args) }

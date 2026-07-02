// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	xmlbuilder "github.com/go-ruby-builder/builder"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// XmlMarkup is the Ruby wrapper around a *builder.XmlMarkup emitter. The gem
// drives Builder::XmlMarkup through method_missing — xml.person { xml.name("x") }
// turns the missing method name into an element — and this shell reproduces that:
// method_missing routes onto the library's Tag, and the bang methods (text!, <<,
// cdata!, comment!, instruct!, declare!, tag!, target!) map onto their library
// counterparts. The whole markup emitter (indentation, escaping, attribute order,
// PI/DOCTYPE bytes) lives in the github.com/go-ruby-builder/builder library (see
// builder_bind.go); this file is the thin wiring plus rbgo's method_missing → Tag
// and block evaluation.
type XmlMarkup struct {
	x *xmlbuilder.XmlMarkup
}

func (m *XmlMarkup) ToS() string     { return "#<Builder::XmlMarkup>" }
func (m *XmlMarkup) Inspect() string { return "#<Builder::XmlMarkup>" }
func (m *XmlMarkup) Truthy() bool    { return true }

// registerBuilder installs the Builder module and its XmlMarkup class
// (require "builder"): Builder::XmlMarkup.new(indent:/margin:/target:) plus the
// method_missing element DSL and the special methods.
func (vm *VM) registerBuilder() {
	mod := newClass("Builder", nil)
	mod.isModule = true
	vm.consts["Builder"] = mod

	cls := newClass("Builder::XmlMarkup", vm.cObject)
	mod.consts["XmlMarkup"] = cls
	vm.consts["Builder::XmlMarkup"] = cls

	// Builder::XmlMarkup.new(indent: 0, margin: 0, target: nil): the emitter. An
	// explicit target: sink is not modelled (rbgo owns the accumulator), so only
	// indent/margin are honoured; target! reads the accumulated markup.
	cls.smethods["new"] = &Method{name: "new", owner: cls,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return &XmlMarkup{x: xmlbuilder.New(builderOptions(args)...)}
		}}

	d := func(name string, fn NativeFn) { cls.define(name, fn) }
	self := func(v object.Value) *XmlMarkup { return v.(*XmlMarkup) }

	// method_missing(name, *args, &blk) emits an element named after the missing
	// method: xml.br, xml.name("text"), xml.p(id: 1) { … }. It maps straight onto
	// the library's Tag, with the block adapted to a child callback.
	d("method_missing", func(vm *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
		m := self(v)
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments")
		}
		name := builderName(args[0])
		m.x.Tag(name, vm.builderTagArgs(args[1:], blk)...)
		return object.NewString(m.x.Target())
	})

	// respond_to_missing? — the emitter answers every element name dynamically.
	d("respond_to_missing?", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(true)
	})

	// tag!(name, *args, &blk): the explicit element emitter (Builder's tag!),
	// identical to method_missing but with the name given as the first argument.
	d("tag!", func(vm *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
		m := self(v)
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..)")
		}
		name := builderName(args[0])
		m.x.Tag(name, vm.builderTagArgs(args[1:], blk)...)
		return object.NewString(m.x.Target())
	})

	// text!(str) appends escaped character data.
	d("text!", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).x.Text(builderContent(args))
		return object.NewString(self(v).x.Target())
	})

	// <<(str) inserts markup verbatim (no escaping).
	d("<<", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).x.Append(builderContent(args))
		return object.NewString(self(v).x.Target())
	})

	// cdata!(text) wraps text in a CDATA section.
	d("cdata!", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).x.CData(builderString(args))
		return object.NewString(self(v).x.Target())
	})

	// comment!(text) emits an XML comment.
	d("comment!", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).x.Comment(builderString(args))
		return object.NewString(self(v).x.Target())
	})

	// instruct!(directive = :xml, **attrs) emits a processing instruction; with no
	// argument it emits the XML declaration.
	d("instruct!", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		directive := ""
		var rest []object.Value
		if len(args) > 0 {
			directive = builderName(args[0])
			rest = args[1:]
		}
		self(v).x.Instruct(directive, builderAttrs(vm, rest))
		return object.NewString(self(v).x.Target())
	})

	// declare!(inst, *args, &blk) emits a markup declaration (e.g. a DOCTYPE). A
	// Symbol argument prints as a bare identifier, a String double-quoted, and a
	// block is the internal subset.
	d("declare!", func(vm *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
		m := self(v)
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..)")
		}
		inst := builderName(args[0])
		m.x.Declare(inst, vm.builderDeclareArgs(args[1:], blk)...)
		return object.NewString(m.x.Target())
	})

	// target! returns the markup accumulated so far.
	target := func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).x.Target())
	}
	d("target!", target)
	d("to_s", target)
}

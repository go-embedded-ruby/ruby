// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	mustache "github.com/go-ruby-mustache/mustache"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerMustache installs the Mustache module/class (require "mustache"):
// Mustache.render(template, context) — the one-shot form of the gem's
// Mustache.render — and Mustache.new + #render / #render(template, context) for
// the class-based view API. The template compiler and renderer live in the
// github.com/go-ruby-mustache/mustache library; this module is the thin wiring
// that maps a Ruby template String and a context (a Hash / Array / scalar / proc)
// to the library's value model around a single mustache.Render call (see
// mustache_bind.go). The Mustache::Error tree is registered so a re-raised parse
// error rescues as the right Ruby class.
func (vm *VM) registerMustache() {
	cls := newClass("Mustache", vm.cObject)
	vm.consts["Mustache"] = cls
	vm.registerMustacheErrors(cls)

	// Mustache.render(template, context = {}) renders template against context in
	// one shot (the gem's Mustache.render class method).
	cls.smethods["render"] = &Method{name: "render", owner: cls,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
			}
			var ctx object.Value = object.NilV
			if len(args) > 1 {
				ctx = args[1]
			}
			return object.NewString(mustacheRender(vm, mustacheStringArg(args[0]), ctx))
		}}

	// Mustache.new(template: nil) builds a view instance whose @template and view
	// data drive #render — the gem's class-based API.
	cls.smethods["new"] = &Method{name: "new", owner: cls,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			inst := &RObject{class: cls, ivars: map[string]object.Value{}}
			if len(args) > 0 {
				setIvar(inst, "@template", args[0])
			}
			return inst
		}}

	// Mustache#template / #template= expose the instance's template source.
	cls.define("template", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if t := getIvar(self, "@template"); t != nil {
			return t
		}
		return object.NilV
	})
	cls.define("template=", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		setIvar(self, "@template", args[0])
		return args[0]
	})

	// Mustache#render(template = @template, context = self) renders the instance's
	// template (or an explicit override) against the given context (or the
	// instance's own ivars, mirroring a Mustache subclass rendering against its
	// view methods — here modelled by the object's instance variables).
	cls.define("render", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		tmpl := ""
		if len(args) > 0 {
			tmpl = mustacheStringArg(args[0])
		} else if t := getIvar(self, "@template"); t != nil {
			tmpl = mustacheStringArg(t)
		}
		var ctx object.Value = self
		if len(args) > 1 {
			ctx = args[1]
		}
		return object.NewString(mustacheRender(vm, tmpl, ctx))
	})
}

// registerMustacheErrors installs the Mustache::Error exception tree
// (Error / ParseError < StandardError). Each class is registered both as a nested
// constant of Mustache (so Ruby `Mustache::ParseError` resolves it) and under its
// qualified name in the top-level table (so a re-raised library error's
// exceptionObject lookup finds the very same class), exactly as JSON:: classes are.
func (vm *VM) registerMustacheErrors(cls *RClass) {
	std := vm.consts["StandardError"].(*RClass)
	reg := func(simple, qualified string, super *RClass) *RClass {
		c := newClass(qualified, super)
		cls.consts[simple] = c
		vm.consts[qualified] = c
		return c
	}
	e := reg("Error", "Mustache::Error", std)
	reg("ParseError", "Mustache::ParseError", e)
}

// mustacheStringArg coerces a template argument to its source string: a String
// yields its contents, and any other value its to_s.
func mustacheStringArg(v object.Value) string {
	if s, ok := v.(*object.String); ok {
		return s.Str()
	}
	return v.ToS()
}

// mustacheRender renders template against ctx, mapping ctx into the library value
// model and turning a parse error into a Ruby Mustache::ParseError.
func mustacheRender(vm *VM, template string, ctx object.Value) string {
	out, err := mustache.RenderString(template, toMustache(vm, ctx))
	if err != nil {
		raise("Mustache::ParseError", "%s", err.Error())
	}
	return out
}

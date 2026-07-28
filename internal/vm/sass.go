// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	sass "github.com/go-ruby-sass/sass"
	sassc "github.com/go-ruby-sass/sass/sassc"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the `require "sass"` binding: it wires the interpreter-independent
// github.com/go-ruby-sass/sass adapter (the Ruby-facing surface of the pure-Go
// Sass/SCSS compiler go-scss/scss) into rbgo's object graph. The adapter exposes
// the modern sass-embedded entry points — CompileString(source, opts) and
// Compile(path, opts) returning a CompileResult{CSS, ...} — plus the legacy
// SassC::Engine surface in its sassc subpackage. This binding maps those to the
// Ruby module methods Sass.compile / compile_string / compile_file (each returning
// the compiled CSS String) and the SassC::Engine.new(...).render pair. A parse or
// evaluation failure surfaces as Sass::CompileError (SassC::SyntaxError for the
// legacy surface), matching the gem's error classes.

// registerSass installs the Sass module (require "sass") and its Sass::CompileError
// class, wires the three source/file compile entry points, and layers on the legacy
// SassC::Engine surface. compile and compile_string both compile a source String;
// compile_file compiles a file at a path. Each returns the compiled CSS as a String
// and raises Sass::CompileError on a compile failure.
func (vm *VM) registerSass() {
	mod := newClass("Sass", nil)
	mod.isModule = true
	vm.consts["Sass"] = mod

	std := vm.consts["StandardError"].(*RClass)
	errCls := newClass("Sass::CompileError", std)
	mod.consts["CompileError"] = errCls
	vm.consts["Sass::CompileError"] = errCls

	// Sass.compile_string(source, **opts) and Sass.compile(source, **opts) both
	// compile source text (the task's headline entry point) via the adapter's
	// CompileString.
	compileString := func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return object.NewString(sassCompileString(sassSourceArg(args[0]), sassOptions(args)))
	}
	mod.smethods["compile_string"] = &Method{name: "compile_string", owner: mod, native: compileString}
	mod.smethods["compile"] = &Method{name: "compile", owner: mod, native: compileString}

	// Sass.compile_file(path, **opts) compiles a Sass/SCSS file at path via the
	// adapter's Compile.
	mod.smethods["compile_file"] = &Method{name: "compile_file", owner: mod,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
			}
			return object.NewString(sassCompileFile(sassSourceArg(args[0]), sassOptions(args)))
		}}

	vm.registerSassC()
}

// registerSassC installs the legacy SassC module and its SassC::Engine class
// (SassC::Engine.new(template, style:, syntax:).render), layered over the same
// pure-Go engine through the adapter's sassc subpackage. A compile failure raises
// SassC::SyntaxError, the gem's error class.
func (vm *VM) registerSassC() {
	mod := newClass("SassC", nil)
	mod.isModule = true
	vm.consts["SassC"] = mod

	std := vm.consts["StandardError"].(*RClass)
	errCls := newClass("SassC::SyntaxError", std)
	mod.consts["SyntaxError"] = errCls
	vm.consts["SassC::SyntaxError"] = errCls

	engCls := newClass("SassC::Engine", vm.cObject)
	mod.consts["Engine"] = engCls
	vm.consts["SassC::Engine"] = engCls

	// SassC::Engine.new(template, **opts) captures the template and options as a
	// native handle; #render compiles them lazily, matching the gem.
	engCls.smethods["new"] = &Method{name: "new", owner: engCls,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			if len(args) == 0 {
				raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
			}
			inst := &RObject{class: engCls, ivars: map[string]object.Value{}}
			inst.ivars["@__engine"] = &sassCEngine{e: sassc.NewEngine(sassSourceArg(args[0]), sassCOptions(args))}
			return inst
		}}

	// SassC::Engine#render returns the compiled CSS, raising SassC::SyntaxError on
	// failure.
	engCls.define("render", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		css, err := sassCEngineHandle(self).e.Render()
		if err != nil {
			raise("SassC::SyntaxError", "%s", err.Error())
		}
		return object.NewString(css)
	})
}

// sassCEngine is the opaque native handle stored on a SassC::Engine instance (as
// @__engine) by new; #render compiles it. It satisfies object.Value only so it can
// live in an ivar; it is never exposed to Ruby as a first-class value.
type sassCEngine struct{ e *sassc.Engine }

func (h *sassCEngine) ToS() string     { return "#<SassC::Engine>" }
func (h *sassCEngine) Inspect() string { return h.ToS() }
func (h *sassCEngine) Truthy() bool    { return true }

// sassCEngineHandle returns the native engine handle stored on a SassC::Engine
// instance by new. A receiver without one (never initialized) raises.
func sassCEngineHandle(self object.Value) *sassCEngine {
	if h, ok := getIvar(self, "@__engine").(*sassCEngine); ok {
		return h
	}
	raise("SassC::SyntaxError", "engine was not initialized")
	return nil
}

// sassCompileString compiles source text, raising Sass::CompileError on failure.
func sassCompileString(src string, opts *sass.Options) string {
	res, err := sass.CompileString(src, opts)
	if err != nil {
		raise("Sass::CompileError", "%s", err.Error())
	}
	return res.CSS
}

// sassCompileFile compiles the file at path, raising Sass::CompileError on failure
// (a missing file or a compile error).
func sassCompileFile(path string, opts *sass.Options) string {
	res, err := sass.Compile(path, opts)
	if err != nil {
		raise("Sass::CompileError", "%s", err.Error())
	}
	return res.CSS
}

// sassSourceArg coerces the first argument to its source (or path) String: a String
// yields its contents, and any other value its to_s.
func sassSourceArg(v object.Value) string {
	if s, ok := v.(*object.String); ok {
		return s.Str()
	}
	return v.ToS()
}

// sassOptions reads the modern compile options from a call's trailing kwargs Hash
// (syntax:, style:, load_paths:). A call with no trailing Hash yields the adapter
// defaults (SCSS, expanded).
func sassOptions(args []object.Value) *sass.Options {
	o := &sass.Options{}
	h, ok := trailingHash(args)
	if !ok {
		return o
	}
	o.Syntax = sassSyntax(h)
	o.Style = sassStyle(h)
	o.LoadPaths = sassLoadPaths(h)
	return o
}

// sassSyntax reads the syntax: keyword (:scss, :indented/:sass, :css). An absent
// or unrecognised value leaves the adapter default (SCSS).
func sassSyntax(h *object.Hash) sass.Syntax {
	v, ok := h.Get(object.Symbol("syntax"))
	if !ok {
		return ""
	}
	switch sassName(v) {
	case "indented", "sass":
		return sass.SyntaxIndented
	case "css":
		return sass.SyntaxCSS
	default:
		return sass.SyntaxSCSS
	}
}

// sassStyle reads the style: keyword (:expanded, :compressed). An absent value
// leaves the adapter default (expanded).
func sassStyle(h *object.Hash) sass.Style {
	v, ok := h.Get(object.Symbol("style"))
	if !ok {
		return ""
	}
	if sassName(v) == "compressed" {
		return sass.StyleCompressed
	}
	return sass.StyleExpanded
}

// sassLoadPaths reads the load_paths: keyword, an Array of directory Strings the
// engine resolves @use/@forward/@import against. A missing or non-Array value is
// no load paths.
func sassLoadPaths(h *object.Hash) []string {
	v, ok := h.Get(object.Symbol("load_paths"))
	if !ok {
		return nil
	}
	arr, ok := v.(*object.Array)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr.Elems))
	for _, e := range arr.Elems {
		out = append(out, sassName(e))
	}
	return out
}

// sassCOptions reads the legacy SassC::Engine options from a call's trailing kwargs
// Hash (style:, syntax:). An absent Hash yields the SassC defaults.
func sassCOptions(args []object.Value) sassc.EngineOptions {
	o := sassc.EngineOptions{}
	h, ok := trailingHash(args)
	if !ok {
		return o
	}
	if v, ok := h.Get(object.Symbol("style")); ok && sassName(v) == "compressed" {
		o.Style = sassc.StyleCompressed
	}
	if v, ok := h.Get(object.Symbol("syntax")); ok {
		o.Syntax = sassName(v)
	}
	return o
}

// sassName renders an option value (a Symbol or String) as its bare name, falling
// back to to_s for any other value so a stray option does not crash the binding.
func sassName(v object.Value) string {
	switch n := v.(type) {
	case object.Symbol:
		return string(n)
	case *object.String:
		return n.Str()
	}
	return v.ToS()
}

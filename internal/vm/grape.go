// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	grape "github.com/go-ruby-grape/grape"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// GrapeRouter is the Ruby wrapper around a *grape.Router — an ordered set of
// routes that resolves a (method, path) request to a match (Grape's router). The
// route modelling + first-declared-first-served matching lives in the
// github.com/go-ruby-grape/grape library; this shell wires the Ruby verb DSL
// (#get/#post/…) and #match onto it. Binding an endpoint block to a live Ruby
// object is the host's job — a route's handler is any Ruby value, handed back
// verbatim on a match.
type GrapeRouter struct {
	rt *grape.Router
}

func (r *GrapeRouter) ToS() string     { return "#<Grape::Router>" }
func (r *GrapeRouter) Inspect() string { return "#<Grape::Router>" }
func (r *GrapeRouter) Truthy() bool    { return true }

// GrapeRoute is the Ruby wrapper around a *grape.Route — one matched endpoint
// (its method, pattern and opaque handler value).
type GrapeRoute struct {
	rt      *grape.Route
	handler object.Value
}

func (r *GrapeRoute) ToS() string {
	return "#<Grape::Router::Route " + r.rt.Method + " " + r.rt.Pattern + ">"
}
func (r *GrapeRoute) Inspect() string { return r.ToS() }
func (r *GrapeRoute) Truthy() bool    { return true }

// GrapeMatch is the Ruby wrapper around a grape.Match — a routing decision
// (#status/#route/#params/#allowed).
type GrapeMatch struct {
	m       grape.Match
	route   object.Value
	handler object.Value
}

func (m *GrapeMatch) ToS() string     { return "#<Grape::Router::Match>" }
func (m *GrapeMatch) Inspect() string { return m.ToS() }
func (m *GrapeMatch) Truthy() bool    { return true }

// GrapeValidator is the Ruby wrapper around a *grape.ParamsValidator plus the
// ParamSet a `params do … end` DSL built. #validate coerces a raw params Hash
// into the coerced Hash or raises Grape::Exceptions::ValidationErrors carrying
// Grape's exact messages.
type GrapeValidator struct {
	set *grape.ParamSet
	v   *grape.ParamsValidator
}

func (v *GrapeValidator) ToS() string     { return "#<Grape::Validations::ParamsScope>" }
func (v *GrapeValidator) Inspect() string { return v.ToS() }
func (v *GrapeValidator) Truthy() bool    { return true }

// GrapeParamsBuilder is the DSL self a `params do … end` block runs against; its
// requires / optional methods append Params to the ParamSet under construction.
type GrapeParamsBuilder struct {
	set *grape.ParamSet
}

func (b *GrapeParamsBuilder) ToS() string     { return "#<Grape::Validations::ParamsScope::DSL>" }
func (b *GrapeParamsBuilder) Inspect() string { return b.ToS() }
func (b *GrapeParamsBuilder) Truthy() bool    { return true }

// GrapeFormatter is the Ruby wrapper around a grape.Formatter — serialises a
// response value tree to json / txt / xml (Grape's built-in formatters).
type GrapeFormatter struct{}

func (f *GrapeFormatter) ToS() string     { return "#<Grape::Formatter>" }
func (f *GrapeFormatter) Inspect() string { return f.ToS() }
func (f *GrapeFormatter) Truthy() bool    { return true }

// registerGrape installs the Grape module and its Router / Validator / Formatter
// surface (require "grape"): Grape::Router.new drives #get/#post/#put/#patch/
// #delete/#head route declaration and #match(method, path) -> a Match
// (#status/#route/#params/#allowed); Grape::Validator.new { params DSL } builds a
// params scope whose #validate(raw) coerces a Hash or raises
// Grape::Exceptions::ValidationErrors; Grape::Formatter#format(fmt, value) and
// Grape.mime_for / Grape.default_status expose the deterministic response
// machinery. Endpoint-block execution and Rack env parsing are the host seam.
func (vm *VM) registerGrape() {
	mod := newClass("Grape", nil)
	mod.isModule = true
	vm.consts["Grape"] = mod

	vm.registerGrapeErrors(mod)
	vm.registerGrapeRouter(mod)
	vm.registerGrapeValidator(mod)
	vm.registerGrapeFormatter(mod)

	// Grape.mime_for(format) -> the MIME type Grape emits, or nil for unknown.
	mod.smethods["mime_for"] = &Method{name: "mime_for", owner: mod, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		mime := grape.MimeFor(grapeStr(args[0]))
		if mime == "" {
			return object.NilV
		}
		return object.NewString(mime)
	}}
	// Grape.default_status(method) -> 201 for POST, 200 otherwise.
	mod.smethods["default_status"] = &Method{name: "default_status", owner: mod, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return object.IntValue(int64(grape.DefaultStatus(grapeStr(args[0]))))
	}}
}

// registerGrapeErrors installs Grape::Exceptions::ValidationErrors <
// StandardError (raised by Validator#validate on a failing params Hash), nested
// under Grape::Exceptions and re-exposed under its qualified name.
func (vm *VM) registerGrapeErrors(mod *RClass) {
	std := object.Kind[*RClass](vm.consts["StandardError"])
	exc := newClass("Grape::Exceptions", nil)
	exc.isModule = true
	mod.consts["Exceptions"] = exc
	vm.consts["Grape::Exceptions"] = exc

	base := newClass("Grape::Exceptions::Base", std)
	exc.consts["Base"] = base
	vm.consts["Grape::Exceptions::Base"] = base

	ve := newClass("Grape::Exceptions::ValidationErrors", base)
	exc.consts["ValidationErrors"] = ve
	vm.consts["Grape::Exceptions::ValidationErrors"] = ve
}

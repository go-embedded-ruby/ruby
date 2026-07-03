// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	sinatra "github.com/go-ruby-sinatra/sinatra"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// sinatraDef is the per-class declaration a Sinatra::Base subclass accumulates
// as its body runs the class-level DSL (get/post/…, before/after, not_found,
// error, set/enable/disable). Each subclass owns one; App.new.call(env) turns it
// (and its ancestors') into a live github.com/go-ruby-sinatra app to serve the
// request. The route/filter handler is the Ruby block captured verbatim; running
// it against a request context is the rbgo seam.
type sinatraDef struct {
	routes   []sinatraRoute
	befores  []sinatraFilter
	afters   []sinatraFilter
	notFound *Proc
	errors   map[int]*Proc
	settings map[string]object.Value
}

// sinatraRoute is one class-DSL route: an HTTP verb, a Mustermann pattern and
// the handler block.
type sinatraRoute struct {
	verb    string
	pattern string
	blk     *Proc
}

// sinatraFilter is one before/after filter: an optional pattern ("" = every
// request) and the block.
type sinatraFilter struct {
	pattern string
	blk     *Proc
}

// SinatraCtx is the self a route/filter block runs against — the request-scoped
// Sinatra helper surface (params, request, response, status, body, headers,
// content_type, redirect, halt, pass, session, settings). It wraps the
// library's per-request *sinatra.Context; the block is instance_eval'd with this
// as self so a bare `params` / `halt` resolves here.
type SinatraCtx struct {
	c        *sinatra.Context
	cls      *RClass
	settings map[string]object.Value // the app's merged set/enable/disable values, for the `settings` helper
}

func (c *SinatraCtx) ToS() string     { return "#<Sinatra::Base request>" }
func (c *SinatraCtx) Inspect() string { return c.ToS() }
func (c *SinatraCtx) Truthy() bool    { return true }

// SinatraSettings is the read view a handler's `settings` helper returns: it
// exposes the class-DSL set/enable/disable values via #[] and method_missing so
// `settings.foo` / `settings.foo?` read what the app declared.
type SinatraSettings struct {
	settings map[string]object.Value
	cls      *RClass
}

func (s *SinatraSettings) ToS() string     { return "#<Sinatra::Base settings>" }
func (s *SinatraSettings) Inspect() string { return s.ToS() }
func (s *SinatraSettings) Truthy() bool    { return true }

// sinatraDefFor returns the sinatraDef for a Sinatra::Base subclass, creating it
// on first use. Definitions are keyed by the class object, so each subclass keeps
// its own route table (and ancestors are walked at dispatch time).
func (vm *VM) sinatraDefFor(cls *RClass) *sinatraDef {
	if vm.sinatraDefs == nil {
		vm.sinatraDefs = map[*RClass]*sinatraDef{}
	}
	d, ok := vm.sinatraDefs[cls]
	if !ok {
		d = &sinatraDef{errors: map[int]*Proc{}, settings: map[string]object.Value{}}
		vm.sinatraDefs[cls] = d
	}
	return d
}

// sinatraChain returns cls and its ancestors up to (and excluding) Sinatra::Base,
// outermost-ancestor first, so a subclass inherits its parents' routes/filters
// (parents declared earlier win, matching Ruby's method-declaration order).
func (vm *VM) sinatraChain(cls *RClass) []*sinatraDef {
	var classes []*RClass
	for c := cls; c != nil && c != vm.cSinatraBase; c = c.super {
		classes = append(classes, c)
	}
	// Reverse so the outermost ancestor's routes register first.
	defs := make([]*sinatraDef, 0, len(classes))
	for i := len(classes) - 1; i >= 0; i-- {
		if d, ok := vm.sinatraDefs[classes[i]]; ok {
			defs = append(defs, d)
		}
	}
	return defs
}

// registerSinatra installs the Sinatra module and Sinatra::Base (require
// "sinatra/base" / "sinatra"): the class-level routing DSL (get/post/put/delete/
// patch/options/head), the before/after filters, not_found / error handlers and
// set/enable/disable settings, plus the instance #call(env) Rack adapter that
// dispatches a request through the go-ruby-sinatra router and returns the SPEC
// [status, headers, body] triple. Route handler blocks run against a SinatraCtx
// (params/request/response/status/body/headers/content_type/redirect/halt/pass);
// their evaluation is the rbgo seam, the routing/params/dispatch is the library.
func (vm *VM) registerSinatra() {
	mod := newClass("Sinatra", nil)
	mod.isModule = true
	vm.consts["Sinatra"] = mod

	base := newClass("Sinatra::Base", vm.cObject)
	mod.consts["Base"] = base
	vm.consts["Sinatra::Base"] = base
	vm.cSinatraBase = base

	ctx := newClass("Sinatra::Base::Context", vm.cObject)
	base.consts["Context"] = ctx
	vm.cSinatraCtx = ctx

	settings := newClass("Sinatra::Base::Settings", vm.cObject)
	base.consts["Settings"] = settings
	vm.cSinatraSettings = settings

	vm.registerSinatraDSL(base)
	vm.registerSinatraContext(ctx)
	vm.registerSinatraSettings(settings)

	// Sinatra::NotFound < StandardError, raised/checked for an unmatched route
	// (Sinatra maps it to a 404). Provided so `require "sinatra/base"` exposes the
	// name application code rescues.
	std := object.Kind[*RClass](vm.consts["StandardError"])
	nf := newClass("Sinatra::NotFound", std)
	mod.consts["NotFound"] = nf
	vm.consts["Sinatra::NotFound"] = nf
}

// sinatraStr coerces an argument to its String contents.
func sinatraStr(v object.Value) string {
	{
		__sw160 := v
		switch {
		case object.IsKind[*object.String](__sw160):
			n := object.Kind[*object.String](__sw160)
			_ = n
			return n.Str()
		case object.IsKind[object.Symbol](__sw160):
			n := object.Kind[object.Symbol](__sw160)
			_ = n
			return string(n)
		}
	}
	return v.ToS()
}

// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"github.com/go-ruby-actionpack/actionpack/dispatch"
	"github.com/go-ruby-actionpack/actionpack/parameters"
	"github.com/go-ruby-actionpack/actionpack/routing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file (with actionpack_routing.go, actionpack_controller.go,
// actionpack_params.go and actionpack_dispatch.go) binds
// github.com/go-ruby-actionpack/actionpack — a pure-Go (CGO=0) port of the four
// deterministic cores of Rails' Action Pack: the routing DSL/route set
// (ActionDispatch::Routing), the controller dispatch core (AbstractController +
// ActionController::Metal), strong parameters (ActionController::Parameters) and
// the HTTP layer (ActionDispatch::Request/Response) — into rbgo (require
// "action_controller" / "action_dispatch"):
//
//	class PostsController < ActionController::Base
//	  before_action :authenticate, except: [:index]
//	  rescue_from ArgumentError do |e| render plain: "bad: #{e.message}", status: 400 end
//	  def index; render plain: "posts"; end
//	  def show;  render plain: "post #{params[:id]}"; end
//	end
//	PostsController.dispatch(:index, env)  # => [status, headers, body] Rack triple
//
//	routes = ActionDispatch::Routing::RouteSet.new
//	routes.draw { resources :posts; root to: "home#index" }
//	routes.recognize("GET", "/posts/1")    # => {"controller"=>"posts", "action"=>"show", "id"=>"1"}
//	routes.path("post", id: 1)             # => "/posts/1"
//
// The library owns the route pattern compiler + recognizer + reverse routing,
// the filter/rescue/render dispatch pipeline and the strong-parameters
// permit/require semantics. rbgo supplies the seams the library leaves
// injectable, all run inline on the VM goroutine under the GVL:
//
//   - RunAction — the controller's Ruby action method (`def index; …; end`),
//     sent to the controller instance; the primary seam.
//   - the before/after/around filter and rescue_from bodies — Ruby blocks or
//     named instance methods, sent to the controller instance.
//   - Renderer — the view-layer seam: it sends a `render` message to an
//     ActionView-style context object (set with `view_context`), so the
//     consolidated binary dispatches to the bound actionview; the library owns
//     the plain/status/content-type response assembly.
//
// actionpack does NOT import actionview: rendering is the Renderer seam above.
// The routing/dispatch value objects (RouteSet/Mapper/Parameters/Request/
// Response) each carry their Ruby class so classOf reports it; a controller
// instance is a plain RObject of the user's subclass whose live *controller.Base
// is reached through the @__ac handle (mirroring the ActionCable channel binding).

// ACRouteSet is the Ruby wrapper over a *routing.RouteSet — the ordered route
// collection built by ActionDispatch::Routing::RouteSet.new and its `draw`
// block, queried with recognize/path/url_for. It carries its class so classOf
// reports ActionDispatch::Routing::RouteSet.
type ACRouteSet struct {
	rs  *routing.RouteSet
	cls *RClass
}

func (s *ACRouteSet) ToS() string     { return "#<ActionDispatch::Routing::RouteSet>" }
func (s *ACRouteSet) Inspect() string { return s.ToS() }
func (s *ACRouteSet) Truthy() bool    { return true }

// ACMapper is the self the routes DSL runs against inside `draw do … end` — a
// thin wrapper over the library's *routing.Mapper exposing the verb matchers,
// resources/resource, root, namespace/scope and member/collection blocks.
type ACMapper struct {
	m   *routing.Mapper
	cls *RClass
}

func (m *ACMapper) ToS() string     { return "#<ActionDispatch::Routing::Mapper>" }
func (m *ACMapper) Inspect() string { return m.ToS() }
func (m *ACMapper) Truthy() bool    { return true }

// ACParams is the Ruby wrapper over a *parameters.Parameters — Rails' strong
// parameters, with permit/require/[]/to_h. It carries its class so classOf
// reports ActionController::Parameters.
type ACParams struct {
	p   *parameters.Parameters
	cls *RClass
}

func (p *ACParams) ToS() string     { return p.p.String() }
func (p *ACParams) Inspect() string { return p.p.String() }
func (p *ACParams) Truthy() bool    { return true }

// ACRequest is the Ruby wrapper over a *dispatch.Request — an ActionDispatch
// request over a Rack env, exposing the merged params, the path parameters and
// the format negotiation. It carries its class so classOf reports
// ActionDispatch::Request.
type ACRequest struct {
	r   *dispatch.Request
	cls *RClass
}

func (r *ACRequest) ToS() string     { return "#<ActionDispatch::Request>" }
func (r *ACRequest) Inspect() string { return r.ToS() }
func (r *ACRequest) Truthy() bool    { return true }

// ACResponse is the Ruby wrapper over a *dispatch.Response — the mutable Rack
// response (status/headers/body/write). It carries its class so classOf reports
// ActionDispatch::Response.
type ACResponse struct {
	r   *dispatch.Response
	cls *RClass
}

func (r *ACResponse) ToS() string     { return "#<ActionDispatch::Response>" }
func (r *ACResponse) Inspect() string { return r.ToS() }
func (r *ACResponse) Truthy() bool    { return true }

// registerActionPack installs the Action Pack surface (require
// "action_controller" / "action_dispatch"): the ActionDispatch module tree
// (Routing::RouteSet/Mapper, Request, Response), the ActionController module
// tree (Base, Metal, Parameters and the ParameterMissing/UnpermittedParameters/
// UnfilteredParameters/DoubleRenderError/ActionNotFound errors) and the
// AbstractController::Base alias. The routing DSL, the controller dispatch
// pipeline, strong parameters and the request/response surfaces are wired in the
// sibling files.
func (vm *VM) registerActionPack() {
	std := vm.consts["StandardError"].(*RClass)

	// --- ActionDispatch -----------------------------------------------------
	dispatchMod := newClass("ActionDispatch", nil)
	dispatchMod.isModule = true
	vm.consts["ActionDispatch"] = dispatchMod

	routingMod := newClass("ActionDispatch::Routing", nil)
	routingMod.isModule = true
	dispatchMod.consts["Routing"] = routingMod
	vm.consts["ActionDispatch::Routing"] = routingMod

	routeSet := newClass("ActionDispatch::Routing::RouteSet", vm.cObject)
	routingMod.consts["RouteSet"] = routeSet
	vm.consts["ActionDispatch::Routing::RouteSet"] = routeSet
	vm.cACRouteSet = routeSet

	mapper := newClass("ActionDispatch::Routing::Mapper", vm.cObject)
	routingMod.consts["Mapper"] = mapper
	vm.consts["ActionDispatch::Routing::Mapper"] = mapper
	vm.cACMapper = mapper

	request := newClass("ActionDispatch::Request", vm.cObject)
	dispatchMod.consts["Request"] = request
	vm.consts["ActionDispatch::Request"] = request
	vm.cACRequest = request

	response := newClass("ActionDispatch::Response", vm.cObject)
	dispatchMod.consts["Response"] = response
	vm.consts["ActionDispatch::Response"] = response
	vm.cACResponse = response

	// --- AbstractController -------------------------------------------------
	abstractMod := newClass("AbstractController", nil)
	abstractMod.isModule = true
	vm.consts["AbstractController"] = abstractMod
	// AbstractController::ActionNotFound / DoubleRenderError — the analogues the
	// library returns, exposed as rescuable error classes.
	acNotFound := newClass("AbstractController::ActionNotFound", std)
	abstractMod.consts["ActionNotFound"] = acNotFound
	vm.consts["AbstractController::ActionNotFound"] = acNotFound
	doubleRender := newClass("AbstractController::DoubleRenderError", std)
	abstractMod.consts["DoubleRenderError"] = doubleRender
	vm.consts["AbstractController::DoubleRenderError"] = doubleRender

	// --- ActionController ---------------------------------------------------
	controllerMod := newClass("ActionController", nil)
	controllerMod.isModule = true
	vm.consts["ActionController"] = controllerMod

	metal := newClass("ActionController::Metal", vm.cObject)
	controllerMod.consts["Metal"] = metal
	vm.consts["ActionController::Metal"] = metal

	base := newClass("ActionController::Base", metal)
	controllerMod.consts["Base"] = base
	vm.consts["ActionController::Base"] = base
	vm.cACControllerBase = base

	// AbstractController::Base — the shared ancestor Rails exposes; alias it to
	// ActionController::Metal so `AbstractController::Base` resolves.
	abstractMod.consts["Base"] = metal
	vm.consts["AbstractController::Base"] = metal

	params := newClass("ActionController::Parameters", vm.cObject)
	controllerMod.consts["Parameters"] = params
	vm.consts["ActionController::Parameters"] = params
	vm.cACParameters = params

	// ActionController::ParameterMissing / UnpermittedParameters /
	// UnfilteredParameters — the strong-parameters errors, rescuable by name.
	for _, name := range []string{"ParameterMissing", "UnpermittedParameters", "UnfilteredParameters", "UrlGenerationError"} {
		e := newClass("ActionController::"+name, std)
		controllerMod.consts[name] = e
		vm.consts["ActionController::"+name] = e
	}

	vm.registerACRouteSet(routeSet, mapper)
	vm.registerACParameters(params)
	vm.registerACRequest(request)
	vm.registerACResponse(response)
	vm.registerACController(base)
}

// apStr coerces an argument to its String contents (String or Symbol), else its
// to_s — the shared key/name coercion for the Action Pack DSLs.
func apStr(v object.Value) string {
	switch n := v.(type) {
	case *object.String:
		return n.Str()
	case object.Symbol:
		return string(n)
	}
	return v.ToS()
}

// apInt coerces an argument to an int (for a status / redirect code), yielding 0
// for a non-numeric value.
func apInt(v object.Value) int {
	switch n := v.(type) {
	case object.Integer:
		return int(n)
	case object.Float:
		return int(n)
	}
	return 0
}

// apArg returns args[i] or nil when i is out of range.
func apArg(args []object.Value, i int) object.Value {
	if i < len(args) {
		return args[i]
	}
	return object.NilV
}

// apStrList reads a String/Symbol or an Array of them into a []string — the
// shape used by via:, only: and except:.
func apStrList(v object.Value) []string {
	if arr, ok := v.(*object.Array); ok {
		out := make([]string, len(arr.Elems))
		for i, e := range arr.Elems {
			out[i] = apStr(e)
		}
		return out
	}
	return []string{apStr(v)}
}

// apStrMap reads a Ruby Hash into a string→string map (stringifying keys and
// values), the shape constraints:/defaults: compile to; a non-Hash yields nil.
func apStrMap(v object.Value) map[string]string {
	h, ok := v.(*object.Hash)
	if !ok {
		return nil
	}
	out := make(map[string]string, len(h.Keys))
	for _, k := range h.Keys {
		val, _ := h.Get(k)
		if rx, ok := val.(*Regexp); ok {
			// A Ruby Regexp constraint (id: /\d+/) contributes its raw, unanchored
			// source — the shape the route pattern compiler expects — not its
			// inspect form (?-mix:…).
			out[apStr(k)] = rx.source
			continue
		}
		out[apStr(k)] = apStr(val)
	}
	return out
}

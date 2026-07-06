// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	hanami "github.com/go-ruby-hanami/hanami"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file (with hanami_bind.go) binds github.com/go-ruby-hanami/hanami — a
// pure-Go (CGO=0) reimplementation of the deterministic core of Ruby's Hanami
// framework, the Router (hanami-router 2.x) and the Action lifecycle
// (hanami-controller) — into rbgo as the Hanami::Router and Hanami::Action
// classes (require "hanami" / "hanami/router" / "hanami/action"):
//
//	router = Hanami::Router.new do
//	  root to: ->(env) { [200, {}, ["home"]] }
//	  get "/books/:id", to: "books.show", as: "book"
//	end
//	router.call(env)                # => [status, headers, body] Rack triple
//
//	class Show < Hanami::Action
//	  before :load_book
//	  def handle(req, resp)
//	    resp.body = "book #{req.params["id"]}"
//	  end
//	end
//	Show.new.call(env)              # => [status, headers, body]
//
// The library owns the segment-trie router (verb helpers, named routes, path
// params, globbing, per-param constraints, scopes, redirects, mounts, path/URL
// helpers) and the action lifecycle (content negotiation, before/after
// callbacks, halt/redirect_to, format/status/header/cookie/session/flash,
// exception handling). rbgo supplies the seams the library leaves injectable:
//
//   - Resolver — the router's `to: "name"` endpoint mapping, wired to a Ruby
//     `resolver:` proc that returns a callable (a Hanami::Action or a Rack app);
//   - ActionCall — the single core action seam, the Ruby `handle(req, resp)`
//     method, run inline on the VM goroutine via vm.send;
//   - ExceptionHandler / SessionLoader / ParamsValidator — wired to the Ruby
//     `handle_exception` / `session_loader` / `params_validator` class blocks
//     where declared, else the library's sensible defaults.
//
// Router#call / Action#call align with rbgo's Rack binding types: both build the
// SPEC [status, headers, body] Array via rackHeadersToHash / rackBodyArray, the
// same helpers roda/sinatra use.

// HanamiRouter is the Ruby wrapper over a *hanami.Router — the segment-trie app
// built by Hanami::Router.new and served with #call(env). It carries its own
// class so classOf reports Hanami::Router.
type HanamiRouter struct {
	rt  *hanami.Router
	cls *RClass
}

func (r *HanamiRouter) ToS() string     { return "#<Hanami::Router>" }
func (r *HanamiRouter) Inspect() string { return r.ToS() }
func (r *HanamiRouter) Truthy() bool    { return true }

// HanamiReq is the Ruby wrapper over a *hanami.Request — the request handed to a
// Hanami action's #handle and its before/after callbacks. It exposes the merged
// params, single-param/format accessors, params-validity, and the session,
// cookies and flash. It carries its own class so classOf reports
// Hanami::Action::Request.
type HanamiReq struct {
	r   *hanami.Request
	cls *RClass
}

func (r *HanamiReq) ToS() string     { return "#<Hanami::Action::Request>" }
func (r *HanamiReq) Inspect() string { return r.ToS() }
func (r *HanamiReq) Truthy() bool    { return true }

// HanamiResp is the Ruby wrapper over a *hanami.Response — the mutable response
// an action's #handle builds up (status/body/format/headers/cookies/session/
// flash), plus redirect_to and halt. It carries its own class so classOf reports
// Hanami::Action::Response.
type HanamiResp struct {
	r   *hanami.Response
	cls *RClass
}

func (r *HanamiResp) ToS() string     { return "#<Hanami::Action::Response>" }
func (r *HanamiResp) Inspect() string { return r.ToS() }
func (r *HanamiResp) Truthy() bool    { return true }

// HanamiFlash is the Ruby wrapper over a *hanami.Flash — the two-generation
// message store reachable through req.flash / resp.flash (#[]/#[]=/keep/empty?).
// It carries its own class so classOf reports Hanami::Action::Flash.
type HanamiFlash struct {
	f   *hanami.Flash
	cls *RClass
}

func (f *HanamiFlash) ToS() string     { return "#<Hanami::Action::Flash>" }
func (f *HanamiFlash) Inspect() string { return f.ToS() }
func (f *HanamiFlash) Truthy() bool    { return true }

// hanamiActionDef is the per-Hanami::Action-subclass declaration the class-level
// DSL accumulates: before/after callbacks, handle_exception handlers, the
// accepted formats, the default status/format, and the params-validation /
// session-loading seam blocks. Each subclass owns one; #call turns it (and its
// ancestors') into a live *hanami.Action. The callback/handler/validator/loader
// bodies are captured Ruby blocks — running them is the rbgo seam.
type hanamiActionDef struct {
	befores       []hanamiCB
	afters        []hanamiCB
	handlers      []*Proc // handle_exception { |err, req, resp| … } → truthy = handled
	accepts       []string
	defaultStatus int // 0 = unset (library defaults to 200)
	defaultFormat string
	validator     *Proc // params_validator { |params| … } → Hash (valid) / String (error)
	sessionLoader *Proc // session_loader { |env| … } → Hash
}

// hanamiCB is one before/after callback: either a captured Ruby block
// (`before { |req, resp| … }`) or a symbol naming an instance method
// (`before :load_book`), which is sent (req, resp) at request time.
type hanamiCB struct {
	blk *Proc
	sym string
}

// registerHanami installs the Hanami module and its Hanami::Router /
// Hanami::Action classes plus the Hanami::Action::Request / ::Response / ::Flash
// value classes (require "hanami" / "hanami/router" / "hanami/action"). The
// router DSL + #call adapter and the action class DSL + lifecycle #call adapter
// are wired in hanami_bind.go.
func (vm *VM) registerHanami() {
	mod := newClass("Hanami", nil)
	mod.isModule = true
	vm.consts["Hanami"] = mod

	router := newClass("Hanami::Router", vm.cObject)
	mod.consts["Router"] = router
	vm.consts["Hanami::Router"] = router
	vm.cHanamiRouter = router

	action := newClass("Hanami::Action", vm.cObject)
	mod.consts["Action"] = action
	vm.consts["Hanami::Action"] = action
	vm.cHanamiAction = action

	req := newClass("Hanami::Action::Request", vm.cObject)
	action.consts["Request"] = req
	vm.consts["Hanami::Action::Request"] = req
	vm.cHanamiRequest = req

	resp := newClass("Hanami::Action::Response", vm.cObject)
	action.consts["Response"] = resp
	vm.consts["Hanami::Action::Response"] = resp
	vm.cHanamiResponse = resp

	flash := newClass("Hanami::Action::Flash", vm.cObject)
	action.consts["Flash"] = flash
	vm.consts["Hanami::Action::Flash"] = flash
	vm.cHanamiFlash = flash

	// Hanami::Error < StandardError — the base error the framework exposes, so
	// application code can rescue it. Registered scoped and flat.
	std := vm.consts["StandardError"].(*RClass)
	herr := newClass("Hanami::Error", std)
	mod.consts["Error"] = herr
	vm.consts["Hanami::Error"] = herr

	vm.registerHanamiRouter(router)
	vm.registerHanamiActionDSL(action)
	vm.registerHanamiRequest(req)
	vm.registerHanamiResponse(resp)
	vm.registerHanamiFlash(flash)
}

// hanamiStr coerces an argument to its String contents (String or Symbol).
func hanamiStr(v object.Value) string {
	switch n := v.(type) {
	case *object.String:
		return n.Str()
	case object.Symbol:
		return string(n)
	}
	return v.ToS()
}

// hanamiInt coerces an argument to an int (for a status / redirect code),
// yielding 0 for a non-numeric value (the library treats 0 as "use the default").
func hanamiInt(v object.Value) int {
	switch n := v.(type) {
	case object.Integer:
		return int(n)
	case object.Float:
		return int(n)
	}
	return 0
}

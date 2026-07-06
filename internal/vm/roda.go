// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	roda "github.com/go-ruby-roda/roda"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file (with roda_bind.go) binds github.com/go-ruby-roda/roda — a pure-Go
// (CGO=0) reimplementation of the routing-tree engine at the heart of Ruby's
// Roda web toolkit — into rbgo as the `Roda` class (require "roda"):
//
//	class MyApp < Roda
//	  route do |r|
//	    r.root { "hello" }
//	    r.on "users", Integer do |id| "user #{id}" end
//	    r.get("about") { "about" }
//	  end
//	end
//	MyApp.call(env)  # => [status, headers, body] Rack triple
//
// The library owns the routing tree: RodaRequest.On/Is/Get/Post/Root peel path
// segments off the request and yield captures, RodaResponse buffers the answer
// and Roda.Call assembles the Rack triple (defaulting to 404 when nothing
// matches). rbgo supplies the two block seams the engine leaves injectable:
// RouteBlock (the top-level `route do |r| … end`) and Handler (each matcher's
// block). Both run the captured Ruby block against a RodaReq — running the block
// is the rbgo seam, all segment consumption/matching/dispatch is the library.
// The library unwinds a matched (or halted/redirected) branch with a panic that
// Roda.Call recovers, exactly like Sinatra's halt; that panic travels up through
// the Ruby call stack untouched (see roda_bind.go).

// RodaReq is the self a Roda route/matcher block runs against — a thin wrapper
// over the library's per-request *roda.RodaRequest exposing the matcher surface
// (on/is/get/post/put/delete/root, redirect/halt, params/path/request_method,
// captures, response). It carries its own class so classOf reports
// Roda::RodaRequest.
type RodaReq struct {
	r   *roda.RodaRequest
	cls *RClass
}

func (r *RodaReq) ToS() string     { return "#<Roda::RodaRequest>" }
func (r *RodaReq) Inspect() string { return r.ToS() }
func (r *RodaReq) Truthy() bool    { return true }

// RodaResp is the Ruby wrapper over the library's *roda.RodaResponse — the
// mutable response a route block writes into (status/headers/body), exposed
// through #[]/#[]=, #status/#status=, #write, #redirect and #finish. It carries
// its own class so classOf reports Roda::RodaResponse.
type RodaResp struct {
	r   *roda.RodaResponse
	cls *RClass
}

func (r *RodaResp) ToS() string     { return "#<Roda::RodaResponse>" }
func (r *RodaResp) Inspect() string { return r.ToS() }
func (r *RodaResp) Truthy() bool    { return true }

// registerRoda installs the Roda class and its Roda::RodaRequest /
// Roda::RodaResponse value classes (require "roda"). A Roda app is written as a
// subclass with a `route do |r| … end` declaration and served with App.call(env)
// / App.new.call(env), returning the Rack [status, headers, body] triple. The
// class-level DSL and the #call adapter are wired in roda_bind.go.
func (vm *VM) registerRoda() {
	base := newClass("Roda", vm.cObject)
	vm.consts["Roda"] = base
	vm.cRodaBase = base

	req := newClass("Roda::RodaRequest", vm.cObject)
	base.consts["RodaRequest"] = req
	vm.consts["Roda::RodaRequest"] = req
	vm.cRodaRequest = req

	resp := newClass("Roda::RodaResponse", vm.cObject)
	base.consts["RodaResponse"] = resp
	vm.consts["Roda::RodaResponse"] = resp
	vm.cRodaResponse = resp

	// Roda::RodaError < StandardError — the base error class the gem exposes, so
	// application code can rescue it. Registered scoped and flat.
	std := vm.consts["StandardError"].(*RClass)
	rerr := newClass("Roda::RodaError", std)
	base.consts["RodaError"] = rerr
	vm.consts["Roda::RodaError"] = rerr

	vm.registerRodaDSL(base)
	vm.registerRodaRequest(req)
	vm.registerRodaResponse(resp)
}

// rodaStr coerces an argument to its String contents (String or Symbol).
func rodaStr(v object.Value) string {
	switch n := v.(type) {
	case *object.String:
		return n.Str()
	case object.Symbol:
		return string(n)
	}
	return v.ToS()
}

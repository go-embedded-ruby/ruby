// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	rack "github.com/go-ruby-rack/rack"
	roda "github.com/go-ruby-roda/roda"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file wires the Roda class-level DSL (`route do |r| … end`), the instance/
// class #call Rack adapter, and the RodaRequest/RodaResponse method surface over
// github.com/go-ruby-roda/roda. The route block and every matcher block is the
// captured Ruby block, run against a RodaReq — the block eval is the rbgo seam,
// the routing-tree matching/dispatch is the library.

// registerRodaDSL installs the class-level `route` declaration and the #call
// Rack adapter on the Roda class (inherited by every subclass).
func (vm *VM) registerRodaDSL(base *RClass) {
	// route do |r| … end — records the top-level route block on the class.
	base.smethods["route"] = &Method{name: "route", owner: base, native: func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("ArgumentError", "Roda.route requires a block")
		}
		if vm.rodaRoutes == nil {
			vm.rodaRoutes = map[*RClass]*Proc{}
		}
		vm.rodaRoutes[vm.rodaClassOf(self)] = blk
		return object.NilV
	}}

	// #call(env) — the Rack adapter, as an instance method (App.new.call(env)) and
	// a class method (App.call(env)); both dispatch the same route block.
	callFn := func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.rodaCall(vm.rodaClassOf(self), rackArg(args))
	}
	base.methods["call"] = &Method{name: "call", owner: base, native: callFn}
	base.smethods["call"] = &Method{name: "call", owner: base, native: callFn}
}

// rodaClassOf resolves the Roda subclass a DSL/#call receiver refers to: the
// class itself for a class-level call, or the instance's class for App.new.call.
func (vm *VM) rodaClassOf(self object.Value) *RClass {
	if c, ok := self.(*RClass); ok {
		return c
	}
	return vm.classOf(self)
}

// rodaRouteFor returns the route block declared on cls or its nearest Roda
// ancestor (a subclass inherits its parent's `route` unless it declares its own).
func (vm *VM) rodaRouteFor(cls *RClass) *Proc {
	for c := cls; c != nil && c != vm.cObject; c = c.super {
		if blk, ok := vm.rodaRoutes[c]; ok {
			return blk
		}
	}
	return nil
}

// rodaCall builds the library app from cls's route block, serves the Rack env
// and returns the SPEC [status, headers, body] triple as a Ruby Array.
func (vm *VM) rodaCall(cls *RClass, envArg object.Value) object.Value {
	blk := vm.rodaRouteFor(cls)
	if blk == nil {
		raise("Roda::RodaError", "no route block defined for %s", cls.name)
	}
	app := roda.New(vm.rodaRouteBlock(blk))
	status, headers, body := app.Call(rackEnv(envArg))
	return object.NewArray(object.IntValue(int64(status)), rackHeadersToHash(headers), rackBodyArray(body))
}

// rodaRouteBlock adapts the top-level Ruby route block into a roda.RouteBlock: it
// runs `route do |r| … end` against a fresh RodaReq wrapping the per-request
// *roda.RodaRequest and maps the block's return value into a response body.
func (vm *VM) rodaRouteBlock(blk *Proc) roda.RouteBlock {
	return func(rr *roda.RodaRequest) (bool, any) {
		req := &RodaReq{r: rr, cls: vm.cRodaRequest}
		return rodaBodyResult(vm.callBlock(blk, []object.Value{req}))
	}
}

// rodaHandler adapts a matcher's Ruby block into a roda.Handler: when the branch
// matches, the engine calls this with the captures accumulated so far; it runs
// the block (yielding those captures as block params) and maps its return value
// into a body. The block re-enters the tree via the same shared RodaRequest
// (referenced through the enclosing Ruby `r`), so nested routing works.
func (vm *VM) rodaHandler(blk *Proc) roda.Handler {
	return func(rr *roda.RodaRequest, captures []any) (bool, any) {
		return rodaBodyResult(vm.callBlock(blk, rodaCaptures(captures)))
	}
}

// rodaBodyResult maps a Ruby block's return value into the (handled, body) pair
// a roda.Handler/RouteBlock returns: a String is a one-part body, an Array is
// its stringified parts, anything else writes nothing (Roda only treats a String
// return as the response body).
func rodaBodyResult(v object.Value) (bool, any) {
	switch n := v.(type) {
	case *object.String:
		return true, n.Str()
	case *object.Array:
		parts := make([]string, len(n.Elems))
		for i, e := range n.Elems {
			parts[i] = rackStr(e)
		}
		return true, parts
	}
	return false, nil
}

// rodaCaptures converts the library's accumulated captures (strings from Sym/
// String matchers, ints from the Integer matcher, bools) into Ruby block params.
func rodaCaptures(caps []any) []object.Value {
	out := make([]object.Value, len(caps))
	for i, c := range caps {
		out[i] = rodaCaptureValue(c)
	}
	return out
}

func rodaCaptureValue(c any) object.Value {
	switch v := c.(type) {
	case string:
		return object.NewString(v)
	case int:
		return object.IntValue(int64(v))
	case int64:
		return object.IntValue(v)
	case bool:
		return object.Bool(v)
	}
	return object.NilV
}

// rodaMatchers converts Ruby matcher arguments into the library's matcher values.
func (vm *VM) rodaMatchers(args []object.Value) []any {
	out := make([]any, 0, len(args))
	for _, a := range args {
		out = append(out, vm.rodaMatcher(a))
	}
	return out
}

// rodaMatcher maps one Ruby matcher argument onto a library matcher: a String is
// a literal segment, a Symbol captures any one segment (roda.Sym), the String /
// Integer class constants are the class matchers, true/false match always/never,
// and a Hash is the keyed matcher (method/param/extension).
func (vm *VM) rodaMatcher(a object.Value) any {
	switch n := a.(type) {
	case *object.String:
		return n.Str()
	case object.Symbol:
		return roda.Sym(string(n))
	case object.Bool:
		return bool(n)
	case object.Integer:
		return n.ToS()
	case *object.Hash:
		return vm.rodaHashMatcher(n)
	case *RClass:
		switch n {
		case vm.consts["String"]:
			return roda.String
		case vm.consts["Integer"]:
			return roda.Integer
		}
		return n.name
	}
	return a.ToS()
}

// rodaHashMatcher builds a roda.Hash matcher from a Ruby Hash, honouring the
// method / param / extension keys (a "method" value may be a String or an Array
// of Strings).
func (vm *VM) rodaHashMatcher(h *object.Hash) roda.Hash {
	m := roda.Hash{}
	for _, k := range h.Keys {
		v, _ := h.Get(k)
		switch rodaStr(k) {
		case "method":
			if arr, ok := v.(*object.Array); ok {
				methods := make([]any, len(arr.Elems))
				for i, e := range arr.Elems {
					methods[i] = rodaStr(e)
				}
				m["method"] = methods
			} else {
				m["method"] = rodaStr(v)
			}
		case "param":
			m["param"] = rodaStr(v)
		case "extension":
			m["extension"] = rodaStr(v)
		}
	}
	return m
}

// registerRodaRequest installs the RodaRequest matcher/accessor surface a route
// block runs against (self = RodaReq).
func (vm *VM) registerRodaRequest(cls *RClass) {
	self := func(v object.Value) *roda.RodaRequest { return v.(*RodaReq).r }

	// Matcher methods taking matchers + a handler block.
	matcher := func(fn func(*roda.RodaRequest, roda.Handler, ...any)) NativeFn {
		return func(vm *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
			fn(self(v), vm.rodaHandler(blk), vm.rodaMatchers(args)...)
			return object.NilV
		}
	}
	cls.define("on", matcher((*roda.RodaRequest).On))
	cls.define("is", matcher((*roda.RodaRequest).Is))
	cls.define("get", matcher((*roda.RodaRequest).Get))
	cls.define("post", matcher((*roda.RodaRequest).Post))
	cls.define("put", matcher((*roda.RodaRequest).Put))
	cls.define("delete", matcher((*roda.RodaRequest).Delete))

	// root { … } — a GET whose remaining path is exactly "/".
	cls.define("root", func(vm *VM, v object.Value, _ []object.Value, blk *Proc) object.Value {
		self(v).Root(vm.rodaHandler(blk))
		return object.NilV
	})

	// redirect(target, status = 302) — sets the redirect and terminates (panics
	// haltSignal, recovered by Roda.Call up through the Ruby call stack).
	cls.define("redirect", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		status := []int{}
		if len(args) > 1 {
			status = append(status, rodaInt(args[1]))
		}
		// RodaRequest.Redirect always terminates the request via a haltSignal panic
		// recovered by Roda.Call, so the return below is never reached at runtime; it
		// shares this basic block with the call so the block is still accounted for.
		self(v).Redirect(rodaStr(args[0]), status...)
		return object.NilV
	})

	// halt — terminates the request immediately with the current response.
	cls.define("halt", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		self(v).Halt()
		return object.NilV
	})

	// params — the merged request parameters (GET + POST) as a Ruby Hash, built
	// from the Rack env via go-ruby-rack.
	cls.define("params", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		p, err := rack.NewRequest(self(v).Env()).Params()
		if err != nil {
			raise("ArgumentError", "%s", err.Error())
		}
		return rackParamsToHash(p)
	})

	cls.define("path", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		s, _ := self(v).Env()["PATH_INFO"].(string)
		return object.NewString(s)
	})
	cls.define("remaining_path", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).RemainingPath())
	})
	cls.define("request_method", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).RequestMethod())
	})
	cls.define("captures", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewArrayFromSlice(rodaCaptures(self(v).Captures()))
	})
	cls.define("response", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return &RodaResp{r: self(v).Response(), cls: vm.cRodaResponse}
	})
	// request — Roda exposes `r` itself as the request; #request returns self.
	cls.define("request", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return v
	})
}

// registerRodaResponse installs the RodaResponse surface (self = RodaResp).
func (vm *VM) registerRodaResponse(cls *RClass) {
	self := func(v object.Value) *roda.RodaResponse { return v.(*RodaResp).r }

	cls.define("status", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).Status()))
	})
	cls.define("status=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		self(v).SetStatus(rodaInt(args[0]))
		return args[0]
	})
	cls.define("write", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		for _, a := range args {
			self(v).Write(rackStr(a))
		}
		return object.NilV
	})
	cls.define("[]", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return rackFromGo(self(v).GetHeader(rodaStr(args[0])))
	})
	cls.define("[]=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		self(v).SetHeader(rodaStr(args[0]), rackStr(args[1]))
		return args[1]
	})
	cls.define("redirect", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		st := 0
		if len(args) >= 2 {
			st = rodaInt(args[1])
		}
		self(v).Redirect(rodaStr(args[0]), st)
		return object.NilV
	})
	cls.define("headers", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return rackHeadersToHash(self(v).Headers())
	})
	cls.define("body", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return rackBodyArray(self(v).Body())
	})
	cls.define("empty?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).Empty())
	})
	cls.define("finish", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		status, headers, body := self(v).Finish()
		return object.NewArray(object.IntValue(int64(status)), rackHeadersToHash(headers), rackBodyArray(body))
	})
}

// rodaInt coerces an argument to an int (for a status / redirect code).
func rodaInt(v object.Value) int {
	switch n := v.(type) {
	case object.Integer:
		return int(n)
	case object.Float:
		return int(n)
	}
	return 0
}

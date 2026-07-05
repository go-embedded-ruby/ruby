// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	sinatra "github.com/go-ruby-sinatra/sinatra"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file wires the Sinatra::Base class-level DSL and the instance #call Rack
// adapter over github.com/go-ruby-sinatra. The class DSL records routes/filters/
// handlers into a per-class sinatraDef; #call builds a live sinatra app from the
// class (and its ancestors), serves the Rack env and returns the SPEC triple.
// Each route handler is the captured Ruby block, instance_eval'd against a
// SinatraCtx — the block eval is the rbgo seam, the routing/params/dispatch is
// the library.

// registerSinatraDSL installs the class-level routing DSL on Sinatra::Base
// (inherited by every subclass) plus the instance/class #call adapter.
func (vm *VM) registerSinatraDSL(base *RClass) {
	// Verb methods: get/post/put/delete/patch/options/head "pattern" do … end.
	verb := func(name string) NativeFn {
		return func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
			d := vm.sinatraDefFor(sinatraClassOf(self))
			d.routes = append(d.routes, sinatraRoute{verb: name, pattern: sinatraPattern(args), blk: blk})
			return object.NilV
		}
	}
	for _, m := range []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS", "HEAD"} {
		base.smethods[sinatraVerbName(m)] = &Method{name: sinatraVerbName(m), owner: base, native: verb(m)}
	}

	// Filters: before / after [pattern] do … end.
	filter := func(after bool) NativeFn {
		return func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
			d := vm.sinatraDefFor(sinatraClassOf(self))
			f := sinatraFilter{pattern: sinatraOptPattern(args), blk: blk}
			if after {
				d.afters = append(d.afters, f)
			} else {
				d.befores = append(d.befores, f)
			}
			return object.NilV
		}
	}
	base.smethods["before"] = &Method{name: "before", owner: base, native: filter(false)}
	base.smethods["after"] = &Method{name: "after", owner: base, native: filter(true)}

	// not_found do … end.
	base.smethods["not_found"] = &Method{name: "not_found", owner: base, native: func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		vm.sinatraDefFor(sinatraClassOf(self)).notFound = blk
		return object.NilV
	}}

	// error(status = 500) do … end — a bare error registers the 500 handler.
	base.smethods["error"] = &Method{name: "error", owner: base, native: func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		code := 500
		if len(args) > 0 {
			code = sinatraInt(args[0], 500)
		}
		vm.sinatraDefFor(sinatraClassOf(self)).errors[code] = blk
		return object.NilV
	}}

	// set(key, value) / enable(key) / disable(key).
	base.smethods["set"] = &Method{name: "set", owner: base, native: func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		vm.sinatraDefFor(sinatraClassOf(self)).settings[sinatraStr(args[0])] = args[1]
		return object.NilV
	}}
	toggle := func(on bool) NativeFn {
		return func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			d := vm.sinatraDefFor(sinatraClassOf(self))
			for _, a := range args {
				d.settings[sinatraStr(a)] = object.Bool(on)
			}
			return object.NilV
		}
	}
	base.smethods["enable"] = &Method{name: "enable", owner: base, native: toggle(true)}
	base.smethods["disable"] = &Method{name: "disable", owner: base, native: toggle(false)}

	// configure { … } runs its block against the class, so a `set`/`enable` inside
	// registers on the class exactly like a top-level call (env filtering, which
	// Sinatra applies, is a no-op here — the block always runs).
	base.smethods["configure"] = &Method{name: "configure", owner: base, native: func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk != nil {
			vm.callBlockSelf(blk, self, nil)
		}
		return object.NilV
	}}

	// helpers { def foo; …; end } grafts the block's method definitions onto the
	// request-context class, so every handler can call them (Sinatra's helpers).
	base.smethods["helpers"] = &Method{name: "helpers", owner: base, native: func(vm *VM, _ object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk != nil {
			vm.classEval(vm.cSinatraCtx, blk, nil)
		}
		return object.NilV
	}}

	// #call(env) — the Rack adapter, available both as an instance method
	// (App.new.call(env)) and a class method (App.call(env)).
	callFn := func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.sinatraCall(sinatraClassOf(self), rackArg(args))
	}
	base.methods["call"] = &Method{name: "call", owner: base, native: callFn}
	base.methods["call!"] = &Method{name: "call!", owner: base, native: callFn}
	base.smethods["call"] = &Method{name: "call", owner: base, native: callFn}
}

// sinatraCall builds the live sinatra app for cls, serves the Rack env and
// returns the SPEC [status, headers, body] triple as a Ruby Array.
func (vm *VM) sinatraCall(cls *RClass, envArg object.Value) object.Value {
	app := vm.buildSinatraApp(cls)
	// The per-request handler-self cache is scoped to this one dispatch: reset it
	// before serving and drop it after so a request's before/route/after share one
	// SinatraCtx (and its @ivars) without leaking across requests.
	vm.sinatraCtxCache = nil
	defer func() { vm.sinatraCtxCache = nil }()
	status, headers, body := app.CallTuple(rackEnv(envArg))
	return object.NewArray(object.IntValue(int64(status)), rackHeadersToHash(headers), rackBodyArray(body))
}

// buildSinatraApp assembles a *sinatra.Sinatra from cls's declaration chain: it
// applies settings (ancestor first, subclass last so a subclass overrides),
// registers routes/filters and the not_found / error handlers, each backed by an
// Action that instance_evals the captured Ruby block against a SinatraCtx.
func (vm *VM) buildSinatraApp(cls *RClass) *sinatra.Sinatra {
	app := sinatra.New()
	merged := map[string]object.Value{}
	defs := vm.sinatraChain(cls)
	for _, d := range defs {
		for k, v := range d.settings {
			merged[k] = v
		}
	}
	for k, v := range merged {
		app.Settings().Set(k, sinatraSettingValue(v))
	}
	for _, d := range defs {
		for _, r := range d.routes {
			vm.sinatraRegisterRoute(app, r, merged)
		}
		for _, f := range d.befores {
			app.Before(f.pattern, vm.sinatraAction(f.blk, merged))
		}
		for _, f := range d.afters {
			app.After(f.pattern, vm.sinatraAction(f.blk, merged))
		}
		if d.notFound != nil {
			app.NotFound(vm.sinatraAction(d.notFound, merged))
		}
		for code, blk := range d.errors {
			app.Error(code, vm.sinatraAction(blk, merged))
		}
	}
	return app
}

// sinatraRegisterRoute registers one route on the app for its verb.
func (vm *VM) sinatraRegisterRoute(app *sinatra.Sinatra, r sinatraRoute, merged map[string]object.Value) {
	action := vm.sinatraAction(r.blk, merged)
	switch r.verb {
	case "GET":
		app.Get(r.pattern, action)
	case "POST":
		app.Post(r.pattern, action)
	case "PUT":
		app.Put(r.pattern, action)
	case "DELETE":
		app.Delete(r.pattern, action)
	case "PATCH":
		app.Patch(r.pattern, action)
	case "OPTIONS":
		app.Options(r.pattern, action)
	case "HEAD":
		app.Head(r.pattern, action)
	}
}

// sinatraAction turns a captured Ruby block into a sinatra.Action: it builds a
// per-request SinatraCtx and instance_evals the block against it, mapping the
// block's return value into Sinatra's body coercion. halt/redirect/pass unwind
// via the library's panic-based control flow, which the dispatcher recovers.
func (vm *VM) sinatraAction(blk *Proc, merged map[string]object.Value) sinatra.Action {
	return func(c *sinatra.Context) any {
		ctx := vm.sinatraCtxFor(c, merged)
		if blk == nil {
			return nil
		}
		return sinatraResult(vm, vm.callBlockSelf(blk, ctx, nil))
	}
}

// sinatraCtxFor returns the handler self for the request identified by c, reusing
// one SinatraCtx across that request's before filter(s), the route body and the
// after filter(s) — real Sinatra runs the whole request against a single
// instance, so instance variables an app sets in a `before` block (e.g. @user)
// are visible in the route and after blocks. The cache is keyed by the library's
// per-request *sinatra.Context (one per dispatch) and reset around each dispatch
// by sinatraCall.
func (vm *VM) sinatraCtxFor(c *sinatra.Context, merged map[string]object.Value) *SinatraCtx {
	if vm.sinatraCtxCache == nil {
		vm.sinatraCtxCache = map[*sinatra.Context]*SinatraCtx{}
	}
	if ctx, ok := vm.sinatraCtxCache[c]; ok {
		return ctx
	}
	ctx := &SinatraCtx{c: c, cls: vm.cSinatraCtx, settings: merged, ivars: map[string]object.Value{}}
	vm.sinatraCtxCache[c] = ctx
	return ctx
}

// registerSinatraContext installs the request-context helper surface a handler
// block runs against (self = SinatraCtx).
func (vm *VM) registerSinatraContext(ctx *RClass) {
	self := func(v object.Value) *SinatraCtx { return v.(*SinatraCtx) }

	ctx.define("params", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return sinatraParamsHash(self(v).c)
	})
	ctx.define("request", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return &RackRequest{req: self(v).c.Request(), cls: vm.consts["Rack::Request"].(*RClass)}
	})
	ctx.define("response", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return &RackResponse{resp: self(v).c.Response(), cls: vm.consts["Rack::Response"].(*RClass)}
	})
	ctx.define("session", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		// The session seam is the rack.session env value, which lives in the Go
		// value model (the env was converted at #call); map it back for Ruby. A
		// missing session reads back as nil.
		return rackFromGo(self(v).c.Session())
	})
	ctx.define("settings", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return &SinatraSettings{settings: self(v).settings, cls: vm.cSinatraSettings}
	})

	// status — reader with no arg, setter with one.
	ctx.define("status", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		c := self(v).c
		if len(args) == 0 {
			return object.IntValue(int64(c.CurrentStatus()))
		}
		c.Status(sinatraInt(args[0], 200))
		return object.NilV
	})
	ctx.define("body", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		c := self(v).c
		if len(args) == 0 {
			// Reader form: the currently buffered body as a Rack body Array.
			return rackBodyArray(c.Response().Body())
		}
		c.Body(sinatraStr(args[0]))
		return args[0]
	})
	ctx.define("headers", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) > 0 {
			if h, ok := args[0].(*object.Hash); ok {
				c := self(v).c
				for _, k := range h.Keys {
					val, _ := h.Get(k)
					c.Header(sinatraStr(k), sinatraStr(val))
				}
			}
		}
		return rackHeadersToHash(self(v).c.Response().Headers())
	})
	ctx.define("content_type", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		sinatraSetContentType(self(v).c, args)
		return object.NilV
	})
	ctx.define("redirect", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		status := []int{}
		if len(args) > 1 {
			status = append(status, sinatraInt(args[1], 302))
		}
		self(v).c.Redirect(sinatraStr(args[0]), status...)
		return object.NilV
	})
	ctx.define("halt", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).c.Halt(sinatraHaltArgs(args)...)
		return object.NilV
	})
	ctx.define("pass", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		self(v).c.Pass()
		return object.NilV
	})
	ctx.define("uri", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).c.URI(sinatraStr(rackArg(args))))
	})
	ctx.define("url", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).c.URI(sinatraStr(rackArg(args))))
	})
}

// registerSinatraSettings installs the read view returned by the `settings`
// helper: #[] and method_missing read the class-DSL set/enable/disable values.
func (vm *VM) registerSinatraSettings(cls *RClass) {
	get := func(s *SinatraSettings, key string) object.Value {
		if v, ok := s.settings[key]; ok {
			return v
		}
		return object.NilV
	}
	cls.define("[]", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return get(v.(*SinatraSettings), sinatraStr(rackArg(args)))
	})
	cls.define("respond_to?", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		_, ok := v.(*SinatraSettings).settings[sinatraStr(rackArg(args))]
		return object.Bool(ok)
	})
	cls.define("method_missing", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s := v.(*SinatraSettings)
		name := sinatraStr(rackArg(args))
		if n := len(name); n > 0 && name[n-1] == '?' {
			key := name[:n-1]
			val, ok := s.settings[key]
			return object.Bool(ok && val.Truthy())
		}
		return get(s, name)
	})
}

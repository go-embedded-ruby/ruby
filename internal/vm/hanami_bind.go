// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"

	hanami "github.com/go-ruby-hanami/hanami"
	rack "github.com/go-ruby-rack/rack"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file wires the Hanami::Router class DSL (verb helpers + block form) and
// #call Rack adapter, the Hanami::Action class DSL (before/after/handle_exception/
// accept/config) and lifecycle #call adapter, and the Request/Response/Flash
// method surfaces over github.com/go-ruby-hanami/hanami. The endpoint Resolver,
// the action #handle body, and the callback/handler/validator/loader blocks are
// the rbgo seams; the routing tree and the lifecycle are the library.

// --- Router ----------------------------------------------------------------

// registerHanamiRouter installs Hanami::Router.new (wrapping a *hanami.Router),
// the verb/route declaration DSL and the #call Rack adapter.
func (vm *VM) registerHanamiRouter(cls *RClass) {
	// Hanami::Router.new(resolver:, scheme:, host:) { … } — builds the wrapper and,
	// when a block is given, instance_evals it against the router so a bare
	// `get "/", to: …` inside resolves to the router's DSL methods.
	cls.smethods["new"] = &Method{name: "new", owner: cls, native: func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		opts := hanamiRouterOptions(vm, args)
		w := &HanamiRouter{rt: hanami.NewRouter(opts...), cls: cls}
		if blk != nil {
			vm.callBlockSelf(blk, w, nil)
		}
		return w
	}}

	// #call(env) — the Rack adapter, returning the SPEC [status, headers, body].
	cls.define("call", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		w := self.(*HanamiRouter)
		return hanamiTuple(w.rt.Call(rackEnv(rackArg(args))))
	})

	// Verb helpers: get/post/put/patch/delete/options/trace/link/unlink "path",
	// to:, as:, constraints:.
	for name, fn := range map[string]func(*hanami.Router, string, hanami.To, ...hanami.RouteOption) *hanami.Route{
		"get":     (*hanami.Router).Get,
		"post":    (*hanami.Router).Post,
		"put":     (*hanami.Router).Put,
		"patch":   (*hanami.Router).Patch,
		"delete":  (*hanami.Router).Delete,
		"options": (*hanami.Router).Options,
		"trace":   (*hanami.Router).Trace,
		"link":    (*hanami.Router).Link,
		"unlink":  (*hanami.Router).Unlink,
	} {
		verb := fn
		cls.define(name, func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			w := self.(*HanamiRouter)
			to, ropts := vm.hanamiRouteArgs(args)
			verb(w.rt, hanamiPath(args), to, ropts...)
			return object.NilV
		})
	}

	// root to: … — the GET "/" route named :root.
	cls.define("root", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		w := self.(*HanamiRouter)
		to, ropts := vm.hanamiRouteArgs(args)
		w.rt.Root(to, ropts...)
		return object.NilV
	})

	// redirect "/old", to: "/new", code: 301 — a redirecting GET route.
	cls.define("redirect", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		w := self.(*HanamiRouter)
		opts, _ := lastHash(args)
		target, code := "", 0
		if opts != nil {
			for _, k := range opts.Keys {
				val, _ := opts.Get(k)
				switch hanamiStr(k) {
				case "to":
					target = hanamiStr(val)
				case "code":
					code = hanamiInt(val)
				}
			}
		}
		w.rt.Redirect(hanamiPath(args), target, code)
		return object.NilV
	})

	// mount app, at: "/prefix" — attaches a Rack app (a Hanami::Router, a proc or
	// any #call-responder) at a path prefix.
	cls.define("mount", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		w := self.(*HanamiRouter)
		prefix := ""
		if opts, ok := lastHash(args); ok {
			for _, k := range opts.Keys {
				if hanamiStr(k) == "at" {
					val, _ := opts.Get(k)
					prefix = hanamiStr(val)
				}
			}
		}
		w.rt.Mount(prefix, vm.hanamiRackApp(args[0]))
		return object.NilV
	})

	// scope("prefix") { … } — declares nested routes under a path prefix.
	cls.define("scope", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		w := self.(*HanamiRouter)
		w.rt.Scope(hanamiPath(args), func() {
			if blk != nil {
				vm.callBlockSelf(blk, w, nil)
			}
		})
		return object.NilV
	})

	// path(:name, **params) / url(:name, **params) — the named-route helpers.
	cls.define("path", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		w := self.(*HanamiRouter)
		s, err := w.rt.Path(hanamiHelperName(args), hanamiHelperParams(args))
		if err != nil {
			raise("Hanami::Error", "%s", err.Error())
		}
		return object.NewString(s)
	})
	cls.define("url", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		w := self.(*HanamiRouter)
		s, err := w.rt.URL(hanamiHelperName(args), hanamiHelperParams(args))
		if err != nil {
			raise("Hanami::Error", "%s", err.Error())
		}
		return object.NewString(s)
	})
}

// hanamiRouterOptions reads Hanami::Router.new's keyword options into
// RouterOptions: `resolver:` (a Ruby proc mapping a name to a callable) becomes
// the endpoint Resolver seam, and `scheme:`/`host:` set the URL-helper base.
func hanamiRouterOptions(vm *VM, args []object.Value) []hanami.RouterOption {
	opts, ok := lastHash(args)
	if !ok {
		return nil
	}
	var out []hanami.RouterOption
	scheme, host := "", ""
	for _, k := range opts.Keys {
		val, _ := opts.Get(k)
		switch hanamiStr(k) {
		case "resolver":
			out = append(out, hanami.WithResolver(vm.hanamiResolver(val)))
		case "scheme":
			scheme = hanamiStr(val)
		case "host":
			host = hanamiStr(val)
		}
	}
	if scheme != "" || host != "" {
		if scheme == "" {
			scheme = "http"
		}
		if host == "" {
			host = "localhost"
		}
		out = append(out, hanami.WithBase(scheme, host))
	}
	return out
}

// hanamiResolver adapts a Ruby resolver value into a hanami.Resolver: it sends
// the name to the Ruby callable (a proc or any #call-responder) and maps a
// non-nil result into a RackApp, reporting a nil result as an unknown endpoint.
func (vm *VM) hanamiResolver(callable object.Value) hanami.Resolver {
	return func(name string) (hanami.RackApp, bool) {
		r := vm.send(callable, "call", []object.Value{object.NewString(name)}, nil)
		if object.IsNil(r) {
			return nil, false
		}
		return vm.hanamiRackApp(r), true
	}
}

// hanamiPath returns the route path (the first positional argument), or "/" when
// only keyword options were given.
func hanamiPath(args []object.Value) string {
	if len(args) > 0 {
		if _, ok := args[0].(*object.Hash); !ok {
			return hanamiStr(args[0])
		}
	}
	return "/"
}

// hanamiRouteArgs reads the `to:`, `as:` and `constraints:` keyword options of a
// route declaration into the endpoint To and the RouteOptions.
func (vm *VM) hanamiRouteArgs(args []object.Value) (hanami.To, []hanami.RouteOption) {
	opts, ok := lastHash(args)
	if !ok {
		return hanami.To{}, nil
	}
	var to hanami.To
	var ropts []hanami.RouteOption
	for _, k := range opts.Keys {
		val, _ := opts.Get(k)
		switch hanamiStr(k) {
		case "to":
			to = vm.hanamiTo(val)
		case "as":
			ropts = append(ropts, hanami.As(hanamiStr(val)))
		case "constraints":
			ropts = append(ropts, hanami.Constraints(hanamiConstraints(val)))
		}
	}
	return to, ropts
}

// hanamiTo maps a Ruby `to:` value onto a hanami endpoint: a String/Symbol is a
// resolver name, a Hanami::Action instance dispatches to its built lifecycle
// (path params preserved, no Ruby env round-trip), and any other #call-responder
// (a proc, a Rack app) is a direct app.
func (vm *VM) hanamiTo(v object.Value) hanami.To {
	switch n := v.(type) {
	case *object.String:
		return hanami.ToName(n.Str())
	case object.Symbol:
		return hanami.ToName(string(n))
	}
	if vm.hanamiIsAction(v) {
		return hanami.ToAction(vm.buildHanamiAction(v))
	}
	if vm.respondsTo(v, "call") {
		return hanami.ToApp(vm.hanamiRackApp(v))
	}
	raise("ArgumentError", "unsupported Hanami route endpoint: %s", v.Inspect())
	return hanami.To{}
}

// hanamiIsAction reports whether v is an instance of Hanami::Action (or a
// subclass), so a `to:`/resolver value can dispatch through the action lifecycle.
func (vm *VM) hanamiIsAction(v object.Value) bool {
	if _, ok := v.(*RClass); ok {
		return false
	}
	return classIsA(vm.classOf(v), vm.cHanamiAction)
}

// hanamiRackApp adapts a Ruby callable into a hanami.RackApp: a Hanami::Action
// instance dispatches through its built lifecycle at the Go level (so the
// router's typed path params survive); any other #call-responder is sent the
// Rack env as a Ruby Hash and its returned [status, headers, body] triple is
// mapped back into a hanami.RackResponse.
func (vm *VM) hanamiRackApp(v object.Value) hanami.RackApp {
	if vm.hanamiIsAction(v) {
		return vm.buildHanamiAction(v).Call
	}
	return func(env rack.Env) hanami.RackResponse {
		r := vm.send(v, "call", []object.Value{hanamiEnvHash(env)}, nil)
		return hanamiRackResponseFrom(r)
	}
}

// hanamiConstraints reads a Ruby `constraints:` Hash into the name→regexp map the
// library compiles; a non-Hash value yields no constraints.
func hanamiConstraints(v object.Value) map[string]string {
	h, ok := v.(*object.Hash)
	if !ok {
		return nil
	}
	out := map[string]string{}
	for _, k := range h.Keys {
		val, _ := h.Get(k)
		out[hanamiStr(k)] = hanamiStr(val)
	}
	return out
}

// hanamiHelperName returns the named-route argument of path/url (the first
// positional argument).
func hanamiHelperName(args []object.Value) string {
	if len(args) > 0 {
		if _, ok := args[0].(*object.Hash); !ok {
			return hanamiStr(args[0])
		}
	}
	return ""
}

// hanamiHelperParams reads the keyword params of path/url into the string map the
// helpers substitute, stringifying every value.
func hanamiHelperParams(args []object.Value) map[string]string {
	opts, ok := lastHash(args)
	if !ok {
		return nil
	}
	out := map[string]string{}
	for _, k := range opts.Keys {
		val, _ := opts.Get(k)
		out[hanamiStr(k)] = hanamiStr(val)
	}
	return out
}

// hanamiEnvHash maps a rack.Env into a Ruby Hash (for a Ruby Rack app's #call),
// mapping each Go value back into the object graph via rackFromGo.
func hanamiEnvHash(env rack.Env) *object.Hash {
	h := object.NewHash()
	for k, v := range env {
		h.Set(object.NewString(k), rackFromGo(v))
	}
	return h
}

// hanamiRackResponseFrom maps a Ruby [status, headers, body] triple into a
// hanami.RackResponse. A non-Array (or short Array) yields a 500 so a misbehaving
// Ruby Rack app cannot corrupt the router's contract.
func hanamiRackResponseFrom(v object.Value) hanami.RackResponse {
	arr, ok := v.(*object.Array)
	if !ok || len(arr.Elems) < 3 {
		return hanami.RackResponse{Status: 500, Headers: rack.NewHeaders(), Body: []string{"Internal Server Error"}}
	}
	return hanami.RackResponse{
		Status:  hanamiInt(arr.Elems[0]),
		Headers: rackHeadersFrom(arr.Elems[1]),
		Body:    hanamiBodyParts(arr.Elems[2]),
	}
}

// hanamiBodyParts reads a Rack body value (an Array of parts or a single String)
// into the []string the library carries.
func hanamiBodyParts(v object.Value) []string {
	switch n := v.(type) {
	case *object.Array:
		out := make([]string, len(n.Elems))
		for i, e := range n.Elems {
			out[i] = rackStr(e)
		}
		return out
	case *object.String:
		return []string{n.Str()}
	}
	return []string{rackStr(v)}
}

// hanamiTuple maps a hanami.RackResponse into the SPEC [status, headers, body]
// Ruby Array, the shape roda/sinatra's #call also return.
func hanamiTuple(r hanami.RackResponse) object.Value {
	status, headers, body := r.ToTuple()
	return object.NewArray(object.IntValue(int64(status)), rackHeadersToHash(headers), rackBodyArray(body))
}

// --- Action ----------------------------------------------------------------

// hanamiActionDefFor returns the hanamiActionDef for a Hanami::Action subclass,
// creating it on first use. Definitions are keyed by the class object so each
// subclass keeps its own callbacks (ancestors are walked at build time).
func (vm *VM) hanamiActionDefFor(cls *RClass) *hanamiActionDef {
	if vm.hanamiActionDefs == nil {
		vm.hanamiActionDefs = map[*RClass]*hanamiActionDef{}
	}
	d, ok := vm.hanamiActionDefs[cls]
	if !ok {
		d = &hanamiActionDef{}
		vm.hanamiActionDefs[cls] = d
	}
	return d
}

// hanamiActionChain returns cls and its ancestors up to (and excluding)
// Hanami::Action, outermost-ancestor first, so a subclass inherits its parents'
// callbacks/handlers (parents declared earlier win, matching Ruby order).
func (vm *VM) hanamiActionChain(cls *RClass) []*hanamiActionDef {
	var classes []*RClass
	for c := cls; c != nil && c != vm.cHanamiAction; c = c.super {
		classes = append(classes, c)
	}
	defs := make([]*hanamiActionDef, 0, len(classes))
	for i := len(classes) - 1; i >= 0; i-- {
		if d, ok := vm.hanamiActionDefs[classes[i]]; ok {
			defs = append(defs, d)
		}
	}
	return defs
}

// hanamiActionClassOf resolves the Hanami::Action subclass a DSL/#call receiver
// refers to: the class itself for a class-level DSL call, or the instance's class
// for an instance method.
func (vm *VM) hanamiActionClassOf(self object.Value) *RClass {
	if c, ok := self.(*RClass); ok {
		return c
	}
	return vm.classOf(self)
}

// registerHanamiActionDSL installs the class-level DSL on Hanami::Action
// (inherited by every subclass) plus the instance #call lifecycle adapter.
func (vm *VM) registerHanamiActionDSL(base *RClass) {
	// before / after — record a callback (a block or a symbol method name).
	callback := func(after bool) NativeFn {
		return func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
			d := vm.hanamiActionDefFor(vm.hanamiActionClassOf(self))
			cbs := hanamiCBList(args, blk)
			if after {
				d.afters = append(d.afters, cbs...)
			} else {
				d.befores = append(d.befores, cbs...)
			}
			return object.NilV
		}
	}
	base.smethods["before"] = &Method{name: "before", owner: base, native: callback(false)}
	base.smethods["after"] = &Method{name: "after", owner: base, native: callback(true)}

	// handle_exception { |err, req, resp| … } — an exception handler returning a
	// truthy value when it has handled the error.
	base.smethods["handle_exception"] = &Method{name: "handle_exception", owner: base, native: func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("ArgumentError", "Hanami::Action.handle_exception requires a block")
		}
		d := vm.hanamiActionDefFor(vm.hanamiActionClassOf(self))
		d.handlers = append(d.handlers, blk)
		return object.NilV
	}}

	// accept :json, :html — restrict the action's formats (406 on a mismatch).
	base.smethods["accept"] = &Method{name: "accept", owner: base, native: func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		d := vm.hanamiActionDefFor(vm.hanamiActionClassOf(self))
		for _, a := range args {
			d.accepts = append(d.accepts, hanamiStr(a))
		}
		return object.NilV
	}}

	// default_status(code) / default_format(fmt) — the response initial state.
	base.smethods["default_status"] = &Method{name: "default_status", owner: base, native: func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		vm.hanamiActionDefFor(vm.hanamiActionClassOf(self)).defaultStatus = hanamiInt(rackArg(args))
		return object.NilV
	}}
	base.smethods["default_format"] = &Method{name: "default_format", owner: base, native: func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		vm.hanamiActionDefFor(vm.hanamiActionClassOf(self)).defaultFormat = hanamiStr(rackArg(args))
		return object.NilV
	}}

	// params_validator { |params| … } — the params-validation seam.
	base.smethods["params_validator"] = &Method{name: "params_validator", owner: base, native: func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("ArgumentError", "Hanami::Action.params_validator requires a block")
		}
		vm.hanamiActionDefFor(vm.hanamiActionClassOf(self)).validator = blk
		return object.NilV
	}}

	// session_loader { |env| … } — the session-store seam.
	base.smethods["session_loader"] = &Method{name: "session_loader", owner: base, native: func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("ArgumentError", "Hanami::Action.session_loader requires a block")
		}
		vm.hanamiActionDefFor(vm.hanamiActionClassOf(self)).sessionLoader = blk
		return object.NilV
	}}

	// #call(env) — build the live action for this instance and serve the env.
	base.methods["call"] = &Method{name: "call", owner: base, native: func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return hanamiTuple(vm.buildHanamiAction(self).Call(rackEnv(rackArg(args))))
	}}
}

// hanamiCBList turns a before/after call's arguments (symbol method names) and/or
// trailing block into hanamiCB entries.
func hanamiCBList(args []object.Value, blk *Proc) []hanamiCB {
	var out []hanamiCB
	for _, a := range args {
		out = append(out, hanamiCB{sym: hanamiStr(a)})
	}
	if blk != nil {
		out = append(out, hanamiCB{blk: blk})
	}
	return out
}

// buildHanamiAction assembles a *hanami.Action from instance's class chain: the
// ActionCall body sends #handle to instance, before/after callbacks run their
// block or send the named method, handle_exception handlers run their block, and
// the accept/default/validator/loader options are applied. instance is the self
// #handle and the callbacks run against — the seam every body block plugs into.
func (vm *VM) buildHanamiAction(instance object.Value) *hanami.Action {
	cls := vm.hanamiActionClassOf(instance)
	var opts []hanami.ActionOption
	for _, d := range vm.hanamiActionChain(cls) {
		for _, cb := range d.befores {
			opts = append(opts, hanami.Before(vm.hanamiCallback(instance, cb)))
		}
		for _, cb := range d.afters {
			opts = append(opts, hanami.After(vm.hanamiCallback(instance, cb)))
		}
		for _, h := range d.handlers {
			opts = append(opts, hanami.HandleException(vm.hanamiExceptionHandler(instance, h)))
		}
		if len(d.accepts) > 0 {
			opts = append(opts, hanami.Accept(d.accepts...))
		}
		if d.defaultStatus != 0 {
			opts = append(opts, hanami.WithDefaultStatus(d.defaultStatus))
		}
		if d.defaultFormat != "" {
			opts = append(opts, hanami.WithDefaultFormat(d.defaultFormat))
		}
		if d.validator != nil {
			opts = append(opts, hanami.WithParamsValidator(vm.hanamiValidator(instance, d.validator)))
		}
		if d.sessionLoader != nil {
			opts = append(opts, hanami.WithSessionLoader(vm.hanamiSessionLoader(instance, d.sessionLoader)))
		}
	}
	return hanami.NewAction(cls.name, vm.hanamiActionCall(instance), opts...)
}

// hanamiActionCall is the core action seam: it sends #handle(req, resp) to the
// action instance, wrapping the Hanami Request/Response for Ruby. A Ruby
// exception raised by #handle is caught and returned as a Go error so the
// library runs the registered exception handlers; the halt/redirect_to unwind
// panic (raised inside resp.halt / resp.redirect_to) is re-raised untouched so
// the library's lifecycle recovers it.
func (vm *VM) hanamiActionCall(instance object.Value) hanami.ActionCall {
	return func(_ string, req *hanami.Request, resp *hanami.Response) (err error) {
		defer func() {
			r := recover()
			if r == nil {
				return
			}
			if re, ok := r.(RubyError); ok {
				err = &hanamiRubyErr{e: re}
				return
			}
			panic(r)
		}()
		reqW := &HanamiReq{r: req, cls: vm.cHanamiRequest}
		respW := &HanamiResp{r: resp, cls: vm.cHanamiResponse}
		vm.send(instance, "handle", []object.Value{reqW, respW}, nil)
		return nil
	}
}

// hanamiRubyErr carries a Ruby exception raised by an action body out to the
// library's exception-handling layer and back into a Ruby handler block.
type hanamiRubyErr struct{ e RubyError }

func (e *hanamiRubyErr) Error() string { return e.e.Error() }

// hanamiCallback adapts a before/after hanamiCB into a hanami.Callback: a block
// runs against the action instance, a symbol sends the named method — both
// receiving (req, resp).
func (vm *VM) hanamiCallback(instance object.Value, cb hanamiCB) hanami.Callback {
	return func(req *hanami.Request, resp *hanami.Response) {
		reqW := &HanamiReq{r: req, cls: vm.cHanamiRequest}
		respW := &HanamiResp{r: resp, cls: vm.cHanamiResponse}
		if cb.blk != nil {
			vm.callBlockSelf(cb.blk, instance, []object.Value{reqW, respW})
			return
		}
		vm.send(instance, cb.sym, []object.Value{reqW, respW}, nil)
	}
}

// hanamiExceptionHandler adapts a handle_exception block into a
// hanami.ExceptionHandler: it runs the block against the action instance with
// (exception, req, resp) and reports the block's truthiness as "handled".
func (vm *VM) hanamiExceptionHandler(instance object.Value, blk *Proc) hanami.ExceptionHandler {
	return func(err error, req *hanami.Request, resp *hanami.Response) bool {
		reqW := &HanamiReq{r: req, cls: vm.cHanamiRequest}
		respW := &HanamiResp{r: resp, cls: vm.cHanamiResponse}
		return vm.callBlockSelf(blk, instance, []object.Value{hanamiErrValue(vm, err), reqW, respW}).Truthy()
	}
}

// hanamiErrValue maps a Go error carried out of the action body back into the
// Ruby exception object a handler block receives: a wrapped Ruby exception is
// rebuilt from its original object, anything else becomes a RuntimeError.
func hanamiErrValue(vm *VM, err error) object.Value {
	var re *hanamiRubyErr
	if errors.As(err, &re) {
		return vm.exceptionObject(re.e)
	}
	return vm.exceptionObject(RubyError{Class: "RuntimeError", Message: err.Error()})
}

// hanamiValidator adapts a params_validator block into a hanami.ParamsValidator:
// it runs the block against the action instance with the raw params Hash; a Hash
// result is the validated params, a String result is an error message (making
// the request invalid), and anything else passes the raw params through.
func (vm *VM) hanamiValidator(instance object.Value, blk *Proc) hanami.ParamsValidator {
	return func(raw *rack.Params) (*rack.Params, error) {
		r := vm.callBlockSelf(blk, instance, []object.Value{rackParamsToHash(raw)})
		switch n := r.(type) {
		case *object.Hash:
			return rackParamsFromHash(n), nil
		case *object.String:
			return raw, errors.New(n.Str())
		}
		return raw, nil
	}
}

// hanamiSessionLoader adapts a session_loader block into a hanami.SessionLoader:
// it runs the block against the action instance with the Rack env Hash and maps
// a Hash result into the session map, yielding nil (empty session) otherwise.
func (vm *VM) hanamiSessionLoader(instance object.Value, blk *Proc) hanami.SessionLoader {
	return func(env rack.Env) map[string]any {
		r := vm.callBlockSelf(blk, instance, []object.Value{hanamiEnvHash(env)})
		if h, ok := r.(*object.Hash); ok {
			if m, ok := rackToGo(h).(map[string]any); ok {
				return m
			}
		}
		return nil
	}
}

// --- Request ---------------------------------------------------------------

// registerHanamiRequest installs the Hanami::Action::Request surface (self =
// HanamiReq) a #handle body and its callbacks read.
func (vm *VM) registerHanamiRequest(cls *RClass) {
	self := func(v object.Value) *hanami.Request { return v.(*HanamiReq).r }

	cls.define("params", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return rackParamsToHash(self(v).Params())
	})
	cls.define("param", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).Param(hanamiStr(rackArg(args))))
	})
	// #[] — a params reader convenience (req[:id]).
	cls.define("[]", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).Param(hanamiStr(rackArg(args))))
	})
	cls.define("params_valid?", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).ParamsValid())
	})
	cls.define("params_error", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		if err := self(v).ParamsError(); err != nil {
			return object.NewString(err.Error())
		}
		return object.NilV
	})
	cls.define("format", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).Format())
	})
	cls.define("accepts?", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).Accepts(hanamiStr(rackArg(args))))
	})
	cls.define("session", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return hanamiSessionHash(self(v).Session())
	})
	cls.define("cookies", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return rackParamsToHash(self(v).Cookies())
	})
	cls.define("flash", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return &HanamiFlash{f: self(v).Flash(), cls: vm.cHanamiFlash}
	})
	cls.define("request_method", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).RequestMethod())
	})
	cls.define("path", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).PathInfo())
	})
}

// --- Response --------------------------------------------------------------

// registerHanamiResponse installs the Hanami::Action::Response surface (self =
// HanamiResp) a #handle body and its callbacks write into.
func (vm *VM) registerHanamiResponse(cls *RClass) {
	self := func(v object.Value) *hanami.Response { return v.(*HanamiResp).r }

	cls.define("status", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).Status()))
	})
	cls.define("status=", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).SetStatus(hanamiInt(rackArg(args)))
		return rackArg(args)
	})
	cls.define("body", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).Body())
	})
	cls.define("body=", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).SetBody(rackStr(rackArg(args)))
		return rackArg(args)
	})
	cls.define("write", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		for _, a := range args {
			self(v).Write(rackStr(a))
		}
		return object.NilV
	})
	cls.define("format", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).Format())
	})
	cls.define("format=", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).SetFormat(hanamiStr(rackArg(args)))
		return rackArg(args)
	})
	cls.define("headers", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return rackHeadersToHash(self(v).Headers())
	})
	cls.define("[]", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return rackFromGo(self(v).GetHeader(hanamiStr(rackArg(args))))
	})
	cls.define("[]=", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		self(v).SetHeader(hanamiStr(args[0]), rackStr(args[1]))
		return args[1]
	})
	cls.define("redirect_to", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		status := 0
		if len(args) > 1 {
			status = hanamiInt(args[1])
		}
		// RedirectTo unwinds the lifecycle via a panic recovered by the library, so
		// the return below is never reached at runtime; it shares this basic block
		// with the call so the block is still accounted for.
		self(v).RedirectTo(hanamiStr(args[0]), status)
		return object.NilV
	})
	cls.define("halt", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		body := ""
		if len(args) > 1 {
			body = rackStr(args[1])
		}
		// Halt unwinds the lifecycle via a panic recovered by the library (see above).
		self(v).Halt(hanamiInt(args[0]), body)
		return object.NilV
	})
	cls.define("session", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return hanamiSessionHash(self(v).Session())
	})
	cls.define("flash", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return &HanamiFlash{f: self(v).Flash(), cls: vm.cHanamiFlash}
	})
	cls.define("set_cookie", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		self(v).SetCookie(hanamiStr(args[0]), rack.CookieValue{Value: rackStr(args[1])})
		return object.NilV
	})
	cls.define("delete_cookie", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).DeleteCookie(hanamiStr(rackArg(args)), rack.CookieValue{})
		return object.NilV
	})
}

// --- Flash -----------------------------------------------------------------

// registerHanamiFlash installs the Hanami::Action::Flash surface (self =
// HanamiFlash): #[]/#[]=/keep/empty?.
func (vm *VM) registerHanamiFlash(cls *RClass) {
	self := func(v object.Value) *hanami.Flash { return v.(*HanamiFlash).f }

	cls.define("[]", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		val, ok := self(v).Get(hanamiStr(rackArg(args)))
		if !ok {
			return object.NilV
		}
		return rackFromGo(val)
	})
	cls.define("[]=", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		self(v).Set(hanamiStr(args[0]), rackToGo(args[1]))
		return args[1]
	})
	cls.define("keep", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).Keep(hanamiStr(rackArg(args)))
		return object.NilV
	})
	cls.define("empty?", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).Empty())
	})
}

// hanamiSessionHash maps a session map into a Ruby Hash (keys are Strings), the
// read view the session accessors return.
func hanamiSessionHash(s map[string]any) *object.Hash {
	h := object.NewHash()
	for k, v := range s {
		h.Set(object.NewString(k), rackFromGo(v))
	}
	return h
}

// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	rack "github.com/go-ruby-rack/rack"
	warden "github.com/go-ruby-warden/warden"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerWarden installs the Warden module (require "warden"): the pluggable
// Rack authentication middleware warden gem, reimplemented in pure Go (CGO=0) by
// github.com/go-ruby-warden/warden on top of go-ruby-rack. The library owns the
// deterministic control flow — proxy injection, the throw :warden catch, the
// failure dispatch and the per-scope session (de)serialization — and treats the
// Rack app(s) and the strategy bodies as injectable seams (see warden_bind.go);
// this file maps that surface onto rbgo classes:
//
//	Warden::Manager.new(app, opts={}){ |mgr| … }  — the Rack middleware; #call(env)
//	                                                 injects a Warden::Proxy and
//	                                                 serves the [status, headers,
//	                                                 body] triple
//	Warden::Proxy                                  — env["warden"]: authenticate /
//	                                                 authenticate! / authenticated?
//	                                                 / user / set_user / logout / …
//	Warden::Strategies.add(:label){ … }            — register a strategy (an anon
//	                                                 subclass of Base whose valid?
//	                                                 / authenticate! body is Ruby)
//	Warden::Strategies::Base                       — success!/fail!/fail/redirect!/
//	                                                 custom!/pass + env/request/
//	                                                 params/session helpers
//	Warden::NotAuthenticated (< StandardError)     — the "no failure app" raise
func (vm *VM) registerWarden() {
	if vm.wardenStrategies == nil {
		vm.wardenStrategies = map[string]*RClass{}
	}

	mod := newClass("Warden", nil)
	mod.isModule = true
	vm.consts["Warden"] = mod

	vm.registerWardenErrors(mod)
	vm.registerWardenManager(mod)
	vm.registerWardenProxy(mod)
	vm.registerWardenStrategies(mod)
}

// registerWardenErrors installs the Warden exception tree: Warden::NotAuthenticated
// (raised when a throw :warden reaches the failure stage with no failure app) and
// Warden::UnknownStrategy, both < StandardError.
func (vm *VM) registerWardenErrors(mod *RClass) {
	std := vm.consts["StandardError"].(*RClass)
	for _, name := range []string{"NotAuthenticated", "UnknownStrategy"} {
		cls := newClass("Warden::"+name, std)
		mod.consts[name] = cls
		vm.consts["Warden::"+name] = cls
	}
}

// registerWardenManager installs Warden::Manager and its config surface.
func (vm *VM) registerWardenManager(mod *RClass) {
	cls := newClass("Warden::Manager", vm.cObject)
	mod.consts["Manager"] = cls
	vm.consts["Warden::Manager"] = cls

	cls.smethods["new"] = &Method{name: "new", owner: cls, native: func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		m := &WardenManager{vm: vm, cls: cls, app: args[0], scopeStrategies: map[string][]string{}}
		if len(args) > 1 {
			if h, ok := args[1].(*object.Hash); ok {
				if s, ok := h.Get(object.Symbol("default_scope")); ok {
					m.defaultScope = rackStr(s)
				}
			}
		}
		// The config block is yielded the manager, mirroring Warden::Manager.new's
		// `yield self if block_given?`; every setter it calls runs before #call
		// builds the engine.
		if blk != nil {
			vm.callBlock(blk, []object.Value{m})
		}
		return m
	}}

	self := func(v object.Value) *WardenManager { return v.(*WardenManager) }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	// #call(env) — the Rack entry point.
	d("call", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return self(v).call(rackArg(args))
	})

	// default_strategies(*names) — set (no-arg: read) the default strategy labels.
	d("default_strategies", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		m := self(v)
		if len(args) == 0 {
			out := object.NewArrayFromSlice(make([]object.Value, len(m.defaultStrategies)))
			for i, s := range m.defaultStrategies {
				out.Elems[i] = object.Symbol(s)
			}
			return out
		}
		m.defaultStrategies = m.defaultStrategies[:0]
		for _, a := range args {
			m.defaultStrategies = append(m.defaultStrategies, rackStr(a))
		}
		return object.NilV
	})

	// scope_defaults(scope, strategies: [...]) — set the strategy labels for a scope.
	d("scope_defaults", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		m := self(v)
		scope := rackStr(args[0])
		if len(args) > 1 {
			if h, ok := args[1].(*object.Hash); ok {
				if sv, ok := h.Get(object.Symbol("strategies")); ok {
					if arr, ok := sv.(*object.Array); ok {
						names := make([]string, len(arr.Elems))
						for i, e := range arr.Elems {
							names[i] = rackStr(e)
						}
						m.scopeStrategies[scope] = names
					}
				}
			}
		}
		return object.NilV
	})

	d("failure_app=", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).failureApp = rackArg(args)
		return rackArg(args)
	})
	d("failure_app", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		if app := self(v).failureApp; app != nil {
			return app
		}
		return object.NilV
	})
	d("default_scope=", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).defaultScope = rackStr(rackArg(args))
		return rackArg(args)
	})
	d("default_scope", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		if s := self(v).defaultScope; s != "" {
			return object.Symbol(s)
		}
		return object.Symbol(warden.DefaultScope)
	})
	d("intercept_401=", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).intercept401 = rackArg(args).Truthy()
		return rackArg(args)
	})
}

// call runs the middleware for one request, translating a *warden.NotAuthenticated
// (no failure app) into a Ruby Warden::NotAuthenticated raise.
func (m *WardenManager) call(envArg object.Value) (result object.Value) {
	if m.mgr == nil {
		m.build()
	}
	env := deepRackEnv(envArg)
	defer func() {
		if r := recover(); r != nil {
			if na, ok := r.(*warden.NotAuthenticated); ok {
				raise("Warden::NotAuthenticated", "%s", na.Error())
			}
			panic(r)
		}
	}()
	return wardenTriple(m.mgr.Call(env))
}

// build assembles the underlying *warden.Manager from the collected config,
// wiring the strategy-run and Rack-app seams.
func (m *WardenManager) build() {
	vm := m.vm
	opts := []warden.Option{warden.WithStrategyRun(vm.wardenRun())}
	if len(m.defaultStrategies) > 0 {
		opts = append(opts, warden.WithDefaultStrategies(m.defaultStrategies...))
	}
	if m.defaultScope != "" {
		opts = append(opts, warden.WithDefaultScope(m.defaultScope))
	}
	if m.intercept401 {
		opts = append(opts, warden.WithIntercept401())
	}
	if m.failureApp != nil {
		opts = append(opts, warden.WithFailureApp(vm.wardenApp(m.failureApp)))
	}
	for scope, names := range m.scopeStrategies {
		opts = append(opts, warden.WithScopeStrategies(scope, names...))
	}
	m.mgr = warden.New(vm.wardenApp(m.app), opts...)
}

// registerWardenProxy installs Warden::Proxy — the env["warden"] object.
func (vm *VM) registerWardenProxy(mod *RClass) {
	cls := newClass("Warden::Proxy", vm.cObject)
	mod.consts["Proxy"] = cls
	vm.consts["Warden::Proxy"] = cls

	self := func(v object.Value) *WardenProxy { return v.(*WardenProxy) }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	d("authenticate", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return wardenUserToRuby(self(v).p.Authenticate(wardenAuthOptions(args)))
	})
	d("authenticate!", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return wardenUserToRuby(self(v).p.AuthenticateBang(wardenAuthOptions(args)))
	})
	d("authenticated?", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).p.Authenticated(wardenScopeArg(args)...))
	})
	d("unauthenticated?", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).p.Unauthenticated(wardenScopeArg(args)...))
	})
	d("user", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return wardenUserToRuby(self(v).p.User(wardenScopeArg(args)...))
	})
	d("set_user", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		scope := warden.DefaultScope
		store := true
		if len(args) > 1 {
			if h, ok := args[1].(*object.Hash); ok {
				if s, ok := h.Get(object.Symbol("scope")); ok {
					scope = rackStr(s)
				}
				if sv, ok := h.Get(object.Symbol("store")); ok {
					store = sv.Truthy()
				}
			}
		}
		self(v).p.SetUser(args[0], scope, store)
		return args[0]
	})
	d("logout", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		scopes := make([]string, len(args))
		for i, a := range args {
			scopes[i] = rackStr(a)
		}
		self(v).p.Logout(scopes...)
		return object.NilV
	})
	d("winning_strategy", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		if s := self(v).p.WinningStrategy(); s != "" {
			return object.Symbol(s)
		}
		return object.NilV
	})
	d("message", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).p.Message())
	})
}

// registerWardenStrategies installs Warden::Strategies (the registry) and its
// Warden::Strategies::Base with the terminating + accessor surface a strategy
// body uses.
func (vm *VM) registerWardenStrategies(mod *RClass) {
	reg := newClass("Warden::Strategies", nil)
	reg.isModule = true
	mod.consts["Strategies"] = reg
	vm.consts["Warden::Strategies"] = reg

	base := newClass("Warden::Strategies::Base", vm.cObject)
	reg.consts["Base"] = base
	vm.consts["Warden::Strategies::Base"] = base
	vm.registerWardenStrategyBase(base)

	// Warden::Strategies.add(:label, klass = nil){ body } — register a strategy.
	// With a block, an anonymous subclass of Base is created and the block is
	// class_eval'd into it (defining valid?/authenticate!); a passed class is
	// registered as-is (it must descend from Base).
	reg.smethods["add"] = &Method{name: "add", owner: reg, native: func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		name := rackStr(args[0])
		var cls *RClass
		if len(args) > 1 {
			c, ok := args[1].(*RClass)
			if !ok {
				raise("TypeError", "strategy must be a Class")
			}
			cls = c
		} else {
			cls = newClass("", base)
		}
		if blk != nil {
			vm.classEval(cls, blk, nil)
		}
		vm.wardenStrategies[name] = cls
		return object.NilV
	}}

	// Warden::Strategies[:label] — the registered strategy class (or nil).
	reg.smethods["[]"] = &Method{name: "[]", owner: reg, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if cls, ok := vm.wardenStrategies[rackStr(rackArg(args))]; ok {
			return cls
		}
		return object.NilV
	}}

	// Warden::Strategies.clear! — drop every registration (test isolation).
	reg.smethods["clear!"] = &Method{name: "clear!", owner: reg, native: func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		vm.wardenStrategies = map[string]*RClass{}
		return object.NilV
	}}
}

// registerWardenStrategyBase installs the instance surface a strategy body runs
// against (self = WardenStrategy).
func (vm *VM) registerWardenStrategyBase(base *RClass) {
	self := func(v object.Value) *WardenStrategy { return v.(*WardenStrategy) }
	d := func(name string, fn NativeFn) { base.define(name, fn) }

	// Environment accessors over the Rack env.
	d("env", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.wardenEnvToRuby(self(v).env)
	})
	d("request", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return &RackRequest{req: rack.NewRequest(self(v).env), cls: vm.consts["Rack::Request"].(*RClass)}
	})
	d("params", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		p, err := rack.NewRequest(self(v).env).Params()
		if err != nil {
			raise("ArgumentError", "%s", err.Error())
		}
		return rackParamsToHash(p)
	})
	d("session", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return rackFromGo(self(v).env[rack.RackSession])
	})

	// valid? defaults to true; a strategy overrides it to gate authenticate!.
	d("valid?", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(true)
	})

	// Terminating methods. success!/fail!/redirect!/custom! halt the chain; the
	// non-bang fail and pass do not.
	d("success!", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s := self(v)
		s.result = warden.ResultSuccess
		s.user = rackArg(args)
		if len(args) > 1 {
			s.message = rackStr(args[1])
		}
		s.halted = true
		return object.NilV
	})
	d("fail!", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s := self(v)
		s.result = warden.ResultFailure
		if len(args) > 0 {
			s.message = rackStr(args[0])
		}
		s.halted = true
		return object.NilV
	})
	d("fail", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s := self(v)
		s.result = warden.ResultFailure
		if len(args) > 0 {
			s.message = rackStr(args[0])
		}
		s.halted = false
		return object.NilV
	})
	d("redirect!", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..3)")
		}
		s := self(v)
		url := rackStr(args[0])
		if len(args) > 1 {
			if ph, ok := args[1].(*object.Hash); ok && len(ph.Keys) > 0 {
				url += "?" + rack.BuildQuery(rackParamsFromHash(ph))
			}
		}
		status := 302
		if len(args) > 2 {
			if oh, ok := args[2].(*object.Hash); ok {
				if pv, ok := oh.Get(object.Symbol("permanent")); ok && pv.Truthy() {
					status = 301
				}
				if mv, ok := oh.Get(object.Symbol("message")); ok {
					s.message = rackStr(mv)
				}
			}
		}
		h := rack.NewHeaders()
		h.Set("Location", url)
		s.response = rack.NewResponse([]string{}, status, h)
		s.result = warden.ResultRedirect
		s.halted = true
		return object.NilV
	})
	d("custom!", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s := self(v)
		s.response = wardenResponseFromTriple(rackArg(args))
		s.result = warden.ResultCustom
		s.halted = true
		return object.NilV
	})
	d("pass", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		s := self(v)
		s.result = warden.ResultNone
		s.halted = false
		return object.NilV
	})
	d("halted?", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).halted)
	})
	d("message", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).message)
	})
}

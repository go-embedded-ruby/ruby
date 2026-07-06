// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	rack "github.com/go-ruby-rack/rack"
	warden "github.com/go-ruby-warden/warden"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the Rack/strategy seam between rbgo's Ruby object graph and the
// interpreter-independent github.com/go-ruby-warden/warden engine. The library
// owns the middleware control flow — proxy injection, the throw :warden catch,
// the failure dispatch, the per-scope session (de)serialization — and leaves the
// interpreter-defined pieces behind two seams:
//
//   - the Rack application(s) it wraps (the downstream app and the failure app)
//     answer #call(env); wardenApp adapts a Ruby value answering #call to the
//     library's warden.App, reusing the go-ruby-rack env/response model so no
//     second Rack model is introduced;
//   - the strategy bodies (valid? + authenticate!) are Ruby, so wardenStrategyRun
//     wires the library's warden.StrategyRun seam to rbgo running the registered
//     strategy class's Ruby methods and reads the outcome back into a
//     warden.StrategyResult.
//
// throw :warden is the library's *warden.Throw panic, raised by Proxy#authenticate!
// and recovered by Manager.Call; it travels unharmed through the interpreter
// frames of the Ruby #call exactly as Sinatra's halt/pass panics do. A
// *warden.NotAuthenticated (no failure app configured) is recovered at the
// #call boundary and re-raised as a Ruby Warden::NotAuthenticated.

// WardenManager is the Ruby wrapper around the Warden Rack middleware
// (Warden::Manager). The configuration collected from the new{|m| …} block is
// held here; the underlying *warden.Manager is built lazily on the first #call,
// so every config setter has run before the engine is assembled.
type WardenManager struct {
	vm                *VM
	cls               *RClass
	app               object.Value // downstream Ruby Rack app (answers #call)
	failureApp        object.Value // Ruby Rack app run on an unauthenticated throw
	defaultStrategies []string
	scopeStrategies   map[string][]string
	defaultScope      string
	intercept401      bool
	mgr               *warden.Manager
}

func (m *WardenManager) ToS() string     { return "#<Warden::Manager>" }
func (m *WardenManager) Inspect() string { return m.ToS() }
func (m *WardenManager) Truthy() bool    { return true }

// WardenProxy is the Ruby wrapper around a *warden.Proxy — the env["warden"]
// object the middleware injects. It is scope-aware and delegates authenticate /
// user / set_user / logout straight to the library.
type WardenProxy struct {
	vm  *VM
	p   *warden.Proxy
	cls *RClass
}

func (p *WardenProxy) ToS() string     { return "#<Warden::Proxy>" }
func (p *WardenProxy) Inspect() string { return p.ToS() }
func (p *WardenProxy) Truthy() bool    { return true }

// WardenStrategy is the self a strategy's valid?/authenticate! Ruby body runs
// against — one per strategy run. It carries the Rack env and accumulates the
// outcome the terminating methods (success!/fail!/redirect!/custom!/pass) record,
// which wardenStrategyRun reads back into a warden.StrategyResult. cls is the
// anonymous subclass of Warden::Strategies::Base that Warden::Strategies.add
// class-eval'd the user block into.
type WardenStrategy struct {
	vm       *VM
	cls      *RClass
	env      rack.Env
	result   warden.Result
	user     object.Value
	message  string
	halted   bool
	response *rack.Response
}

func (s *WardenStrategy) ToS() string     { return "#<Warden::Strategy>" }
func (s *WardenStrategy) Inspect() string { return s.ToS() }
func (s *WardenStrategy) Truthy() bool    { return true }

// wardenEnvToRuby renders a rack.Env back into a Ruby Hash for a wrapped Ruby
// Rack app, wrapping the injected *warden.Proxy as a Warden::Proxy and the
// throw payload (warden.ThrowOptions at "warden.options") as a Ruby Hash so the
// app and the failure app reach them as Ruby objects; every other value maps
// through rackFromGo.
func (vm *VM) wardenEnvToRuby(env rack.Env) *object.Hash {
	h := object.NewHash()
	for k, v := range env {
		h.Set(object.NewString(k), vm.wardenEnvValue(v))
	}
	return h
}

// wardenEnvValue maps one rack.Env entry into its Ruby value.
func (vm *VM) wardenEnvValue(v any) object.Value {
	switch x := v.(type) {
	case *warden.Proxy:
		return &WardenProxy{vm: vm, p: x, cls: vm.consts["Warden::Proxy"].(*RClass)}
	case warden.ThrowOptions:
		return wardenThrowOptionsHash(x)
	}
	return rackFromGo(v)
}

// wardenThrowOptionsHash renders the throw(:warden, opts) payload the failure app
// reads at env["warden.options"] as a Ruby Hash keyed by Symbol.
func wardenThrowOptionsHash(o warden.ThrowOptions) *object.Hash {
	h := object.NewHash()
	h.Set(object.Symbol("scope"), object.NewString(o.Scope))
	h.Set(object.Symbol("action"), object.NewString(o.Action))
	h.Set(object.Symbol("message"), object.NewString(o.Message))
	h.Set(object.Symbol("result"), object.NewString(string(o.Result)))
	h.Set(object.Symbol("strategy"), object.NewString(o.Strategy))
	return h
}

// wardenApp adapts a Ruby Rack app (any value answering #call(env)) to the
// library's warden.App: it renders the rack.Env into a Ruby Hash, invokes #call
// and turns the returned [status, headers, body] triple into a *rack.Response.
func (vm *VM) wardenApp(app object.Value) warden.App {
	return func(env rack.Env) *rack.Response {
		h := vm.wardenEnvToRuby(env)
		result := vm.send(app, "call", []object.Value{h}, nil)
		return wardenResponseFromTriple(result)
	}
}

// wardenResponseFromTriple turns a Ruby [status, headers, body] Array into a
// *rack.Response, raising TypeError for a value that is not such a triple.
func wardenResponseFromTriple(v object.Value) *rack.Response {
	arr, ok := v.(*object.Array)
	if !ok || len(arr.Elems) < 3 {
		raise("TypeError", "Rack app must return a [status, headers, body] triple, got %s", v.Inspect())
	}
	status := rackInt(arr.Elems[0], 200)
	headers := rackHeadersFrom(arr.Elems[1])
	body := rackResponseBody([]object.Value{arr.Elems[2]})
	return rack.NewResponse(body, status, headers)
}

// wardenTriple renders a *rack.Response as the Ruby [status, headers, body]
// Array a Rack server consumes.
func wardenTriple(resp *rack.Response) object.Value {
	status, headers, body := resp.Finish()
	return object.NewArray(object.IntValue(int64(status)), rackHeadersToHash(headers), rackBodyArray(body))
}

// wardenRun returns the warden.StrategyRun seam bound to this VM: it looks up the
// registered strategy class by label, runs its valid? predicate (skipping the
// strategy when false), then its authenticate! body against a fresh
// WardenStrategy, and reads the recorded outcome back into a StrategyResult.
func (vm *VM) wardenRun() warden.StrategyRun {
	return func(name string, env rack.Env) warden.StrategyResult {
		cls, ok := vm.wardenStrategies[name]
		if !ok {
			return warden.StrategyResult{}
		}
		s := &WardenStrategy{vm: vm, cls: cls, env: env}
		if !vm.send(s, "valid?", nil, nil).Truthy() {
			return warden.StrategyResult{Valid: false}
		}
		vm.send(s, "authenticate!", nil, nil)
		return warden.StrategyResult{
			Valid:    true,
			Result:   s.result,
			User:     s.user,
			Message:  s.message,
			Halted:   s.halted,
			Response: s.response,
		}
	}
}

// wardenAuthOptions parses the arguments of authenticate / authenticate! into a
// warden.AuthOptions: a trailing Hash contributes scope: (and strategies:), and
// leading Symbols/Strings are the strategy labels for this call.
func wardenAuthOptions(args []object.Value) warden.AuthOptions {
	opts := warden.AuthOptions{}
	for _, a := range args {
		if h, ok := a.(*object.Hash); ok {
			if s, ok := h.Get(object.Symbol("scope")); ok {
				opts.Scope = rackStr(s)
			}
			if sv, ok := h.Get(object.Symbol("strategies")); ok {
				if arr, ok := sv.(*object.Array); ok {
					for _, e := range arr.Elems {
						opts.Strategies = append(opts.Strategies, rackStr(e))
					}
				}
			}
			continue
		}
		opts.Strategies = append(opts.Strategies, rackStr(a))
	}
	return opts
}

// wardenScopeArg reads an optional leading scope argument (a Symbol/String) for
// the scope-taking proxy readers, returning the variadic scope list the library
// accepts. A Hash argument (options) or no argument yields no scope (the default).
func wardenScopeArg(args []object.Value) []string {
	if len(args) == 0 {
		return nil
	}
	if _, ok := args[0].(*object.Hash); ok {
		return nil
	}
	if _, ok := args[0].(object.Nil); ok {
		return nil
	}
	return []string{rackStr(args[0])}
}

// wardenUserToRuby maps a user stored by the library (always the Ruby object.Value
// a strategy handed to success!/set_user) back into the object graph; an absent
// user (unauthenticated, nil) is Ruby nil.
func wardenUserToRuby(u any) object.Value {
	if v, ok := u.(object.Value); ok {
		return v
	}
	return object.NilV
}

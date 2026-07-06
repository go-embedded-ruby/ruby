// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	omniauth "github.com/go-ruby-omniauth/omniauth"
	rack "github.com/go-ruby-rack/rack"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the Rack/strategy seam between rbgo's Ruby object graph and the
// interpreter-independent github.com/go-ruby-omniauth/omniauth engine. The
// library owns the provider-agnostic machinery — the /auth/:provider and
// /auth/:provider/callback routing state-machine, the allowed-methods gate, the
// test-mode mock flow and the failure redirect — and leaves the provider-specific
// bodies behind the omniauth.StrategyPhase seam and the HTTP session behind the
// Rack env. omniPhase wires that seam to rbgo running the mounted provider's Ruby
// strategy (its request_phase for the request phase, its uid/info/credentials/
// extra for the callback AuthHash); the wrapped downstream Rack app answers
// #call(env). The Rack env/response model is reused from go-ruby-rack, so no
// second Rack model is introduced.

// OmniAuthBuilder is the Ruby wrapper around omniauth.Builder — the Rack::Builder
// with a `provider` DSL. Providers mounted by the new{…} block are recorded, and
// the middleware stack is assembled lazily on the first #call.
type OmniAuthBuilder struct {
	vm         *VM
	cls        *RClass
	app        object.Value // downstream Ruby Rack app (nil -> terminal 404)
	config     *omniauth.Config
	builder    *omniauth.Builder
	strategies *omniauth.Strategies
	built      omniauth.App
	dirty      bool
}

func (b *OmniAuthBuilder) ToS() string     { return "#<OmniAuth::Builder>" }
func (b *OmniAuthBuilder) Inspect() string { return b.ToS() }
func (b *OmniAuthBuilder) Truthy() bool    { return true }

// OmniAuthConfig is the Ruby wrapper around the shared omniauth.Config the module
// exposes as OmniAuth.config.
type OmniAuthConfig struct {
	vm  *VM
	cfg *omniauth.Config
	cls *RClass
}

func (c *OmniAuthConfig) ToS() string     { return "#<OmniAuth::Configuration>" }
func (c *OmniAuthConfig) Inspect() string { return c.ToS() }
func (c *OmniAuthConfig) Truthy() bool    { return true }

// OmniAuthMockAuth is the live view OmniAuth.config.mock_auth returns: writing
// mock_auth[:provider] = hash records a mocked callback identity, and a Symbol
// value records a mocked failure key (OmniAuth's test-mode conventions).
type OmniAuthMockAuth struct {
	cfg *omniauth.Config
	cls *RClass
}

func (m *OmniAuthMockAuth) ToS() string     { return "#<OmniAuth::MockAuth>" }
func (m *OmniAuthMockAuth) Inspect() string { return m.ToS() }
func (m *OmniAuthMockAuth) Truthy() bool    { return true }

// OmniAuthStrategy is the self a mounted provider's Ruby strategy body runs
// against for one phase. It carries the Rack env and the provider name and
// records a fail!(key) so omniPhase can take the failure flow.
type OmniAuthStrategy struct {
	vm      *VM
	cls     *RClass
	name    string
	env     rack.Env
	options *omniauth.Options
	failKey string
	failErr error
}

func (s *OmniAuthStrategy) ToS() string     { return "#<OmniAuth::Strategy>" }
func (s *OmniAuthStrategy) Inspect() string { return s.ToS() }
func (s *OmniAuthStrategy) Truthy() bool    { return true }

// OmniAuthHash is the Ruby wrapper around an omniauth.AuthHash — the
// indifferent-access identity hash exposed at env["omniauth.auth"] and returned
// by OmniAuth::AuthHash.new.
type OmniAuthHash struct {
	vm  *VM
	ah  *omniauth.AuthHash
	cls *RClass
}

func (h *OmniAuthHash) ToS() string     { return "#<OmniAuth::AuthHash>" }
func (h *OmniAuthHash) Inspect() string { return h.ToS() }
func (h *OmniAuthHash) Truthy() bool    { return true }

// omniEnvToRuby renders a rack.Env into a Ruby Hash for a wrapped Ruby Rack app,
// wrapping the resolved omniauth.AuthHash (env["omniauth.auth"]) as an
// OmniAuth::AuthHash and the routing Strategy as its provider name; every other
// value maps through rackFromGo.
func (vm *VM) omniEnvToRuby(env rack.Env) *object.Hash {
	h := object.NewHash()
	for k, v := range env {
		h.Set(object.NewString(k), vm.omniEnvValue(v))
	}
	return h
}

// omniEnvValue maps one rack.Env entry into its Ruby value.
func (vm *VM) omniEnvValue(v any) object.Value {
	switch x := v.(type) {
	case *omniauth.AuthHash:
		return &OmniAuthHash{vm: vm, ah: x, cls: vm.consts["OmniAuth::AuthHash"].(*RClass)}
	case *omniauth.Strategy:
		return object.NewString(x.Name())
	}
	return rackFromGo(v)
}

// omniValueToRuby maps a value read out of an omniauth.AuthHash into the object
// graph: a nested AuthHash/InfoHash becomes an OmniAuth::AuthHash wrapper and a
// Go scalar maps via rackFromGo (a value stored in an AuthHash is always a Go
// value or a sub-hash, never a raw Ruby object).
func (vm *VM) omniValueToRuby(v any) object.Value {
	switch x := v.(type) {
	case *omniauth.InfoHash:
		return &OmniAuthHash{vm: vm, ah: x.AuthHash, cls: vm.consts["OmniAuth::AuthHash"].(*RClass)}
	case *omniauth.AuthHash:
		return &OmniAuthHash{vm: vm, ah: x, cls: vm.consts["OmniAuth::AuthHash"].(*RClass)}
	}
	return rackFromGo(v)
}

// omniAuthHashFromRuby builds an omniauth.AuthHash from a Ruby Hash (or an
// existing OmniAuth::AuthHash), recursing into sub-Hashes so info/credentials/
// extra keep their nested indifferent shape.
func omniAuthHashFromRuby(v object.Value) *omniauth.AuthHash {
	switch n := v.(type) {
	case *OmniAuthHash:
		return n.ah
	case *object.Hash:
		ah := omniauth.NewAuthHash()
		for _, k := range n.Keys {
			val, _ := n.Get(k)
			key := rackStr(k)
			if sub, ok := val.(*object.Hash); ok {
				ah.Set(key, omniAuthHashFromRuby(sub))
			} else if oh, ok := val.(*OmniAuthHash); ok {
				ah.Set(key, oh.ah)
			} else {
				ah.Set(key, rackToGo(val))
			}
		}
		return ah
	}
	return omniauth.NewAuthHash()
}

// omniHashToRuby renders an omniauth.AuthHash into a plain Ruby Hash (#to_h),
// recursing into sub-hashes, keyed by String in insertion order.
func (vm *VM) omniHashToRuby(ah *omniauth.AuthHash) *object.Hash {
	h := object.NewHash()
	for _, k := range ah.Keys() {
		val := ah.Get(k)
		switch sub := val.(type) {
		case *omniauth.InfoHash:
			h.Set(object.NewString(k), vm.omniHashToRuby(sub.AuthHash))
		case *omniauth.AuthHash:
			h.Set(object.NewString(k), vm.omniHashToRuby(sub))
		default:
			h.Set(object.NewString(k), rackFromGo(val))
		}
	}
	return h
}

// omniEnv converts a Ruby env Hash into a rack.Env (nested Hashes survive as
// map[string]any so rack.session round-trips), shared with the warden binding.
func (vm *VM) omniEnv(v object.Value) rack.Env { return deepRackEnv(v) }

// omniRubyApp adapts a Ruby Rack app (answering #call(env)) to the library's
// omniauth.App: it renders the env, invokes #call and turns the returned
// [status, headers, body] triple into an omniauth.Response.
type omniRubyApp struct {
	vm  *VM
	app object.Value
}

// Call implements omniauth.App.
func (a *omniRubyApp) Call(env rack.Env) (omniauth.Response, error) {
	h := a.vm.omniEnvToRuby(env)
	result := a.vm.send(a.app, "call", []object.Value{h}, nil)
	return omniResponseFromTriple(result), nil
}

// omniResponseFromTriple turns a Ruby [status, headers, body] Array into an
// omniauth.Response, raising TypeError otherwise.
func omniResponseFromTriple(v object.Value) omniauth.Response {
	arr, ok := v.(*object.Array)
	if !ok || len(arr.Elems) < 3 {
		raise("TypeError", "Rack app must return a [status, headers, body] triple, got %s", v.Inspect())
	}
	return omniauth.Response{
		Status:  rackInt(arr.Elems[0], 200),
		Headers: rackHeadersFrom(arr.Elems[1]),
		Body:    rackResponseBody([]object.Value{arr.Elems[2]}),
	}
}

// omniResponseToTriple renders an omniauth.Response as the Ruby [status, headers,
// body] Array a Rack server consumes.
func omniResponseToTriple(resp omniauth.Response) object.Value {
	return object.NewArray(object.IntValue(int64(resp.Status)), rackHeadersToHash(resp.Headers), rackBodyArray(resp.Body))
}

// omniPhase returns the omniauth.StrategyPhase seam bound to this VM. For the
// request phase it runs the provider's Ruby request_phase (a redirect triple, or
// a fail!); for the callback phase it assembles the AuthHash from the provider's
// uid/info/credentials/extra Ruby methods (or a fail!).
func (vm *VM) omniPhase() omniauth.StrategyPhase {
	return func(name, phase string, env any) omniauth.PhaseResult {
		renv, _ := env.(rack.Env)
		cls, ok := vm.omniAuthStrategies[name]
		if !ok {
			return omniauth.PhaseResult{Fail: "invalid_strategy", Err: omniauth.NewError("invalid_strategy", "no strategy registered for "+name)}
		}
		s := &OmniAuthStrategy{vm: vm, cls: cls, name: name, env: renv, options: &omniauth.Options{Args: vm.omniAuthProviderOpts[name]}}
		if phase == omniauth.PhaseRequest {
			res := vm.send(s, "request_phase", nil, nil)
			if s.failKey != "" {
				return omniauth.PhaseResult{Fail: s.failKey, Err: s.failErr}
			}
			if arr, ok := res.(*object.Array); ok && len(arr.Elems) >= 3 {
				resp := omniResponseFromTriple(arr)
				return omniauth.PhaseResult{Response: &resp}
			}
			return omniauth.PhaseResult{Fail: "invalid_request", Err: omniauth.NewError("invalid_request", "request phase produced no response")}
		}
		auth := vm.omniBuildAuth(s)
		if s.failKey != "" {
			return omniauth.PhaseResult{Fail: s.failKey, Err: s.failErr}
		}
		return omniauth.PhaseResult{Auth: auth}
	}
}

// omniBuildAuth assembles the callback AuthHash from the provider strategy's Ruby
// accessors: uid (a scalar) and the info/credentials/extra sub-hashes. A fail!
// during any accessor is honoured by omniPhase after this returns.
func (vm *VM) omniBuildAuth(s *OmniAuthStrategy) *omniauth.AuthHash {
	ah := omniauth.NewAuthHash()
	if uid := vm.send(s, "uid", nil, nil); !object.IsNil(uid) {
		ah.Set("uid", rackStr(uid))
	}
	for _, key := range []string{"info", "credentials", "extra"} {
		if sub, ok := vm.send(s, key, nil, nil).(*object.Hash); ok && len(sub.Keys) > 0 {
			ah.Set(key, omniAuthHashFromRuby(sub))
		}
	}
	return ah
}

// omniRaise translates a library error into the matching Ruby OmniAuth exception.
func omniRaise(err error) {
	switch e := err.(type) {
	case *omniauth.NoSessionError:
		raise("OmniAuth::NoSessionError", "%s", e.Error())
	case *omniauth.AuthenticityError:
		raise("OmniAuth::AuthenticityError", "%s", e.Error())
	default:
		raise("OmniAuth::Error", "%s", err.Error())
	}
}

// call runs the assembled middleware stack for one request.
func (b *OmniAuthBuilder) call(envArg object.Value) object.Value {
	if b.built == nil || b.dirty {
		b.assemble()
	}
	resp, err := b.built.Call(b.vm.omniEnv(envArg))
	if err != nil {
		omniRaise(err)
	}
	return omniResponseToTriple(resp)
}

// assemble wraps the downstream Ruby app (or a terminal 404) with the mounted
// providers, raising OmniAuth::Error if a mounted provider was never registered.
func (b *OmniAuthBuilder) assemble() {
	terminal := omniauth.App(omniauth.PassThroughApp())
	if b.app != nil {
		terminal = &omniRubyApp{vm: b.vm, app: b.app}
	}
	built, err := b.builder.Build(terminal)
	if err != nil {
		raise("OmniAuth::Error", "%s", err.Error())
	}
	b.built = built
	b.dirty = false
}

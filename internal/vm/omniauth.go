// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	omniauth "github.com/go-ruby-omniauth/omniauth"
	rack "github.com/go-ruby-rack/rack"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerOmniAuth installs the OmniAuth module (require "omniauth"): the
// multi-provider Rack authentication framework omniauth gem, reimplemented in
// pure Go (CGO=0) by github.com/go-ruby-omniauth/omniauth on top of go-ruby-rack.
// The library owns the request/callback routing, the test-mode mock flow and the
// failure redirect, and treats the provider strategy bodies as an injectable
// seam (see omniauth_bind.go); this file maps that surface onto rbgo classes:
//
//	OmniAuth::Builder.new(app){ provider :name, … }  — the middleware stack; #call(env)
//	                                                   routes /auth/:provider and
//	                                                   /auth/:provider/callback
//	OmniAuth::Strategies.add(:name){ … }             — register a provider strategy
//	                                                   (request_phase + uid/info/…)
//	OmniAuth::Strategy                               — the strategy base: redirect /
//	                                                   fail! / uid / info / … helpers
//	OmniAuth::AuthHash                               — the identity hash: provider /
//	                                                   uid / info / credentials / extra
//	OmniAuth.config                                  — test_mode / mock_auth / path_prefix
//	OmniAuth::Error tree                             — Error / AuthenticityError /
//	                                                   NoSessionError (< StandardError)
func (vm *VM) registerOmniAuth() {
	if vm.omniAuthStrategies == nil {
		vm.omniAuthStrategies = map[string]*RClass{}
		vm.omniAuthProviderOpts = map[string]map[string]any{}
	}

	mod := newClass("OmniAuth", nil)
	mod.isModule = true
	vm.consts["OmniAuth"] = mod

	vm.registerOmniAuthErrors(mod)
	vm.registerOmniAuthConfig(mod)
	vm.registerOmniAuthHash(mod)
	vm.registerOmniAuthStrategy(mod)
	vm.registerOmniAuthBuilder(mod)
}

// registerOmniAuthErrors installs the OmniAuth exception tree, all < StandardError.
func (vm *VM) registerOmniAuthErrors(mod *RClass) {
	std := vm.consts["StandardError"].(*RClass)
	base := newClass("OmniAuth::Error", std)
	mod.consts["Error"] = base
	vm.consts["OmniAuth::Error"] = base
	for _, name := range []string{"AuthenticityError", "NoSessionError"} {
		cls := newClass("OmniAuth::"+name, base)
		mod.consts[name] = cls
		vm.consts["OmniAuth::"+name] = cls
	}
}

// registerOmniAuthConfig installs OmniAuth.config and the OmniAuth::Configuration
// surface (test_mode / mock_auth / path_prefix).
func (vm *VM) registerOmniAuthConfig(mod *RClass) {
	cfgCls := newClass("OmniAuth::Configuration", vm.cObject)
	mod.consts["Configuration"] = cfgCls
	vm.consts["OmniAuth::Configuration"] = cfgCls

	vm.omniAuthConfig = &OmniAuthConfig{vm: vm, cfg: omniauth.DefaultConfig(), cls: cfgCls}

	mod.smethods["config"] = &Method{name: "config", owner: mod, native: func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.omniAuthConfig
	}}

	self := func(v object.Value) *OmniAuthConfig { return v.(*OmniAuthConfig) }
	d := func(name string, fn NativeFn) { cfgCls.define(name, fn) }

	d("test_mode=", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).cfg.TestMode = rackArg(args).Truthy()
		return rackArg(args)
	})
	d("test_mode", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).cfg.TestMode)
	})
	d("test_mode?", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).cfg.TestMode)
	})
	d("path_prefix=", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).cfg.PathPrefix = rackStr(rackArg(args))
		return rackArg(args)
	})
	d("path_prefix", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).cfg.PathPrefix)
	})
	d("mock_auth", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return &OmniAuthMockAuth{cfg: self(v).cfg, cls: vm.consts["OmniAuth::MockAuth"].(*RClass)}
	})
	// add_mock(provider, hash) — the block form of mock_auth[provider] = hash.
	d("add_mock", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		omniStoreMock(self(v).cfg, rackStr(args[0]), args[1])
		return object.NilV
	})

	// The live mock_auth view: mock_auth[:provider] reads/writes the mocked identity.
	mockCls := newClass("OmniAuth::MockAuth", vm.cObject)
	mod.consts["MockAuth"] = mockCls
	vm.consts["OmniAuth::MockAuth"] = mockCls
	mockCls.define("[]=", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		omniStoreMock(v.(*OmniAuthMockAuth).cfg, rackStr(args[0]), args[1])
		return args[1]
	})
	mockCls.define("[]", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		cfg := v.(*OmniAuthMockAuth).cfg
		key := rackStr(rackArg(args))
		if fk, ok := cfg.MockFailure[key]; ok {
			return object.Symbol(fk)
		}
		if ah, ok := cfg.MockAuth[key]; ok {
			return &OmniAuthHash{vm: vm, ah: ah, cls: vm.consts["OmniAuth::AuthHash"].(*RClass)}
		}
		return object.NilV
	})
}

// omniStoreMock records a mocked callback for provider: a Symbol value is a
// mocked failure key, any Hash/AuthHash a mocked identity.
func omniStoreMock(cfg *omniauth.Config, provider string, val object.Value) {
	if sym, ok := val.(object.Symbol); ok {
		cfg.MockFailure[provider] = string(sym)
		return
	}
	cfg.MockAuth[provider] = omniAuthHashFromRuby(val)
}

// registerOmniAuthHash installs OmniAuth::AuthHash and its indifferent-access
// accessor surface over an omniauth.AuthHash.
func (vm *VM) registerOmniAuthHash(mod *RClass) {
	cls := newClass("OmniAuth::AuthHash", vm.cObject)
	mod.consts["AuthHash"] = cls
	vm.consts["OmniAuth::AuthHash"] = cls

	cls.smethods["new"] = &Method{name: "new", owner: cls, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		ah := omniauth.NewAuthHash()
		if len(args) > 0 {
			ah = omniAuthHashFromRuby(args[0])
		}
		return &OmniAuthHash{vm: vm, ah: ah, cls: cls}
	}}

	self := func(v object.Value) *OmniAuthHash { return v.(*OmniAuthHash) }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	str := func(fn func(*omniauth.AuthHash) string) NativeFn {
		return func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
			if s := fn(self(v).ah); s != "" {
				return object.NewString(s)
			}
			return object.NilV
		}
	}
	d("provider", str((*omniauth.AuthHash).Provider))
	d("uid", str((*omniauth.AuthHash).UID))

	sub := func(fn func(*omniauth.AuthHash) *omniauth.AuthHash) NativeFn {
		return func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
			return &OmniAuthHash{vm: vm, ah: fn(self(v).ah), cls: cls}
		}
	}
	d("credentials", sub((*omniauth.AuthHash).Credentials))
	d("extra", sub((*omniauth.AuthHash).Extra))
	d("info", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return &OmniAuthHash{vm: vm, ah: self(v).ah.Info().AuthHash, cls: cls}
	})

	d("[]", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		val, ok := self(v).ah.GetOK(rackStr(rackArg(args)))
		if !ok {
			return object.NilV
		}
		return vm.omniValueToRuby(val)
	})
	d("[]=", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		key := rackStr(args[0])
		if sub, ok := args[1].(*object.Hash); ok {
			self(v).ah.Set(key, omniAuthHashFromRuby(sub))
		} else {
			self(v).ah.Set(key, rackToGo(args[1]))
		}
		return args[1]
	})
	d("key?", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).ah.Has(rackStr(rackArg(args))))
	})
	d("valid?", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).ah.ValidQ())
	})
	d("to_h", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.omniHashToRuby(self(v).ah)
	})
	d("to_hash", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.omniHashToRuby(self(v).ah)
	})
}

// registerOmniAuthStrategy installs OmniAuth::Strategy — the base a mounted
// provider's Ruby strategy subclasses. It provides the request-phase redirect
// and fail! terminators, default identity accessors (overridden per provider) and
// the env/request/options/callback_url helpers.
func (vm *VM) registerOmniAuthStrategy(mod *RClass) {
	cls := newClass("OmniAuth::Strategy", vm.cObject)
	mod.consts["Strategy"] = cls
	vm.consts["OmniAuth::Strategy"] = cls

	self := func(v object.Value) *OmniAuthStrategy { return v.(*OmniAuthStrategy) }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	d("name", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).name)
	})
	d("env", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.omniEnvToRuby(self(v).env)
	})
	d("request", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return &RackRequest{req: rack.NewRequest(self(v).env), cls: vm.consts["Rack::Request"].(*RClass)}
	})
	d("session", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return rackFromGo(self(v).env[rack.RackSession])
	})
	d("options", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		h := object.NewHash()
		for k, val := range self(v).options.Args {
			h.Set(object.NewString(k), rackFromGo(val))
		}
		return h
	})
	// callback_url — the absolute callback path the provider redirects back to.
	d("callback_url", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		s := self(v)
		return object.NewString(vm.omniAuthConfig.cfg.PathPrefix + "/" + s.name + "/callback")
	})

	// redirect(url) — the request-phase provider redirect; returns the Rack triple.
	d("redirect", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		h := object.NewHash()
		h.Set(object.NewString("Location"), object.NewString(rackStr(args[0])))
		h.Set(object.NewString("Content-Type"), object.NewString("text/html"))
		return object.NewArray(object.IntValue(302), h, object.NewArray(object.NewString("302 Moved")))
	})
	// fail!(key, exception = nil) — take the failure flow with a message key.
	d("fail!", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		s := self(v)
		s.failKey = rackStr(rackArg(args))
		if s.failKey == "" {
			s.failKey = "invalid_credentials"
		}
		return object.NilV
	})

	// Default identity accessors; a provider overrides those it can supply.
	d("uid", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value { return object.NilV })
	for _, name := range []string{"info", "credentials", "extra"} {
		d(name, func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value { return object.NewHash() })
	}
}

// registerOmniAuthBuilder installs OmniAuth::Builder — the provider-mounting Rack
// middleware stack.
func (vm *VM) registerOmniAuthBuilder(mod *RClass) {
	cls := newClass("OmniAuth::Builder", vm.cObject)
	mod.consts["Builder"] = cls
	vm.consts["OmniAuth::Builder"] = cls

	cls.smethods["new"] = &Method{name: "new", owner: cls, native: func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		b := &OmniAuthBuilder{vm: vm, cls: cls, config: vm.omniAuthConfig.cfg, strategies: omniauth.NewStrategies()}
		if len(args) > 0 && !object.IsNil(args[0]) {
			b.app = args[0]
		}
		b.builder = omniauth.NewBuilder(b.config, b.strategies)
		// The block is instance_eval'd against the builder (Rack::Builder style) so a
		// bare `provider :name` inside it mounts onto this builder.
		if blk != nil {
			vm.callBlockSelf(blk, b, nil)
		}
		return b
	}}

	self := func(v object.Value) *OmniAuthBuilder { return v.(*OmniAuthBuilder) }
	d := func(name string, fn NativeFn) { cls.define(name, fn) }

	// provider(name_or_class, *args) — mount a provider. A Class is registered as
	// the strategy for its derived name; a Symbol/String names a strategy
	// registered via OmniAuth::Strategies.add (or a following Class argument).
	d("provider", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1+)")
		}
		b := self(v)
		var name string
		if c, ok := args[0].(*RClass); ok {
			name = omniProviderName(c)
			vm.omniAuthStrategies[name] = c
		} else {
			name = rackStr(args[0])
			if len(args) > 1 {
				if c, ok := args[1].(*RClass); ok {
					vm.omniAuthStrategies[name] = c
				}
			}
		}
		opts := &omniauth.Options{Args: map[string]any{}}
		if len(args) > 1 {
			if h, ok := args[len(args)-1].(*object.Hash); ok {
				for _, k := range h.Keys {
					val, _ := h.Get(k)
					opts.Args[rackStr(k)] = rackToGo(val)
				}
			}
		}
		vm.omniAuthProviderOpts[name] = opts.Args
		b.builder.Provider(name, opts)
		if _, ok := b.strategies.Lookup(name); !ok {
			b.strategies.Register(name, vm.omniPhase())
		}
		b.dirty = true
		return object.NilV
	})

	d("call", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return self(v).call(rackArg(args))
	})
	d("to_app", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return v
	})

	// OmniAuth::Strategies.add(:name){ … } — register a provider strategy (an
	// anonymous subclass of OmniAuth::Strategy the block defines request_phase /
	// uid / info / … on), mirroring the warden strategy-registry convention.
	reg := newClass("OmniAuth::Strategies", nil)
	reg.isModule = true
	mod.consts["Strategies"] = reg
	vm.consts["OmniAuth::Strategies"] = reg
	base := vm.consts["OmniAuth::Strategy"].(*RClass)
	reg.smethods["add"] = &Method{name: "add", owner: reg, native: func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		name := rackStr(args[0])
		var scls *RClass
		if len(args) > 1 {
			c, ok := args[1].(*RClass)
			if !ok {
				raise("TypeError", "strategy must be a Class")
			}
			scls = c
		} else {
			scls = newClass("", base)
		}
		if blk != nil {
			vm.classEval(scls, blk, nil)
		}
		vm.omniAuthStrategies[name] = scls
		return object.NilV
	}}
	reg.smethods["[]"] = &Method{name: "[]", owner: reg, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if cls, ok := vm.omniAuthStrategies[rackStr(rackArg(args))]; ok {
			return cls
		}
		return object.NilV
	}}
}

// omniProviderName derives a provider label from a strategy Class: the last
// "::"-segment of its name, snake_cased (Developer -> "developer",
// GoogleOauth2 -> "google_oauth2"). An anonymous class falls back to "strategy".
func omniProviderName(c *RClass) string {
	name := c.name
	if name == "" {
		return "strategy"
	}
	if i := lastIndexOfSep(name); i >= 0 {
		name = name[i+2:]
	}
	return snakeCase(name)
}

// lastIndexOfSep returns the byte index of the last "::" in s, or -1.
func lastIndexOfSep(s string) int {
	for i := len(s) - 2; i >= 0; i-- {
		if s[i] == ':' && s[i+1] == ':' {
			return i
		}
	}
	return -1
}

// snakeCase lowercases s inserting an underscore before each interior uppercase
// run boundary (GoogleOauth2 -> google_oauth2).
func snakeCase(s string) string {
	var b []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			if i > 0 && !(s[i-1] >= 'A' && s[i-1] <= 'Z') {
				b = append(b, '_')
			}
			c += 'a' - 'A'
		}
		b = append(b, c)
	}
	return string(b)
}

// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	httparty "github.com/go-ruby-httparty/httparty"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// httpartyVerbs are the HTTP methods HTTParty exposes both as module methods
// (HTTParty.get/…) and, on a class that does `include HTTParty`, as class and
// instance methods. The name maps directly to the library Client verb.
var httpartyVerbs = []string{"get", "post", "put", "patch", "delete", "head", "options"}

// registerHTTParty installs the HTTParty module (require "httparty"): the module
// verb methods HTTParty.get/post/put/patch/delete/head/options(url, options={}),
// the `include HTTParty` class DSL (base_uri/headers/default_params/basic_auth/
// format/default_options plus the verb methods as class and instance methods),
// the content-type-aware HTTParty::Response (#code/#body/#headers/#parsed_response/
// #success?/#[]) and the HTTParty error tree (HTTParty::Error < StandardError,
// with UnsupportedFormat/UnsupportedURIScheme/ResponseError beneath it and
// RedirectionTooDeep/DuplicateLocationHeader beneath ResponseError). The whole
// HTTP-client behaviour (URL building, body/query encoding, Basic auth, redirect
// following, content-type parsing) lives in the github.com/go-ruby-httparty
// library; this file is the class + method wiring and httparty_bind.go holds the
// value conversions and the transport seam.
func (vm *VM) registerHTTParty() {
	mod := newClass("HTTParty", nil)
	mod.isModule = true
	vm.consts["HTTParty"] = mod

	vm.registerHTTPartyErrors(mod)

	cResp := newClass("HTTParty::Response", vm.cObject)
	vm.consts["HTTParty::Response"] = cResp
	mod.consts["Response"] = cResp
	vm.registerHTTPartyResponse(cResp)

	// Module verb methods: HTTParty.get(url, options={}), … — issued through a
	// fresh zero-config client whose transport comes from the httpartyAdapter seam.
	for _, verb := range httpartyVerbs {
		v := verb
		mod.smethods[v] = &Method{name: v, owner: mod, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			client := httparty.NewClient(func(c *httparty.Client) { c.Adapter(httpartyAdapter()) })
			return vm.httpartyRun(client, v, args)
		}}
	}

	// included hook: when a class does `include HTTParty`, install the class DSL
	// and the verb methods on it (mirroring HTTParty's ClassMethods).
	mod.smethods["included"] = &Method{name: "included", owner: mod, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) > 0 {
			if base, ok := args[0].(*RClass); ok {
				vm.httpartyInclude(base)
			}
		}
		return object.NilV
	}}
}

// registerHTTPartyErrors installs the HTTParty error tree, mirroring the gem:
// HTTParty::Error < StandardError, the UnsupportedFormat/UnsupportedURIScheme/
// ResponseError subclasses beneath it, and RedirectionTooDeep/
// DuplicateLocationHeader beneath ResponseError. Every class name equals the
// library's ErrorKind string, so a raised *httparty.Error maps to its Ruby class
// by name. HTTParty::Error#response exposes the response a ResponseError carries.
func (vm *VM) registerHTTPartyErrors(mod *RClass) {
	defs := []struct{ qualified, parent string }{
		{"HTTParty::Error", "StandardError"},
		{"HTTParty::UnsupportedFormat", "HTTParty::Error"},
		{"HTTParty::UnsupportedURIScheme", "HTTParty::Error"},
		{"HTTParty::ResponseError", "HTTParty::Error"},
		{"HTTParty::RedirectionTooDeep", "HTTParty::ResponseError"},
		{"HTTParty::DuplicateLocationHeader", "HTTParty::ResponseError"},
	}
	for _, d := range defs {
		parent := vm.consts[d.parent].(*RClass)
		cls := newClass(d.qualified, parent)
		vm.consts[d.qualified] = cls
		mod.consts[d.qualified[len("HTTParty::"):]] = cls
	}
	base := vm.consts["HTTParty::Error"].(*RClass)
	base.define("response", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@response")
	})
}

// httpartyInclude installs the `include HTTParty` surface on base: the class-level
// DSL (base_uri/headers/default_params/basic_auth/format/default_options) as class
// methods, and the HTTP verbs as both class methods and instance methods. The DSL
// accumulates its configuration in the including class's own instance variables
// (read back by httpartyClientFromClass when a request is issued), matching how
// HTTParty's class methods build up default_options.
func (vm *VM) httpartyInclude(base *RClass) {
	sm := func(name string, fn NativeFn) {
		base.smethods[name] = &Method{name: name, owner: base, native: fn}
	}

	sm("base_uri", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) > 0 {
			setIvar(self, "@httparty_base_uri", object.NewString(args[0].ToS()))
		}
		return getIvar(self, "@httparty_base_uri")
	})
	sm("headers", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return httpartyMergeHashIvar(self, "@httparty_headers", args)
	})
	sm("default_params", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return httpartyMergeHashIvar(self, "@httparty_default_params", args)
	})
	sm("basic_auth", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		h := object.NewHash()
		h.Set(object.Symbol("username"), object.NewString(args[0].ToS()))
		h.Set(object.Symbol("password"), object.NewString(args[1].ToS()))
		setIvar(self, "@httparty_basic_auth", h)
		return h
	})
	sm("format", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) > 0 {
			setIvar(self, "@httparty_format", object.Symbol(httpartyName(args[0])))
		}
		return getIvar(self, "@httparty_format")
	})
	sm("default_options", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return httpartyDefaultOptions(self)
	})

	for _, verb := range httpartyVerbs {
		v := verb
		sm(v, func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			return vm.httpartyRun(vm.httpartyClientFromClass(self.(*RClass)), v, args)
		})
		base.define(v, func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			return vm.httpartyRun(vm.httpartyClientFromClass(vm.classOf(self)), v, args)
		})
	}
}

// httpartyMergeHashIvar merges an optional Hash argument into a class-level Hash
// instance variable (creating it on first use) and returns the accumulated Hash,
// modelling HTTParty's headers / default_params class methods.
func httpartyMergeHashIvar(self object.Value, name string, args []object.Value) object.Value {
	cur, ok := getIvar(self, name).(*object.Hash)
	if !ok {
		cur = object.NewHash()
		setIvar(self, name, cur)
	}
	if len(args) > 0 {
		if h, ok := args[0].(*object.Hash); ok {
			for _, k := range h.Keys {
				v, _ := h.Get(k)
				cur.Set(k, v)
			}
		}
	}
	return cur
}

// httpartyDefaultOptions renders the accumulated class DSL configuration as the
// options Hash HTTParty exposes through default_options, including only the keys
// that were set.
func httpartyDefaultOptions(self object.Value) object.Value {
	h := object.NewHash()
	for _, e := range []struct {
		key string
		iv  string
	}{
		{"base_uri", "@httparty_base_uri"},
		{"headers", "@httparty_headers"},
		{"default_params", "@httparty_default_params"},
		{"basic_auth", "@httparty_basic_auth"},
		{"format", "@httparty_format"},
	} {
		if v := getIvar(self, e.iv); !object.IsNil(v) {
			h.Set(object.Symbol(e.key), v)
		}
	}
	return h
}

// registerHTTPartyResponse installs the read-only HTTParty::Response surface:
// #code, #body, #headers, #success?, the content-type-aware #parsed_response and
// its #[] shortcut (parsed_response[key]).
func (vm *VM) registerHTTPartyResponse(c *RClass) {
	respOf := func(self object.Value) *httparty.Response { return self.(*HTTPartyResponse).r }

	c.define("code", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(respOf(self).Code()))
	})
	c.define("body", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(respOf(self).Body())
	})
	c.define("headers", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return httpartyHeadersToRubyHash(respOf(self).Headers())
	})
	c.define("success?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(respOf(self).Success())
	})
	c.define("parsed_response", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.httpartyParsed(respOf(self))
	})
	c.define("[]", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.httpartyIndex(respOf(self), args[0])
	})
}

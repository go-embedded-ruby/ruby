// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	faraday "github.com/go-ruby-faraday/faraday"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// faradayAdapter returns the terminal transport rbgo wires into every Faraday
// connection — the library's net/http Doer in production. Tests override it with
// a Doer that talks to an in-process httptest server (or a canned stub), so the
// suite touches no external network. The library performs the whole client
// abstraction around this seam; only the round-trip is host-provided.
var faradayAdapter = func() faraday.Doer { return faraday.NetHTTP() }

// registerFaraday installs the Faraday module (require "faraday"): the connection
// factory Faraday.new(url:, headers:, params:) { |conn| … } and the module-level
// one-shot verb helpers (Faraday.get/post/…), the Faraday::Connection with its
// middleware DSL (request/response/adapter/basic_auth/authorization) and verb
// methods, the mutable per-request Faraday::Request, the read-only
// Faraday::Response (#status/#body/#headers/#success?), and the gem's error tree
// (Faraday::Error < StandardError, with ClientError/ServerError/ConnectionFailed/
// TimeoutError/… beneath it). The middleware-based HTTP-client core lives in the
// github.com/go-ruby-faraday/faraday library; this file is the class + method
// wiring (see faraday_bind.go for the wrappers and value conversions).
func (vm *VM) registerFaraday() {
	mod := newClass("Faraday", nil)
	mod.isModule = true
	vm.consts["Faraday"] = mod

	vm.registerFaradayErrors(mod)

	cConn := vm.faradayClass(mod, "Connection", "Faraday::Connection")
	cReq := vm.faradayClass(mod, "Request", "Faraday::Request")
	cResp := vm.faradayClass(mod, "Response", "Faraday::Response")

	utils := newClass("Faraday::Utils", nil)
	utils.isModule = true
	mod.consts["Utils"] = utils
	vm.consts["Faraday::Utils"] = utils
	cHeaders := newClass("Faraday::Utils::Headers", vm.cObject)
	utils.consts["Headers"] = cHeaders
	vm.consts["Faraday::Utils::Headers"] = cHeaders
	cParams := newClass("Faraday::Utils::ParamsHash", vm.cObject)
	utils.consts["ParamsHash"] = cParams
	vm.consts["Faraday::Utils::ParamsHash"] = cParams

	sm := func(name string, fn NativeFn) {
		mod.smethods[name] = &Method{name: name, owner: mod, native: fn}
	}
	sm("new", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		return vm.faradayNew(args, blk)
	})
	for _, verb := range []string{"get", "head", "delete", "trace", "options"} {
		v := verb
		sm(v, func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
			return vm.faradayModuleVerb(v, false, args, blk)
		})
	}
	for _, verb := range []string{"post", "put", "patch"} {
		v := verb
		sm(v, func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
			return vm.faradayModuleVerb(v, true, args, blk)
		})
	}

	vm.registerFaradayConnection(cConn)
	vm.registerFaradayRequest(cReq)
	vm.registerFaradayResponse(cResp)
	vm.registerFaradayProxies(cHeaders, cParams)
}

// faradayClass creates a Faraday::* class under cObject, records it flat (for
// classOf) and nests it under the Faraday module by its simple name.
func (vm *VM) faradayClass(mod *RClass, simple, qualified string) *RClass {
	c := newClass(qualified, vm.cObject)
	vm.consts[qualified] = c
	mod.consts[simple] = c
	return c
}

// registerFaradayErrors installs the Faraday error tree, mirroring the gem: the
// root Faraday::Error < StandardError, the ClientError/ServerError/transport
// subclasses beneath it, and the status-specific 4xx errors beneath ClientError.
// Every class name equals the library's ErrorKind string, so a raised
// *faraday.Error maps to its Ruby class by name. Faraday::Error#response exposes
// the {status:, headers:, body:} context carried by a raised response error.
func (vm *VM) registerFaradayErrors(mod *RClass) {
	defs := []struct{ qualified, parent string }{
		{"Faraday::Error", "StandardError"},
		{"Faraday::ClientError", "Faraday::Error"},
		{"Faraday::ServerError", "Faraday::Error"},
		{"Faraday::ConnectionFailed", "Faraday::Error"},
		{"Faraday::TimeoutError", "Faraday::Error"},
		{"Faraday::SSLError", "Faraday::Error"},
		{"Faraday::ParsingError", "Faraday::Error"},
		{"Faraday::NilStatusError", "Faraday::ServerError"},
		{"Faraday::BadRequestError", "Faraday::ClientError"},
		{"Faraday::UnauthorizedError", "Faraday::ClientError"},
		{"Faraday::ForbiddenError", "Faraday::ClientError"},
		{"Faraday::ResourceNotFound", "Faraday::ClientError"},
		{"Faraday::ProxyAuthError", "Faraday::ClientError"},
		{"Faraday::ConflictError", "Faraday::ClientError"},
		{"Faraday::UnprocessableEntityError", "Faraday::ClientError"},
		{"Faraday::TooManyRequestsError", "Faraday::ClientError"},
	}
	for _, d := range defs {
		parent := vm.consts[d.parent].(*RClass)
		cls := newClass(d.qualified, parent)
		vm.consts[d.qualified] = cls
		mod.consts[d.qualified[len("Faraday::"):]] = cls
	}
	base := vm.consts["Faraday::Error"].(*RClass)
	base.define("response", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@response")
	})
}

// faradayNew builds a Faraday::Connection from Faraday.new's optional URL and
// keyword options (url:/headers:/params:), wires the default adapter through the
// faradayAdapter seam, and yields the connection to the configuration block.
func (vm *VM) faradayNew(args []object.Value, blk *Proc) object.Value {
	opts := faradayParseOptions(args)
	conn := &FaradayConnection{
		c: faraday.New(opts, func(c *faraday.Connection) { c.Adapter(faradayAdapter()) }),
	}
	if blk != nil {
		vm.callBlock(blk, []object.Value{conn})
	}
	return conn
}

// faradayParseOptions reads Faraday.new's arguments: an optional leading URL
// String and a trailing keyword Hash (url:/headers:/params:), a later url: key
// overriding the positional URL, as in the gem.
func faradayParseOptions(args []object.Value) faraday.Options {
	var opts faraday.Options
	var kw *object.Hash
	for _, a := range args {
		switch x := a.(type) {
		case *object.String:
			opts.URL = x.Str()
		case *object.Hash:
			kw = x
		}
	}
	if kw != nil {
		if v, ok := kw.Get(object.Symbol("url")); ok {
			opts.URL = v.ToS()
		}
		if h := faradayKwHash(kw, "headers"); h != nil {
			opts.Headers = rubyHashToHeaders(h)
		}
		if h := faradayKwHash(kw, "params"); h != nil {
			opts.Params = rubyHashToParams(h)
		}
	}
	return opts
}

// faradayKwHash returns a keyword option as a Ruby Hash, or nil when the key is
// absent or its value is not a Hash.
func faradayKwHash(kw *object.Hash, key string) *object.Hash {
	if v, ok := kw.Get(object.Symbol(key)); ok {
		if h, ok := v.(*object.Hash); ok {
			return h
		}
	}
	return nil
}

// registerFaradayConnection installs the Faraday::Connection surface: the
// middleware DSL (request/response/adapter/use-less basic_auth/authorization),
// the read accessors (headers/params/url_prefix) and the HTTP verb methods.
func (vm *VM) registerFaradayConnection(c *RClass) {
	connOf := func(self object.Value) *FaradayConnection { return self.(*FaradayConnection) }

	c.define("request", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		fc := connOf(self)
		name := faradayName(args[0])
		fc.c.Request(name, faradayStrArgs(args[1:])...)
		if name == "url_encoded" {
			fc.formEncode = true
		}
		return self
	})
	c.define("response", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		connOf(self).c.Response(faradayName(args[0]))
		return self
	})
	c.define("adapter", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		connOf(self).c.Adapter(faradayAdapter())
		return self
	})
	c.define("basic_auth", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		connOf(self).c.Request("basic_auth", args[0].ToS(), args[1].ToS())
		return self
	})
	c.define("authorization", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		connOf(self).c.Request("authorization", args[0].ToS(), args[1].ToS())
		return self
	})
	c.define("url_prefix", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(connOf(self).c.URLPrefix())
	})
	c.define("headers", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return headersToRubyHash(connOf(self).c.Headers())
	})
	c.define("params", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return paramsToRubyHash(connOf(self).c.Params())
	})

	for _, verb := range []string{"get", "head", "delete", "trace", "options"} {
		v := verb
		c.define(v, func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
			return vm.faradayRun(connOf(self), v, false, args, blk)
		})
	}
	for _, verb := range []string{"post", "put", "patch"} {
		v := verb
		c.define(v, func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
			return vm.faradayRun(connOf(self), v, true, args, blk)
		})
	}
}

// faradayRun issues one request on a connection: it resolves the path, folds the
// per-call params/headers/body and the Ruby block into the library's per-request
// block, dispatches the verb, and wraps the finished response (or raises the
// matching Faraday error).
func (vm *VM) faradayRun(fc *FaradayConnection, method string, hasBody bool, args []object.Value, blk *Proc) object.Value {
	path := faradayPath(args)
	var body any
	var params, headers *object.Hash
	if hasBody {
		bodyV := object.Value(object.NilV)
		if len(args) > 1 {
			bodyV = args[1]
		}
		headers = faradayHashAt(args, 2)
		body = rubyBodyToGo(bodyV, fc.formEncode)
	} else {
		params = faradayHashAt(args, 1)
		headers = faradayHashAt(args, 2)
	}
	goBlk := func(req *faraday.Request) {
		if headers != nil {
			applyRubyHeaders(req.Headers, headers)
		}
		if params != nil {
			applyRubyParams(req.Params, params)
		}
		if blk != nil {
			vm.callBlock(blk, []object.Value{&FaradayRequest{req}})
		}
	}
	resp, err := faradayVerb(fc.c, method, path, body, goBlk)
	if err != nil {
		vm.raiseFaradayError(err)
	}
	return &FaradayResponse{resp}
}

// faradayModuleVerb runs a module-level one-shot (Faraday.get/post/…) through a
// fresh default connection whose adapter comes from the faradayAdapter seam.
func (vm *VM) faradayModuleVerb(method string, hasBody bool, args []object.Value, blk *Proc) object.Value {
	fc := &FaradayConnection{
		c: faraday.New(faraday.Options{}, func(c *faraday.Connection) { c.Adapter(faradayAdapter()) }),
	}
	return vm.faradayRun(fc, method, hasBody, args, blk)
}

// faradayVerb dispatches a verb name to the matching Connection method. The name
// always comes from rbgo's fixed verb sets, so the default arm is unreachable
// from Ruby (covered white-box).
func faradayVerb(c *faraday.Connection, method, path string, body any, blk func(*faraday.Request)) (*faraday.Response, error) {
	switch method {
	case "get":
		return c.Get(path, blk)
	case "head":
		return c.Head(path, blk)
	case "delete":
		return c.Delete(path, blk)
	case "trace":
		return c.Trace(path, blk)
	case "options":
		return c.Options(path, blk)
	case "post":
		return c.Post(path, body, blk)
	case "put":
		return c.Put(path, body, blk)
	case "patch":
		return c.Patch(path, body, blk)
	}
	return nil, nil
}

// raiseFaradayError re-raises a library *faraday.Error as its matching Ruby
// exception (named by the error kind), carrying the message and, for a response
// error, the #response context Hash. Every error the library returns is a
// *faraday.Error, so the assertion is total.
func (vm *VM) raiseFaradayError(err error) {
	fe := err.(*faraday.Error)
	cls := vm.consts[string(fe.Kind)].(*RClass)
	exc := &RObject{class: cls, ivars: map[string]object.Value{}}
	exc.ivars["@message"] = object.NewString(fe.Message)
	if fe.Response != nil {
		exc.ivars["@response"] = faradayResponseHash(fe.Response)
	} else {
		exc.ivars["@response"] = object.NilV
	}
	panic(vm.excError(exc))
}

// registerFaradayRequest installs the Faraday::Request surface yielded to a
// per-request block: params/headers proxies, body assignment, url override and
// the method/path readers.
func (vm *VM) registerFaradayRequest(c *RClass) {
	reqOf := func(self object.Value) *faraday.Request { return self.(*FaradayRequest).r }

	c.define("params", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return &FaradayParams{reqOf(self).Params}
	})
	c.define("headers", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return &FaradayHeaders{reqOf(self).Headers}
	})
	c.define("body=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		reqOf(self).SetBody(rubyBodyToGo(args[0], false))
		return args[0]
	})
	c.define("body", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return goValueToRuby(reqOf(self).Body)
	})
	c.define("url", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		req := reqOf(self)
		if h := faradayHashAt(args, 1); h != nil {
			req.URL(args[0].ToS(), rubyHashToParams(h))
		} else {
			req.URL(args[0].ToS())
		}
		return object.NilV
	})
	c.define("method", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(reqOf(self).Method)
	})
	c.define("path", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(reqOf(self).Path)
	})
}

// registerFaradayResponse installs the read-only Faraday::Response surface.
func (vm *VM) registerFaradayResponse(c *RClass) {
	respOf := func(self object.Value) *faraday.Response { return self.(*FaradayResponse).r }

	c.define("status", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(respOf(self).Status()))
	})
	c.define("body", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return goValueToRuby(respOf(self).Body())
	})
	c.define("headers", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return headersToRubyHash(respOf(self).Headers())
	})
	c.define("success?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(respOf(self).Success())
	})
	c.define("reason_phrase", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(respOf(self).ReasonPhrase())
	})
	c.define("finished?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(respOf(self).Finished())
	})
}

// registerFaradayProxies installs the Hash-like [] / []= / to_h surface of the
// request headers and params proxies.
func (vm *VM) registerFaradayProxies(cHeaders, cParams *RClass) {
	cParams.define("[]", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if v, ok := self.(*FaradayParams).p.Get(args[0].ToS()); ok {
			return object.NewString(v)
		}
		return object.NilV
	})
	cParams.define("[]=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		self.(*FaradayParams).p.Set(args[0].ToS(), args[1].ToS())
		return args[1]
	})
	cParams.define("to_h", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return paramsToRubyHash(self.(*FaradayParams).p)
	})

	cHeaders.define("[]", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if v, ok := self.(*FaradayHeaders).h.Get(args[0].ToS()); ok {
			return object.NewString(v)
		}
		return object.NilV
	})
	cHeaders.define("[]=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		self.(*FaradayHeaders).h.Set(args[0].ToS(), args[1].ToS())
		return args[1]
	})
	cHeaders.define("to_h", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return headersToRubyHash(self.(*FaradayHeaders).h)
	})
}

// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	httprb "github.com/go-ruby-http/http"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// httprbTransport returns the terminal transport rbgo wires into every http.rb
// client — the library's net/http Transport in production. Tests leave it at the
// default and drive an in-process httptest server over loopback, so the suite
// touches no external network. The library performs the whole chainable-client
// abstraction around this seam; only the round-trip is host-provided.
var httprbTransport = func() httprb.Transport { return httprb.DefaultTransport() }

// registerHTTPrb installs the HTTP module (require "http"): the chainable client
// entry points on the module (HTTP.headers/auth/basic_auth/accept/timeout/follow/
// header, each branching a fresh HTTP::Client) and the module-level one-shot verbs
// (HTTP.get/post/…), the HTTP::Client with its chainable DSL and verb methods, the
// read-only HTTP::Response (#status/#code/#body/#headers/#parse/#content_type/…),
// the HTTP::Response::Status value with http.rb's status predicates, and the gem's
// error tree (HTTP::Error < StandardError with ConnectionError/RequestError/
// ResponseError/StateError/TimeoutError/HeaderError beneath it). The chainable
// client core lives in the github.com/go-ruby-http/http library; this file is the
// class + method wiring (see httprb_bind.go for the wrappers and conversions).
func (vm *VM) registerHTTPrb() {
	mod := newClass("HTTP", nil)
	mod.isModule = true
	vm.consts["HTTP"] = mod

	vm.registerHTTPrbErrors(mod)

	cClient := vm.httprbClass(mod, "Client", "HTTP::Client")
	cResp := vm.httprbClass(mod, "Response", "HTTP::Response")
	cStatus := newClass("HTTP::Response::Status", vm.cObject)
	cResp.consts["Status"] = cStatus
	vm.consts["HTTP::Response::Status"] = cStatus

	sm := func(name string, fn NativeFn) {
		mod.smethods[name] = &Method{name: name, owner: mod, native: fn}
	}
	sm("headers", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.httprbChain("headers", args)
	})
	sm("header", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.httprbChain("header", args)
	})
	sm("auth", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.httprbChain("auth", args)
	})
	sm("basic_auth", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.httprbChain("basic_auth", args)
	})
	sm("accept", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.httprbChain("accept", args)
	})
	sm("timeout", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.httprbChain("timeout", args)
	})
	sm("follow", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.httprbChain("follow", args)
	})
	for _, verb := range httprbVerbs {
		v := verb
		sm(v, func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			base := &HTTPrbClient{httprb.NewClient().WithTransport(httprbTransport())}
			return vm.httprbDo(base.c, v, args)
		})
	}

	vm.registerHTTPrbClient(cClient)
	vm.registerHTTPrbResponse(cResp)
	vm.registerHTTPrbStatus(cStatus)
}

// httprbVerbs is the fixed set of HTTP verbs rbgo exposes on the module and the
// client; the name always comes from this set, so httprbVerb's default arm is
// unreachable from Ruby (covered white-box).
var httprbVerbs = []string{"get", "post", "put", "delete", "head", "patch"}

// httprbClass creates an HTTP::* class under cObject, records it flat (for
// classOf) and nests it under the HTTP module by its simple name.
func (vm *VM) httprbClass(mod *RClass, simple, qualified string) *RClass {
	c := newClass(qualified, vm.cObject)
	vm.consts[qualified] = c
	mod.consts[simple] = c
	return c
}

// registerHTTPrbErrors installs http.rb's error tree, mirroring the gem: the root
// HTTP::Error < StandardError and the ConnectionError/RequestError/ResponseError/
// StateError/TimeoutError/HeaderError subclasses beneath it. Every class name
// equals the library's ErrorKind string, so a raised *httprb.Error maps to its
// Ruby class by name. HTTP::Error#response exposes the {status:, headers:, body:}
// context of a response error (nil for a transport/parse error).
func (vm *VM) registerHTTPrbErrors(mod *RClass) {
	defs := []struct{ qualified, parent string }{
		{"HTTP::Error", "StandardError"},
		{"HTTP::ConnectionError", "HTTP::Error"},
		{"HTTP::RequestError", "HTTP::Error"},
		{"HTTP::ResponseError", "HTTP::Error"},
		{"HTTP::TimeoutError", "HTTP::Error"},
		{"HTTP::HeaderError", "HTTP::Error"},
		{"HTTP::StateError", "HTTP::ResponseError"},
	}
	for _, d := range defs {
		parent := vm.consts[d.parent].(*RClass)
		cls := newClass(d.qualified, parent)
		vm.consts[d.qualified] = cls
		mod.consts[d.qualified[len("HTTP::"):]] = cls
	}
	base := vm.consts["HTTP::Error"].(*RClass)
	base.define("response", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@response")
	})
}

// httprbChain starts a fresh client bound to the seam transport and applies one
// chainable method to it, returning the branched HTTP::Client (matching the
// module-level HTTP.accept(:json)… entry points that http.rb exposes on
// HTTP::Chainable).
func (vm *VM) httprbChain(name string, args []object.Value) object.Value {
	base := &HTTPrbClient{httprb.NewClient().WithTransport(httprbTransport())}
	return httprbApplyChain(base, name, args)
}

// httprbApplyChain applies a single chainable method (headers/header/auth/
// basic_auth/accept/timeout/follow) to a client and returns the new branched
// client. The method name always comes from rbgo's fixed chainable set.
func httprbApplyChain(base *HTTPrbClient, name string, args []object.Value) object.Value {
	switch name {
	case "headers":
		return &HTTPrbClient{base.c.Headers(rubyHashToHTTPKV(args[0].(*object.Hash))...)}
	case "header":
		return &HTTPrbClient{base.c.Header(args[0].ToS(), args[1].ToS())}
	case "auth":
		return &HTTPrbClient{base.c.Auth(args[0].ToS())}
	case "basic_auth":
		user, pass := httprbBasicAuthArgs(args[0].(*object.Hash))
		return &HTTPrbClient{base.c.BasicAuth(user, pass)}
	case "accept":
		return &HTTPrbClient{base.c.Accept(httprbName(args[0]))}
	case "timeout":
		return &HTTPrbClient{base.c.Timeout(int(toInt(args[0])))}
	case "follow":
		return &HTTPrbClient{base.c.Follow()}
	}
	return base // unreachable: name is from rbgo's fixed chainable set
}

// httprbBasicAuthArgs reads a basic_auth keyword Hash ({user:, pass:}) into the
// user/password pair the library's BasicAuth takes.
func httprbBasicAuthArgs(h *object.Hash) (user, pass string) {
	if v, ok := h.Get(object.Symbol("user")); ok {
		user = v.ToS()
	}
	if v, ok := h.Get(object.Symbol("pass")); ok {
		pass = v.ToS()
	}
	return user, pass
}

// registerHTTPrbClient installs the HTTP::Client surface: the chainable DSL
// (returning a fresh branched client each time) and the verb methods.
func (vm *VM) registerHTTPrbClient(c *RClass) {
	clientOf := func(self object.Value) *HTTPrbClient { return self.(*HTTPrbClient) }

	for _, name := range []string{"headers", "header", "auth", "basic_auth", "accept", "timeout", "follow"} {
		n := name
		c.define(n, func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			return httprbApplyChain(clientOf(self), n, args)
		})
	}
	for _, verb := range httprbVerbs {
		v := verb
		c.define(v, func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			return vm.httprbDo(clientOf(self).c, v, args)
		})
	}
}

// httprbDo issues one request through a client: it resolves the URL and the
// trailing keyword body/params options, dispatches the verb through the seam
// transport, and wraps the finished response (or raises the matching HTTP error).
func (vm *VM) httprbDo(c *httprb.Client, verb string, args []object.Value) object.Value {
	uri := args[0].ToS()
	opts := httprbReqOptions(args)
	resp, err := httprbVerb(c, verb, uri, opts)
	if err != nil {
		vm.raiseHTTPrbError(err)
	}
	return &HTTPrbResponse{resp}
}

// httprbVerb dispatches a verb name to the matching Client method. The name always
// comes from httprbVerbs, so the default arm is unreachable from Ruby (covered
// white-box).
func httprbVerb(c *httprb.Client, verb, uri string, opts []httprb.RequestOption) (*httprb.Response, error) {
	switch verb {
	case "get":
		return c.Get(uri, opts...)
	case "post":
		return c.Post(uri, opts...)
	case "put":
		return c.Put(uri, opts...)
	case "delete":
		return c.Delete(uri, opts...)
	case "head":
		return c.Head(uri, opts...)
	case "patch":
		return c.Patch(uri, opts...)
	}
	return nil, nil
}

// raiseHTTPrbError re-raises a library *httprb.Error as its matching Ruby
// exception (named by the error kind), carrying the message and, for a response
// error, the #response context Hash. Every error the library returns is a
// *httprb.Error, so the assertion is total.
func (vm *VM) raiseHTTPrbError(err error) {
	he := err.(*httprb.Error)
	cls := vm.consts[string(he.Kind)].(*RClass)
	exc := &RObject{class: cls, ivars: map[string]object.Value{}}
	exc.ivars["@message"] = object.NewString(he.Message)
	exc.ivars["@response"] = httprbErrorResponseValue(he)
	panic(vm.excError(exc))
}

// registerHTTPrbResponse installs the read-only HTTP::Response surface.
func (vm *VM) registerHTTPrbResponse(c *RClass) {
	respOf := func(self object.Value) *httprb.Response { return self.(*HTTPrbResponse).r }

	c.define("status", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return &HTTPrbStatus{respOf(self).Status()}
	})
	c.define("code", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(respOf(self).Code()))
	})
	c.define("body", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(respOf(self).Body().String())
	})
	c.define("to_s", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(respOf(self).Body().String())
	})
	c.define("headers", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return httprbHeadersHash(respOf(self).Headers())
	})
	c.define("content_type", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(respOf(self).ContentType().MimeType)
	})
	c.define("reason", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(respOf(self).Reason())
	})
	c.define("uri", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(respOf(self).URI())
	})
	c.define("version", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(respOf(self).Version())
	})
	c.define("parse", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		var parsed any
		var err error
		if len(args) > 0 {
			parsed, err = respOf(self).Parse(httprbName(args[0]))
		} else {
			parsed, err = respOf(self).Parse()
		}
		if err != nil {
			vm.raiseHTTPrbError(err)
		}
		return goValueToRuby(parsed)
	})
}

// registerHTTPrbStatus installs the HTTP::Response::Status surface — the status
// code with http.rb's query predicates.
func (vm *VM) registerHTTPrbStatus(c *RClass) {
	statusOf := func(self object.Value) httprb.Status { return self.(*HTTPrbStatus).s }

	code := func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(statusOf(self).Code()))
	}
	c.define("code", code)
	c.define("to_i", code)
	c.define("to_s", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(statusOf(self).String())
	})
	c.define("reason", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(statusOf(self).Reason())
	})
	c.define("success?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(statusOf(self).Success())
	})
	c.define("informational?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(statusOf(self).Informational())
	})
	c.define("redirect?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(statusOf(self).Redirect())
	})
	c.define("client_error?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(statusOf(self).ClientError())
	})
	c.define("server_error?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(statusOf(self).ServerError())
	})
}

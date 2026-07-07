// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"strings"

	"github.com/go-ruby-webmock/webmock"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file installs the Ruby WebMock surface: the WebMock module (stub_request /
// disable_net_connect! / allow_net_connect! / reset! / enable! / disable! /
// assert_requested), the WebMock::RequestStub builder (with / to_return /
// to_raise / to_timeout) and the WebMock::NetConnectNotAllowedError class. Every
// method drives the process-wide webmock.Default registry — the Go analogue of
// Ruby's global WebMock module — so the interception hook in webmock.go
// (nethttpDoXfer -> webmockIntercept) answers the requests these stubs describe.
//
// The registry surface is registered eagerly; the interception itself stays inert
// until require "webmock" (or "webmock/minitest") flips vm.webmockActive through
// a featureHook, so an rbgo program that never requires webmock performs real
// Net::HTTP unchanged. Keyword/argument decoding reuses the shared acmeKwargs /
// acmeKwGet / acmeKeyName helpers (symbol-or-string keys), avoiding a duplicate.

// registerWebMock installs the WebMock module, its stub builder and the
// unregistered-request error, and wires the require "webmock" activation hooks. It
// runs after registerNetHTTPTransport so the Net::HTTP transport it intercepts
// already exists.
func (vm *VM) registerWebMock() {
	std := vm.consts["StandardError"].(*RClass)

	mod := newClass("WebMock", nil)
	mod.isModule = true
	vm.consts["WebMock"] = mod

	// The error a disabled net connection raises for an unregistered request;
	// published at the top level so raise() and `rescue` resolve it by name.
	nc := newClass("WebMock::NetConnectNotAllowedError", std)
	mod.consts["NetConnectNotAllowedError"] = nc
	vm.consts["WebMock::NetConnectNotAllowedError"] = nc

	stubClass := newClass("WebMock::RequestStub", vm.cObject)
	mod.consts["RequestStub"] = stubClass
	vm.consts["WebMock::RequestStub"] = stubClass
	vm.registerWebMockStubMethods(stubClass)

	sm := func(name string, fn NativeFn) {
		mod.smethods[name] = &Method{name: name, owner: mod, native: fn}
	}

	// WebMock.stub_request(method, uri) → a RequestStub builder.
	sm("stub_request", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		method, uri := webmockMethodURI(args)
		return &WebMockStub{stub: webmock.Default.StubRequest(method, uri), cls: stubClass}
	})
	// Net-connect policy (mirrors WebMock.disable_net_connect! / allow_net_connect!).
	sm("disable_net_connect!", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		webmock.DisableNetConnect()
		return object.NilV
	})
	sm("allow_net_connect!", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		webmock.AllowNetConnect()
		return object.NilV
	})
	// WebMock.reset! clears stubs and recorded request history.
	sm("reset!", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		webmock.Reset()
		return object.NilV
	})
	// WebMock.enable! / disable! toggle the transport interception explicitly.
	sm("enable!", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		vm.webmockActive = true
		return object.NilV
	})
	sm("disable!", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		vm.webmockActive = false
		return object.NilV
	})
	// Assertion helpers (also installed as bare methods by require "webmock/minitest").
	sm("assert_requested", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return webmockAssert(args, false)
	})
	sm("assert_not_requested", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return webmockAssert(args, true)
	})

	vm.featureHooks["webmock"] = func() { vm.webmockActive = true }
	vm.featureHooks["webmock/minitest"] = func() {
		vm.webmockActive = true
		vm.installWebMockMinitest()
	}
}

// registerWebMockStubMethods installs the fluent builder on WebMock::RequestStub:
// .with adds request constraints, .to_return / .to_raise / .to_timeout append the
// behaviours the matched request produces. Each returns the stub for chaining.
func (vm *VM) registerWebMockStubMethods(stub *RClass) {
	stub.define("with", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		self.(*WebMockStub).stub.With(webmockOptions(acmeKwargs(args))...)
		return self
	})
	stub.define("to_return", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		self.(*WebMockStub).stub.ToReturn(webmockStubResponse(acmeKwargs(args)))
		return self
	})
	stub.define("to_raise", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		self.(*WebMockStub).stub.ToRaise(webmockRaiseArg(args))
		return self
	})
	stub.define("to_timeout", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		self.(*WebMockStub).stub.ToTimeout()
		return self
	})
}

// installWebMockMinitest adds bare assert_requested / assert_not_requested methods
// (WebMock::API's minitest surface) on Object, so a test can call them directly —
// run once on the first require "webmock/minitest".
func (vm *VM) installWebMockMinitest() {
	vm.cObject.define("assert_requested", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return webmockAssert(args, false)
	})
	vm.cObject.define("assert_not_requested", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return webmockAssert(args, true)
	})
}

// webmockAssert backs assert_requested (negate=false) / assert_not_requested
// (negate=true): it counts recorded requests matching method/uri (and any
// with:-style constraints) against the expected times (default 1, or 0 for the
// negated form), raising Minitest::Assertion with webmock's message on mismatch.
func webmockAssert(args []object.Value, negate bool) object.Value {
	method, uri := webmockMethodURI(args)
	kw := acmeKwargs(args)
	times := 1
	if negate {
		times = 0
	}
	if t, ok := acmeKwGet(kw, "times").(object.Integer); ok {
		times = int(t)
	}
	if err := webmock.Default.AssertRequested(method, uri, times, webmockOptions(kw)...); err != nil {
		return raise("Minitest::Assertion", "%s", err.Error())
	}
	return object.True
}

// webmockMethodURI decodes the (method, uri) pair a stub / assertion begins with:
// the method is a Symbol or String upcased to the HTTP verb (":any" → "ANY", the
// engine's any-method wildcard) and the uri is a String pattern.
func webmockMethodURI(args []object.Value) (method, uri string) {
	return strings.ToUpper(acmeKeyName(args[0])), strArg(args[1])
}

// webmockOptions builds the webmock matcher constraints from a with:/assertion
// keyword hash: headers: (exact header map), body: (exact body) and query: (query
// parameters). Absent keys add no constraint.
func webmockOptions(h *object.Hash) []webmock.Option {
	var opts []webmock.Option
	if hv, ok := acmeKwGet(h, "headers").(*object.Hash); ok {
		opts = append(opts, webmock.Headers(webmockStrMap(hv)))
	}
	if bv, ok := acmeKwGet(h, "body").(*object.String); ok {
		opts = append(opts, webmock.Body(bv.Str()))
	}
	if qv, ok := acmeKwGet(h, "query").(*object.Hash); ok {
		opts = append(opts, webmock.Query(webmockStrMap(qv)))
	}
	return opts
}

// webmockStubResponse builds a StubResponse from a to_return keyword hash: status:
// (defaults to 200 in the engine when zero), body: and headers:.
func webmockStubResponse(h *object.Hash) webmock.StubResponse {
	var sr webmock.StubResponse
	if s, ok := acmeKwGet(h, "status").(object.Integer); ok {
		sr.Status = int(s)
	}
	if b, ok := acmeKwGet(h, "body").(*object.String); ok {
		sr.Body = b.Str()
	}
	if hv, ok := acmeKwGet(h, "headers").(*object.Hash); ok {
		sr.Headers = webmockMultiMap(hv)
	}
	return sr
}

// webmockRaiseArg decodes a to_raise argument into the exception the stub raises:
// a Class raises that class ("Exception from WebMock"), a String raises a
// RuntimeError with that message, anything else its to_s, and no argument a bare
// RuntimeError.
func webmockRaiseArg(args []object.Value) error {
	if len(args) == 0 {
		return &webmockRubyError{class: "RuntimeError", message: "Exception from WebMock"}
	}
	switch a := args[0].(type) {
	case *RClass:
		return &webmockRubyError{class: a.name, message: "Exception from WebMock"}
	case *object.String:
		return &webmockRubyError{class: "RuntimeError", message: a.Str()}
	}
	return &webmockRubyError{class: "RuntimeError", message: args[0].ToS()}
}

// webmockStrMap flattens a Ruby Hash of exact string constraints (headers/query)
// into a Go map, decoding symbol-or-string keys and stringifying values.
func webmockStrMap(h *object.Hash) map[string]string {
	m := make(map[string]string, len(h.Keys))
	for _, k := range h.Keys {
		v, _ := h.Get(k)
		m[acmeKeyName(k)] = strArg(v)
	}
	return m
}

// webmockMultiMap is webmockStrMap for the response header model, whose values are
// string slices (one value per key here).
func webmockMultiMap(h *object.Hash) map[string][]string {
	m := make(map[string][]string, len(h.Keys))
	for _, k := range h.Keys {
		v, _ := h.Get(k)
		m[acmeKeyName(k)] = []string{strArg(v)}
	}
	return m
}

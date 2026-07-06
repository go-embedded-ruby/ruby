// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	excon "github.com/go-ruby-excon/excon"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// exconDoer returns the terminal transport rbgo wires into every Excon connection
// — the library's net/http Doer in production. Tests leave it at the default and
// drive an in-process httptest server over loopback, so the suite touches no
// external network. The library performs the whole persistent-client abstraction
// (option merge, URL/query build, Basic auth, :expects assertion, :idempotent
// retry) around this seam; only the round-trip is host-provided.
var exconDoer = func() excon.Doer { return excon.NetHTTP() }

// registerExcon installs the Excon module (require "excon"): the connection
// factory Excon.new(url, opts) and the module-level one-shot verbs
// (Excon.get/post/…), the persistent Excon::Connection with its verb methods
// (get/post/put/delete/head/patch/request), the read-only Excon::Response
// (#status/#body/#headers/#reason_phrase/#remote_ip/#success?), and the gem's full
// Excon::Error tree (Excon::Error < StandardError with the transport errors, the
// HTTPStatus range bases and every named status error beneath it) plus the legacy
// Excon::Errors alias namespace. The client core lives in the
// github.com/go-ruby-excon/excon library; this file is the class + method wiring
// (see excon_bind.go for the wrappers and value conversions).
func (vm *VM) registerExcon() {
	mod := newClass("Excon", nil)
	mod.isModule = true
	vm.consts["Excon"] = mod

	cError := vm.registerExconErrors(mod)

	cConn := newClass("Excon::Connection", vm.cObject)
	mod.consts["Connection"] = cConn
	vm.consts["Excon::Connection"] = cConn
	cResp := newClass("Excon::Response", vm.cObject)
	mod.consts["Response"] = cResp
	vm.consts["Excon::Response"] = cResp
	_ = cError

	sm := func(name string, fn NativeFn) {
		mod.smethods[name] = &Method{name: name, owner: mod, native: fn}
	}
	sm("new", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		url, opts := exconNewArgs(args)
		return &ExconConnection{excon.New(url, opts).Transport(exconDoer())}
	})
	for _, verb := range exconVerbs {
		v := verb
		sm(v, func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			url, opts := exconNewArgs(args)
			conn := excon.New(url).Transport(exconDoer())
			opts.Method = v
			return vm.exconRun(conn, opts)
		})
	}

	vm.registerExconConnection(cConn)
	vm.registerExconResponse(cResp)
}

// exconVerbs is the fixed set of HTTP verbs rbgo exposes on the module and the
// connection; the name always comes from this set, so exconVerb's default arm is
// unreachable from Ruby (covered white-box).
var exconVerbs = []string{"get", "post", "put", "delete", "head", "patch"}

// exconNewArgs reads Excon.new / Excon.get's arguments: a leading URL String and
// an optional trailing options Hash.
func exconNewArgs(args []object.Value) (string, excon.Options) {
	var url string
	if len(args) > 0 {
		if s, ok := args[0].(*object.String); ok {
			url = s.Str()
		}
	}
	return url, rubyHashToExconOptions(exconHashAt(args, 1))
}

// exconErrorDef names an Excon::Error subclass and its parent (a qualified name).
type exconErrorDef struct{ qualified, parent string }

// registerExconErrors installs the full Excon::Error tree, mirroring the gem: the
// root Excon::Error < StandardError, the transport errors and the HTTPStatus range
// bases beneath it, and every named status error beneath its range base. Each
// subclass is nested under the Excon::Error namespace by its simple name; a
// separate Excon::Errors module aliases the same classes (the gem's legacy
// namespace). Every class name equals the library's ErrorKind string, so a raised
// *excon.Error maps to its Ruby class by name. Excon::Error#response exposes the
// Excon::Response that triggered a status error.
func (vm *VM) registerExconErrors(mod *RClass) *RClass {
	defs := []exconErrorDef{
		{"Excon::Error", "StandardError"},
		{"Excon::Error::Socket", "Excon::Error"},
		{"Excon::Error::Certificate", "Excon::Error::Socket"},
		{"Excon::Error::Timeout", "Excon::Error"},
		{"Excon::Error::ResponseParse", "Excon::Error"},
		{"Excon::Error::ProxyConnectionError", "Excon::Error"},
		{"Excon::Error::ProxyParse", "Excon::Error"},
		{"Excon::Error::TooManyRedirects", "Excon::Error"},
		{"Excon::Error::HTTPStatus", "Excon::Error"},
		{"Excon::Error::Informational", "Excon::Error::HTTPStatus"},
		{"Excon::Error::Redirection", "Excon::Error::HTTPStatus"},
		{"Excon::Error::Client", "Excon::Error::HTTPStatus"},
		{"Excon::Error::Server", "Excon::Error::HTTPStatus"},
		{"Excon::Error::BadRequest", "Excon::Error::Client"},
		{"Excon::Error::Unauthorized", "Excon::Error::Client"},
		{"Excon::Error::PaymentRequired", "Excon::Error::Client"},
		{"Excon::Error::Forbidden", "Excon::Error::Client"},
		{"Excon::Error::NotFound", "Excon::Error::Client"},
		{"Excon::Error::MethodNotAllowed", "Excon::Error::Client"},
		{"Excon::Error::NotAcceptable", "Excon::Error::Client"},
		{"Excon::Error::ProxyAuthenticationRequired", "Excon::Error::Client"},
		{"Excon::Error::RequestTimeout", "Excon::Error::Client"},
		{"Excon::Error::Conflict", "Excon::Error::Client"},
		{"Excon::Error::Gone", "Excon::Error::Client"},
		{"Excon::Error::LengthRequired", "Excon::Error::Client"},
		{"Excon::Error::PreconditionFailed", "Excon::Error::Client"},
		{"Excon::Error::RequestEntityTooLarge", "Excon::Error::Client"},
		{"Excon::Error::RequestURITooLong", "Excon::Error::Client"},
		{"Excon::Error::UnsupportedMediaType", "Excon::Error::Client"},
		{"Excon::Error::RequestedRangeNotSatisfiable", "Excon::Error::Client"},
		{"Excon::Error::ExpectationFailed", "Excon::Error::Client"},
		{"Excon::Error::UnprocessableEntity", "Excon::Error::Client"},
		{"Excon::Error::TooManyRequests", "Excon::Error::Client"},
		{"Excon::Error::InternalServerError", "Excon::Error::Server"},
		{"Excon::Error::NotImplemented", "Excon::Error::Server"},
		{"Excon::Error::BadGateway", "Excon::Error::Server"},
		{"Excon::Error::ServiceUnavailable", "Excon::Error::Server"},
		{"Excon::Error::GatewayTimeout", "Excon::Error::Server"},
	}
	var cError *RClass
	for _, d := range defs {
		parent := vm.consts[d.parent].(*RClass)
		cls := newClass(d.qualified, parent)
		vm.consts[d.qualified] = cls
		if d.qualified == "Excon::Error" {
			cError = cls
			mod.consts["Error"] = cls
		} else {
			cError.consts[exconSimpleName(d.qualified)] = cls
		}
	}
	cError.define("response", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@response")
	})

	// Excon::Errors — the gem's legacy alias namespace: the same class objects
	// reachable under Excon::Errors::<Name>.
	errs := newClass("Excon::Errors", nil)
	errs.isModule = true
	mod.consts["Errors"] = errs
	vm.consts["Excon::Errors"] = errs
	for _, d := range defs {
		errs.consts[exconSimpleName(d.qualified)] = vm.consts[d.qualified].(*RClass)
	}
	return cError
}

// exconSimpleName returns the last "::"-separated segment of a qualified name
// (e.g. "NotFound" for "Excon::Error::NotFound", "Error" for "Excon::Error").
func exconSimpleName(qualified string) string {
	for i := len(qualified) - 2; i >= 0; i-- {
		if qualified[i] == ':' && qualified[i+1] == ':' {
			return qualified[i+2:]
		}
	}
	return qualified
}

// registerExconConnection installs the Excon::Connection verb methods.
func (vm *VM) registerExconConnection(c *RClass) {
	connOf := func(self object.Value) *excon.Connection { return self.(*ExconConnection).c }

	c.define("request", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.exconRun(connOf(self), rubyHashToExconOptions(exconHashAt(args, 0)))
	})
	for _, verb := range exconVerbs {
		v := verb
		c.define(v, func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			opts := rubyHashToExconOptions(exconHashAt(args, 0))
			opts.Method = v
			return vm.exconRun(connOf(self), opts)
		})
	}
}

// exconRun issues one request on a connection through the seam transport and wraps
// the finished response (or raises the matching Excon error, e.g. on an :expects
// mismatch).
func (vm *VM) exconRun(conn *excon.Connection, opts excon.Options) object.Value {
	resp, err := exconVerb(conn, opts)
	if err != nil {
		vm.raiseExconError(err)
	}
	return &ExconResponse{resp}
}

// exconVerb dispatches an Options.Method to the matching Connection method. The
// method name always comes from rbgo's fixed verb sets (or Connection#request's
// merged default), so the default arm falls back to Connection.Request — the
// generic dispatch the library itself uses for an arbitrary verb.
func exconVerb(conn *excon.Connection, opts excon.Options) (*excon.Response, error) {
	switch opts.Method {
	case "get":
		return conn.Get(opts)
	case "post":
		return conn.Post(opts)
	case "put":
		return conn.Put(opts)
	case "delete":
		return conn.Delete(opts)
	case "head":
		return conn.Head(opts)
	case "patch":
		return conn.Patch(opts)
	}
	return conn.Request(opts)
}

// raiseExconError re-raises a library *excon.Error as its matching Ruby exception
// (named by the error kind), carrying the message and, for a status error, the
// #response context. Every error the library returns is an *excon.Error, so the
// assertion is total.
func (vm *VM) raiseExconError(err error) {
	ee := err.(*excon.Error)
	cls := vm.consts[string(ee.Kind)].(*RClass)
	exc := &RObject{class: cls, ivars: map[string]object.Value{}}
	exc.ivars["@message"] = object.NewString(ee.Message)
	if ee.Response != nil {
		exc.ivars["@response"] = &ExconResponse{ee.Response}
	} else {
		exc.ivars["@response"] = object.NilV
	}
	panic(vm.excError(exc))
}

// registerExconResponse installs the read-only Excon::Response surface.
func (vm *VM) registerExconResponse(c *RClass) {
	respOf := func(self object.Value) *excon.Response { return self.(*ExconResponse).r }

	c.define("status", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(respOf(self).Status()))
	})
	c.define("body", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(respOf(self).Body())
	})
	c.define("headers", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return exconHeadersHash(respOf(self).Headers())
	})
	c.define("reason_phrase", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(respOf(self).ReasonPhrase())
	})
	c.define("remote_ip", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(respOf(self).RemoteIp())
	})
	c.define("success?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(respOf(self).Success())
	})
}

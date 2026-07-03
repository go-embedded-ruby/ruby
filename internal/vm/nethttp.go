// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerNetHTTP installs the Net module with the net/http + net/https loadable
// shell: enough class-and-constant structure that `require "net/http"` /
// `require "net/https"` complete and Puppet's load-time references resolve
// (Net::HTTP, the Net::HTTPResponse status subclass tree, Net::HTTPHeader, the
// request verb classes, and the Net timeout/error classes). Actual socket I/O is
// a later round: the networking methods (start/request/get/...) raise
// NotImplementedError, while the cheap-and-real header surface is implemented.
func (vm *VM) registerNetHTTP() {
	std := object.Kind[*RClass](vm.consts["StandardError"])

	net := newClass("Net", nil)
	net.isModule = true
	vm.consts["Net"] = net

	notImpl := func(what string) NativeFn {
		return func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			return raise("NotImplementedError", "net/http networking not yet supported (%s)", what)
		}
	}

	// --- Net::HTTPHeader: a real header map mixin (cheap, no networking) ---------
	header := newClass("Net::HTTPHeader", nil)
	header.isModule = true
	net.consts["HTTPHeader"] = header
	defNetHTTPHeader(header)

	// --- Net::HTTP --------------------------------------------------------------
	http := newClass("Net::HTTP", vm.cObject)
	http.includes = append(http.includes, header)
	net.consts["HTTP"] = http
	// Class-level conveniences and the instance networking surface are all stubbed:
	// they need real sockets (next round).
	for _, m := range []string{"get", "post", "get_response", "start"} {
		http.smethods[m] = &Method{name: m, owner: http, native: notImpl("Net::HTTP." + m)}
	}
	http.smethods["new"] = &Method{name: "new", owner: http,
		native: func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			// Constructing the object is allowed (Puppet builds one, then configures
			// it); only the I/O methods raise.
			return &RObject{class: http, ivars: map[string]object.Value{}}
		}}
	for _, m := range []string{"start", "request", "get", "post", "head", "put", "delete", "finish"} {
		http.define(m, notImpl("Net::HTTP#"+m))
	}

	// Request verb classes nested under Net::HTTP (Net::HTTP::Get, ...). Each is a
	// header-carrying object; #new builds one, the body is a later round.
	for _, verb := range []string{"Get", "Head", "Post", "Put", "Delete", "Patch", "Options"} {
		rc := newClass("Net::HTTP::"+verb, vm.cObject)
		rc.includes = append(rc.includes, header)
		rc.smethods["new"] = &Method{name: "new", owner: rc,
			native: func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
				o := &RObject{class: object.Kind[*RClass](self), ivars: map[string]object.Value{}}
				o.ivars["@header"] = object.NewHash()
				return o
			}}
		http.consts[verb] = rc
	}

	// --- Net::HTTPResponse status subclass tree ---------------------------------
	resp := newClass("Net::HTTPResponse", vm.cObject)
	resp.includes = append(resp.includes, header)
	net.consts["HTTPResponse"] = resp
	defNetHTTPResponse(resp)

	// Category bases + the concrete status classes Puppet references, each a
	// subclass of its category so `is_a?(Net::HTTPSuccess)` works.
	mk := func(name string, super *RClass) *RClass {
		c := newClass("Net::"+name, super)
		net.consts[name] = c
		return c
	}
	info := mk("HTTPInformation", resp)
	success := mk("HTTPSuccess", resp)
	redirect := mk("HTTPRedirection", resp)
	clientErr := mk("HTTPClientError", resp)
	serverErr := mk("HTTPServerError", resp)
	mk("HTTPUnknownResponse", resp)
	_ = info
	mk("HTTPOK", success)
	mk("HTTPCreated", success)
	mk("HTTPNoContent", success)
	mk("HTTPMovedPermanently", redirect)
	mk("HTTPFound", redirect)
	mk("HTTPSeeOther", redirect)
	mk("HTTPNotModified", redirect)
	mk("HTTPBadRequest", clientErr)
	mk("HTTPUnauthorized", clientErr)
	mk("HTTPForbidden", clientErr)
	mk("HTTPNotFound", clientErr)
	mk("HTTPNotAcceptable", clientErr)
	mk("HTTPInternalServerError", serverErr)
	mk("HTTPBadGateway", serverErr)
	mk("HTTPServiceUnavailable", serverErr)
	mk("HTTPGatewayTimeout", serverErr)

	// --- Net error / timeout classes --------------------------------------------
	net.consts["HTTPError"] = newClass("Net::HTTPError", std)
	net.consts["HTTPBadResponse"] = newClass("Net::HTTPBadResponse", std)
	net.consts["HTTPFatalError"] = newClass("Net::HTTPFatalError", std)
	net.consts["OpenTimeout"] = newClass("Net::OpenTimeout", std)
	net.consts["ReadTimeout"] = newClass("Net::ReadTimeout", std)
	net.consts["WriteTimeout"] = newClass("Net::WriteTimeout", std)
}

// defNetHTTPHeader implements the cheap, real Net::HTTPHeader surface over an
// instance @header Hash (keys downcased, as Net::HTTP does), so manipulating
// request/response headers works without any networking.
func defNetHTTPHeader(header *RClass) {
	hashOf := func(self object.Value) *object.Hash {
		o, ok := object.KindOK[*RObject](self)
		if !ok {
			raise("TypeError", "Net::HTTPHeader expects an object receiver")
		}
		h, ok := object.KindOK[*object.Hash](o.ivars["@header"])
		if !ok {
			h = object.NewHash()
			o.ivars["@header"] = h
		}
		return h
	}
	header.define("[]", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		v, ok := hashOf(self).Get(object.NewString(headerKey(args[0])))
		if !ok {
			return object.NilV
		}
		return v
	})
	header.define("[]=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		hashOf(self).Set(object.NewString(headerKey(args[0])), args[1])
		return args[1]
	})
	header.define("key?", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		_, ok := hashOf(self).Get(object.NewString(headerKey(args[0])))
		return object.Bool(ok)
	})
	header.define("delete", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		v, _ := hashOf(self).Delete(object.NewString(headerKey(args[0])))
		return v
	})
}

// headerKey downcases a header name to Net::HTTP's canonical form.
func headerKey(v object.Value) string {
	return toLowerASCII(strArg(v))
}

// toLowerASCII lowercases ASCII letters (header names are ASCII), leaving other
// bytes untouched.
func toLowerASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

// defNetHTTPResponse implements the cheap accessors on Net::HTTPResponse that
// need no networking (code/message/body stored as ivars), so a constructed
// response object is inspectable.
func defNetHTTPResponse(resp *RClass) {
	resp.define("code", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@code")
	})
	resp.define("message", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@message")
	})
	resp.define("body", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return getIvar(self, "@body")
	})
}

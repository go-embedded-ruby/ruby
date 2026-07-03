// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"sort"

	rack "github.com/go-ruby-rack/rack"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// rackRelease is the Rack::RELEASE version string the binding advertises. Rack
// is a wire/contract library, so RELEASE is the SPEC generation it implements
// (Rack 3), not the go-ruby-rack module version.
const rackRelease = "3.1.0"

// RackRequest is the Ruby wrapper around a *rack.Request — the read-mostly view
// over a Rack env (Rack::Request). The env parsing, query decoding and header
// normalisation all live in github.com/go-ruby-rack/rack; this shell exposes the
// accessor surface (#path_info / #request_method / #params / …) to Ruby. The
// wrapper carries its own class so classOf reports Rack::Request.
type RackRequest struct {
	req *rack.Request
	cls *RClass
}

func (r *RackRequest) ToS() string     { return "#<Rack::Request>" }
func (r *RackRequest) Inspect() string { return r.ToS() }
func (r *RackRequest) Truthy() bool    { return true }

// RackResponse is the Ruby wrapper around a *rack.Response — the buffered
// status/headers/body a handler assembles (Rack::Response), yielding the SPEC
// [status, headers, body] triple through #finish / #to_a.
type RackResponse struct {
	resp *rack.Response
	cls  *RClass
}

func (r *RackResponse) ToS() string     { return "#<Rack::Response>" }
func (r *RackResponse) Inspect() string { return r.ToS() }
func (r *RackResponse) Truthy() bool    { return true }

// registerRack installs the Rack module and its Request / Response value objects
// plus the Rack::Utils escaping/query surface (require "rack" / "rack/utils"):
// Rack::Request.new(env) exposes the request accessors over go-ruby-rack;
// Rack::Response.new(body, status, headers) buffers a response and #finish
// returns the [status, headers, body] triple a Rack server consumes;
// Rack::Utils.escape / unescape / escape_html / parse_query / build_query bind
// straight through to the library so encoding comes from one authoritative
// source. The whole contract is deterministic Go — no socket, no network.
func (vm *VM) registerRack() {
	mod := newClass("Rack", nil)
	mod.isModule = true
	vm.consts["Rack"] = mod

	mod.consts["RELEASE"] = object.NewString(rackRelease)

	vm.registerRackRequest(mod)
	vm.registerRackResponse(mod)
	vm.registerRackUtils(mod)
}

// registerRackRequest installs Rack::Request and its accessor methods.
func (vm *VM) registerRackRequest(mod *RClass) {
	cls := newClass("Rack::Request", vm.cObject)
	mod.consts["Request"] = cls
	vm.consts["Rack::Request"] = cls

	cls.smethods["new"] = &Method{name: "new", owner: cls, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return &RackRequest{req: rack.NewRequest(rackEnv(args[0])), cls: cls}
	}}

	self := func(v object.Value) *rack.Request { return v.(*RackRequest).req }

	// String accessors straight off the env.
	str := func(fn func(*rack.Request) string) NativeFn {
		return func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
			return object.NewString(fn(self(v)))
		}
	}
	cls.define("request_method", str((*rack.Request).RequestMethod))
	cls.define("path_info", str((*rack.Request).PathInfo))
	cls.define("script_name", str((*rack.Request).ScriptName))
	cls.define("query_string", str((*rack.Request).QueryString))
	cls.define("server_name", str((*rack.Request).ServerName))
	cls.define("server_port", str((*rack.Request).ServerPort))
	cls.define("content_type", str((*rack.Request).ContentType))
	cls.define("media_type", str((*rack.Request).MediaType))
	cls.define("scheme", str((*rack.Request).Scheme))
	cls.define("host", str((*rack.Request).Host))
	cls.define("base_url", str((*rack.Request).BaseURL))
	cls.define("path", str((*rack.Request).Path))
	cls.define("fullpath", str((*rack.Request).Fullpath))
	cls.define("url", str((*rack.Request).URL))
	cls.define("ip", str((*rack.Request).IP))

	// Boolean predicates.
	boolean := func(fn func(*rack.Request) bool) NativeFn {
		return func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
			return object.Bool(fn(self(v)))
		}
	}
	cls.define("get?", boolean((*rack.Request).IsGet))
	cls.define("post?", boolean((*rack.Request).IsPost))
	cls.define("put?", boolean((*rack.Request).IsPut))
	cls.define("patch?", boolean((*rack.Request).IsPatch))
	cls.define("delete?", boolean((*rack.Request).IsDelete))
	cls.define("head?", boolean((*rack.Request).IsHead))
	cls.define("options?", boolean((*rack.Request).IsOptions))
	cls.define("xhr?", boolean((*rack.Request).XHR))
	cls.define("ssl?", boolean((*rack.Request).SSL))

	cls.define("port", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).Port()))
	})

	// #get_header(name) returns the raw env entry (or nil).
	cls.define("get_header", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		raw, ok := self(v).GetHeaderRaw(rackStr(args[0]))
		if !ok {
			return object.NilV
		}
		return rackFromGo(raw)
	})
	cls.define("has_header?", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return object.Bool(self(v).HasHeader(rackStr(args[0])))
	})

	// Parsed params as Ruby Hashes.
	cls.define("params", rackParamsMethod((*rack.Request).Params))
	cls.define("GET", rackParamsMethod((*rack.Request).GET))
	cls.define("POST", rackParamsMethod((*rack.Request).POST))
	cls.define("cookies", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return rackParamsToHash(self(v).Cookies())
	})
}

// rackParamsMethod adapts a (*rack.Request) accessor returning (*Params, error)
// into a native method returning a Ruby Hash, raising on a parse error.
func rackParamsMethod(fn func(*rack.Request) (*rack.Params, error)) NativeFn {
	return func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		p, err := fn(v.(*RackRequest).req)
		if err != nil {
			raise("ArgumentError", "%s", err.Error())
		}
		return rackParamsToHash(p)
	}
}

// registerRackResponse installs Rack::Response and its buffering surface.
func (vm *VM) registerRackResponse(mod *RClass) {
	cls := newClass("Rack::Response", vm.cObject)
	mod.consts["Response"] = cls
	vm.consts["Rack::Response"] = cls

	cls.smethods["new"] = &Method{name: "new", owner: cls, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		body := rackResponseBody(args)
		status := 200
		if len(args) > 1 {
			status = rackInt(args[1], 200)
		}
		var headers *rack.Headers
		if len(args) > 2 {
			headers = rackHeadersFrom(args[2])
		}
		return &RackResponse{resp: rack.NewResponse(body, status, headers), cls: cls}
	}}

	self := func(v object.Value) *rack.Response { return v.(*RackResponse).resp }

	cls.define("write", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		chunk := rackStr(rackArg(args))
		self(v).Write(chunk)
		return object.NewString(chunk)
	})
	cls.define("status", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self(v).Status()))
	})
	cls.define("status=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).SetStatus(rackInt(rackArg(args), 200))
		return rackArg(args)
	})
	cls.define("headers", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return rackHeadersToHash(self(v).Headers())
	})
	cls.define("[]", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return rackFromGo(self(v).GetHeader(rackStr(args[0])))
	})
	cls.define("[]=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		self(v).SetHeader(rackStr(args[0]), rackStr(args[1]))
		return args[1]
	})
	cls.define("set_header", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		self(v).SetHeader(rackStr(args[0]), rackStr(args[1]))
		return args[1]
	})
	cls.define("content_type", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).ContentType())
	})
	cls.define("content_type=", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		self(v).SetContentType(rackStr(rackArg(args)))
		return rackArg(args)
	})
	cls.define("location", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self(v).Location())
	})
	cls.define("redirect", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
		}
		status := 302
		if len(args) > 1 {
			status = rackInt(args[1], 302)
		}
		self(v).Redirect(rackStr(args[0]), status)
		return object.NilV
	})
	cls.define("body", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return rackBodyArray(self(v).Body())
	})
	cls.define("empty?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).Empty())
	})
	cls.define("finish", rackFinishMethod(self))
	cls.define("to_a", rackFinishMethod(self))
	cls.define("redirect?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).IsRedirect())
	})
	cls.define("ok?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).OK())
	})
	cls.define("not_found?", func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self(v).NotFound())
	})
}

// rackFinishMethod returns the [status, headers, body] triple as a Ruby Array,
// the SPEC form (#finish / #to_a).
func rackFinishMethod(self func(object.Value) *rack.Response) NativeFn {
	return func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		status, headers, body := self(v).Finish()
		return object.NewArray(object.IntValue(int64(status)), rackHeadersToHash(headers), rackBodyArray(body))
	}
}

// registerRackUtils installs the Rack::Utils module of pure encoding/query
// helpers (also the target of require "rack/utils").
func (vm *VM) registerRackUtils(mod *RClass) {
	util := newClass("Rack::Utils", nil)
	util.isModule = true
	mod.consts["Utils"] = util
	vm.consts["Rack::Utils"] = util

	def := func(name string, fn NativeFn) { util.smethods[name] = &Method{name: name, owner: util, native: fn} }

	strFn := func(fn func(string) string) NativeFn {
		return func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return object.NewString(fn(rackStr(rackArg(args))))
		}
	}
	def("escape", strFn(rack.Escape))
	def("escape_path", strFn(rack.EscapePath))
	def("escape_html", strFn(rack.EscapeHTML))
	def("unescape_html", strFn(rack.UnescapeHTML))
	def("unescape_path", strFn(rack.UnescapePath))
	def("unescape", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		s, err := rack.Unescape(rackStr(rackArg(args)))
		if err != nil {
			raise("ArgumentError", "%s", err.Error())
		}
		return object.NewString(s)
	})
	def("parse_query", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		p, err := rack.ParseQuery(rackStr(rackArg(args)), "&")
		if err != nil {
			raise("ArgumentError", "%s", err.Error())
		}
		return rackParamsToHash(p)
	})
	def("build_query", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(rack.BuildQuery(rackParamsFromHash(rackArg(args))))
	})
	def("parse_nested_query", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		p, err := rack.ParseNestedQuery(rackStr(rackArg(args)), rackSep(args, 1), 0)
		if err != nil {
			raise("ArgumentError", "%s", err.Error())
		}
		return rackParamsToHash(p)
	})
	def("build_nested_query", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		prefix := ""
		if len(args) > 1 {
			prefix = rackStr(args[1])
		}
		out, err := rack.BuildNestedQuery(rackToGoNested(rackArg(args)), prefix)
		if err != nil {
			raise("ArgumentError", "%s", err.Error())
		}
		return object.NewString(out)
	})
	def("q_values", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		qs := rack.QValues(rackStr(rackArg(args)))
		out := object.NewArrayFromSlice(make([]object.Value, len(qs)))
		for i, q := range qs {
			out.Elems[i] = object.NewArray(object.NewString(q.Value), object.Float(q.Quality))
		}
		return out
	})
	def("best_q_match", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		m := rack.BestQMatch(rackStr(args[0]), rackStrArray(args[1]))
		if m == "" {
			return object.NilV
		}
		return object.NewString(m)
	})
	// secure_compare(a, b) — constant-time string equality (CSRF/token-safe).
	def("secure_compare", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		return object.Bool(rack.SecureCompare(rackStr(args[0]), rackStr(args[1])))
	})
	// clean_path_info(path) — traversal-safe PATH_INFO normalisation.
	def("clean_path_info", strFn(rack.CleanPathInfo))
	// valid_path?(path) — valid UTF-8 and NUL-free.
	def("valid_path?", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(rack.ValidPath(rackStr(rackArg(args))))
	})
	// select_best_encoding(available_encodings, accept_encoding) — pick the best
	// content-coding. Note the MRI arg order: the server's available list first,
	// the pre-parsed [name, quality] accept list second. Returns nil when nothing
	// is acceptable.
	def("select_best_encoding", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		best, ok := rack.SelectBestEncoding(rackStrArray(args[0]), rackQValues(args[1]))
		if !ok {
			return object.NilV
		}
		return object.NewString(best)
	})
	// forwarded_values(forwarded_header) — parse an RFC 7239 Forwarded header into
	// a Hash of symbol parameter name → ordered value list, in header order. A
	// falsy header or a disallowed parameter yields nil (MRI's nil return).
	def("forwarded_values", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		header, has := rackForwardedArg(args[0])
		vals, present := rack.ForwardedValues(header, has)
		if !present {
			return object.NilV
		}
		h := object.NewHash()
		for _, k := range rackForwardedOrder(header) {
			list := vals[k]
			arr := object.NewArrayFromSlice(make([]object.Value, len(list)))
			for i, s := range list {
				arr.Elems[i] = object.NewString(s)
			}
			h.Set(object.Symbol(k), arr)
		}
		return h
	})
	def("parse_cookies_header", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return rackParamsToHash(rack.ParseCookiesHeader(rackStr(rackArg(args))))
	})
	def("parse_cookies", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return rackParamsToHash(rack.ParseCookiesHeader(rackCookieHeader(rackArg(args))))
	})
	def("status_code", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		a := rackArg(args)
		if sym, ok := a.(object.Symbol); ok {
			code, found := rack.SymbolToStatusCode(string(sym))
			if !found {
				raise("ArgumentError", "Unrecognized status code :%s", string(sym))
			}
			return object.IntValue(int64(code))
		}
		return object.IntValue(int64(rackInt(a, 500)))
	})
	// HTTP_STATUS_CODES — the code → reason-phrase table (Rack::Utils::
	// HTTP_STATUS_CODES), materialised as a Ruby Hash in ascending code order.
	codes := make([]int, 0, len(rack.HTTPStatusCodes))
	for c := range rack.HTTPStatusCodes {
		codes = append(codes, c)
	}
	sort.Ints(codes)
	statusTable := object.NewHash()
	for _, c := range codes {
		statusTable.Set(object.IntValue(int64(c)), object.NewString(rack.HTTPStatusCodes[c]))
	}
	util.consts["HTTP_STATUS_CODES"] = statusTable
}

// rackSep reads an optional query-separator argument at index i (Ruby's
// parse_nested_query second parameter). A missing or nil argument yields ""
// so the library falls back to its default ("&").
func rackSep(args []object.Value, i int) string {
	if len(args) <= i {
		return ""
	}
	if _, isNil := args[i].(object.Nil); isNil || args[i] == nil {
		return ""
	}
	return rackStr(args[i])
}

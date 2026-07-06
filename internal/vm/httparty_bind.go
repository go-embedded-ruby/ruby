// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	httparty "github.com/go-ruby-httparty/httparty"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph (object.Value)
// and the interpreter-independent github.com/go-ruby-httparty/httparty library.
// The library owns the whole HTTP-client behaviour around the wire — URL building
// (base_uri + path + escaped query), body/query encoding, Basic auth, redirect
// following and content-type-aware response parsing — while the transport itself
// is a host seam (a httparty.Doer). rbgo wires that seam to its real net/http
// transport through httpartyAdapter (tests point it at an in-process httptest
// server), maps the Ruby HTTParty surface onto the library API (see httparty.go
// for the class + method registration) and converts values across the boundary
// here. The generic Ruby⇄Go value conversions (rubyToGoValue / goValueToRuby)
// are shared with the Faraday binding (faraday_bind.go).

// httpartyAdapter returns the terminal transport rbgo wires into every HTTParty
// request — the library's net/http Doer in production. Tests override it with a
// Doer that talks to an in-process httptest server (or a canned stub), so the
// suite touches no external network. The library performs the whole client
// abstraction around this seam; only the round-trip is host-provided.
var httpartyAdapter = func() httparty.Doer { return httparty.NetHTTP() }

// HTTPartyResponse wraps a finished *httparty.Response (HTTParty::Response).
type HTTPartyResponse struct{ r *httparty.Response }

func (v *HTTPartyResponse) ToS() string     { return "#<HTTParty::Response>" }
func (v *HTTPartyResponse) Inspect() string { return "#<HTTParty::Response>" }
func (v *HTTPartyResponse) Truthy() bool    { return true }

// httpartyName coerces a format/name argument (a Symbol like :json or a String)
// to its plain string, matching HTTParty's Symbol-or-String options.
func httpartyName(v object.Value) string {
	if s, ok := v.(*object.String); ok {
		return s.Str()
	}
	return v.ToS()
}

// httpartyClientFromClass builds a library Client from the class-level DSL
// configuration stored on an including class's instance variables (see
// httpartyInclude), wiring the transport through the httpartyAdapter seam.
func (vm *VM) httpartyClientFromClass(cls *RClass) *httparty.Client {
	return httparty.NewClient(func(c *httparty.Client) {
		c.Adapter(httpartyAdapter())
		if v, ok := getIvar(cls, "@httparty_base_uri").(*object.String); ok {
			c.BaseURI(v.Str())
		}
		if h, ok := getIvar(cls, "@httparty_headers").(*object.Hash); ok {
			c.Headers(rubyHashToHTTPartyHeaders(h))
		}
		if h, ok := getIvar(cls, "@httparty_default_params").(*object.Hash); ok {
			c.DefaultParams(rubyHashToHTTPartyParams(h))
		}
		if h, ok := getIvar(cls, "@httparty_basic_auth").(*object.Hash); ok {
			user, pass := httpartyAuthPair(h)
			c.BasicAuth(user, pass)
		}
		if s := getIvar(cls, "@httparty_format"); !object.IsNil(s) {
			c.Format(httpartyName(s))
		}
	})
}

// httpartyRun issues one request on a client: it reads the URL and the per-call
// options Hash, dispatches the verb, and wraps the finished response (or raises
// the matching HTTParty error). Every verb shares the (url, options={}) shape.
func (vm *VM) httpartyRun(client *httparty.Client, method string, args []object.Value) object.Value {
	url := args[0].ToS()
	opts := httpartyParseOptions(args)
	resp, err := httpartyDispatch(client, method, url, opts)
	if err != nil {
		vm.httpartyRaise(err)
	}
	return &HTTPartyResponse{resp}
}

// httpartyDispatch routes a verb name to the matching Client method. The name
// always comes from rbgo's fixed verb set, so the default arm is unreachable from
// Ruby (covered white-box).
func httpartyDispatch(c *httparty.Client, method, url string, opts httparty.RequestOptions) (*httparty.Response, error) {
	switch method {
	case "get":
		return c.Get(url, opts)
	case "post":
		return c.Post(url, opts)
	case "put":
		return c.Put(url, opts)
	case "patch":
		return c.Patch(url, opts)
	case "delete":
		return c.Delete(url, opts)
	case "head":
		return c.Head(url, opts)
	case "options":
		return c.Options(url, opts)
	}
	return nil, nil
}

// httpartyParseOptions reads a verb's trailing options Hash (query:/body:/headers:/
// basic_auth:/timeout:/follow_redirects:/format:) into a library RequestOptions.
// A missing or non-Hash second argument yields the zero options (a plain request).
func httpartyParseOptions(args []object.Value) httparty.RequestOptions {
	var opt httparty.RequestOptions
	if len(args) < 2 {
		return opt
	}
	h, ok := args[1].(*object.Hash)
	if !ok {
		return opt
	}
	if v, ok := httpartyOpt(h, "query"); ok {
		if q, ok := v.(*object.Hash); ok {
			opt.Query = rubyHashToHTTPartyParams(q)
		}
	}
	if v, ok := httpartyOpt(h, "body"); ok {
		opt.Body = httpartyBody(v)
	}
	if v, ok := httpartyOpt(h, "headers"); ok {
		if hh, ok := v.(*object.Hash); ok {
			opt.Headers = rubyHashToHTTPartyHeaders(hh)
		}
	}
	if v, ok := httpartyOpt(h, "basic_auth"); ok {
		if ah, ok := v.(*object.Hash); ok {
			user, pass := httpartyAuthPair(ah)
			opt.BasicAuth = &httparty.BasicAuth{Username: user, Password: pass}
		}
	}
	if v, ok := httpartyOpt(h, "timeout"); ok {
		if i, ok := v.(object.Integer); ok {
			opt.Timeout = int(i)
		}
	}
	if v, ok := httpartyOpt(h, "follow_redirects"); ok {
		b := v.Truthy()
		opt.FollowRedirects = &b
	}
	if v, ok := httpartyOpt(h, "format"); ok {
		opt.Format = httpartyName(v)
	}
	return opt
}

// httpartyOpt looks a keyword option up in an options Hash, accepting either a
// Symbol key (the idiomatic :query) or a String key ("query").
func httpartyOpt(h *object.Hash, key string) (object.Value, bool) {
	if v, ok := h.Get(object.Symbol(key)); ok {
		return v, true
	}
	if v, ok := h.Get(object.NewString(key)); ok {
		return v, true
	}
	return object.NilV, false
}

// httpartyBody converts a Ruby :body option to the value the library encodes: a
// Hash is carried as ordered form Params (application/x-www-form-urlencoded, as
// the gem form-encodes a Hash body), a String is sent verbatim, and any other
// value is JSON-encoded.
func httpartyBody(v object.Value) any {
	switch x := v.(type) {
	case *object.Hash:
		return rubyHashToHTTPartyParams(x)
	case *object.String:
		return x.Str()
	}
	if object.IsNil(v) {
		return nil
	}
	return rubyToGoValue(v)
}

// httpartyAuthPair reads the :username/:password (Symbol or String keyed)
// credentials from a basic_auth Hash.
func httpartyAuthPair(h *object.Hash) (user, pass string) {
	if v, ok := httpartyOpt(h, "username"); ok {
		user = v.ToS()
	}
	if v, ok := httpartyOpt(h, "password"); ok {
		pass = v.ToS()
	}
	return user, pass
}

// httpartyParsed returns HTTParty::Response#parsed_response: the content-type-
// aware parsed body as a Ruby value (Hash/Array for JSON or XML, String
// otherwise). A malformed JSON/XML body raises an HTTParty::Error, as the gem's
// parser does.
func (vm *VM) httpartyParsed(resp *httparty.Response) object.Value {
	v, err := resp.Parsed()
	if err != nil {
		vm.httpartyRaise(err)
	}
	return goValueToRuby(v)
}

// httpartyIndex implements HTTParty::Response#[] — parsed_response[key] — indexing
// the parsed Hash by key or the parsed Array by integer, and returning nil for
// anything else (a scalar/String parsed body or an out-of-range/missing key).
func (vm *VM) httpartyIndex(resp *httparty.Response, key object.Value) object.Value {
	switch parsed := vm.httpartyParsed(resp).(type) {
	case *object.Hash:
		if v, ok := parsed.Get(object.NewString(key.ToS())); ok {
			return v
		}
	case *object.Array:
		if i, ok := key.(object.Integer); ok {
			idx := int(i)
			if idx >= 0 && idx < len(parsed.Elems) {
				return parsed.Elems[idx]
			}
		}
	}
	return object.NilV
}

// httpartyRaise re-raises a library *httparty.Error as its matching Ruby
// exception (named by the error kind), carrying the message and, for a
// response-carrying error, the #response context. Every error the library
// returns is a *httparty.Error, so the assertion is total.
func (vm *VM) httpartyRaise(err error) {
	he := err.(*httparty.Error)
	cls, ok := vm.consts[string(he.Kind)].(*RClass)
	if !ok {
		cls = vm.consts["HTTParty::Error"].(*RClass)
	}
	exc := &RObject{class: cls, ivars: map[string]object.Value{}}
	exc.ivars["@message"] = object.NewString(he.Message)
	if he.Response != nil {
		exc.ivars["@response"] = &HTTPartyResponse{he.Response}
	} else {
		exc.ivars["@response"] = object.NilV
	}
	panic(vm.excError(exc))
}

// --- Ruby Hash ⇄ library Headers/Params ------------------------------------

// rubyHashToHTTPartyHeaders builds a case-insensitive library Headers from a Ruby
// Hash, keying and valuing by each entry's #to_s.
func rubyHashToHTTPartyHeaders(h *object.Hash) *httparty.Headers {
	hdr := httparty.NewHeaders()
	for _, k := range h.Keys {
		v, _ := h.Get(k)
		hdr.Set(k.ToS(), v.ToS())
	}
	return hdr
}

// rubyHashToHTTPartyParams builds an ordered library Params from a Ruby Hash.
func rubyHashToHTTPartyParams(h *object.Hash) *httparty.Params {
	p := httparty.NewParams()
	for _, k := range h.Keys {
		v, _ := h.Get(k)
		p.Set(k.ToS(), v.ToS())
	}
	return p
}

// httpartyHeadersToRubyHash renders a library Headers as a Ruby Hash of
// String→String, preserving the first-seen key casing and insertion order.
func httpartyHeadersToRubyHash(h *httparty.Headers) object.Value {
	rh := object.NewHash()
	if h == nil {
		return rh
	}
	for _, p := range h.Pairs() {
		rh.Set(object.NewString(p.Key), object.NewString(p.Val))
	}
	return rh
}

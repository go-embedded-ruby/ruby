// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	httprb "github.com/go-ruby-http/http"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph (object.Value)
// and the interpreter-independent github.com/go-ruby-http/http library — the
// chainable "http.rb" client (require "http"), NOT Ruby's stdlib net/http. The
// library owns everything http.rb does around the wire: the chainable client DSL
// (headers/auth/basic_auth/accept/timeout/follow), the request build (URL, merged
// headers, form/JSON/params/raw body encoding), redirect following, the Response
// (status predicates, headers, content-type, JSON parse) and the HTTP::Error
// tree. Only the round-trip is a host seam (an httprb.Transport); rbgo wires the
// library's net/http transport through httprbTransport so tests point it at an
// in-process httptest server. rbgo wraps each library object as a Ruby object
// reporting the matching HTTP::* class (see httprb.go for the class + method
// registration) and converts values across the boundary here.

// The wrapper types. Each holds a value from the library and reports the matching
// HTTP::* class (see classOf); the methods registered in httprb.go operate on it.

// HTTPrbClient wraps an *httprb.Client (HTTP::Client / HTTP::Chainable). Each
// chainable method returns a fresh branched client, exactly like http.rb.
type HTTPrbClient struct{ c *httprb.Client }

// HTTPrbResponse wraps a finished *httprb.Response (HTTP::Response).
type HTTPrbResponse struct{ r *httprb.Response }

// HTTPrbStatus wraps an httprb.Status (HTTP::Response::Status), the status code
// with http.rb's query predicates.
type HTTPrbStatus struct{ s httprb.Status }

func (v *HTTPrbClient) ToS() string       { return "#<HTTP::Client>" }
func (v *HTTPrbClient) Inspect() string   { return "#<HTTP::Client>" }
func (v *HTTPrbClient) Truthy() bool      { return true }
func (v *HTTPrbResponse) ToS() string     { return "#<HTTP::Response>" }
func (v *HTTPrbResponse) Inspect() string { return "#<HTTP::Response>" }
func (v *HTTPrbResponse) Truthy() bool    { return true }
func (v *HTTPrbStatus) ToS() string       { return "#<HTTP::Response::Status>" }
func (v *HTTPrbStatus) Inspect() string   { return "#<HTTP::Response::Status>" }
func (v *HTTPrbStatus) Truthy() bool      { return true }

// httprbName coerces a header/accept name argument (a Symbol like :json or a
// String) to its plain string, matching http.rb's Symbol-or-String DSL.
func httprbName(v object.Value) string {
	if s, ok := v.(*object.String); ok {
		return s.Str()
	}
	return v.ToS()
}

// rubyHashToHTTPValues builds an ordered library Values (form body / query
// params) from a Ruby Hash, keying and valuing by each entry's #to_s and keeping
// insertion order (http.rb keeps the Hash order in its form encoder).
func rubyHashToHTTPValues(h *object.Hash) *httprb.Values {
	vals := httprb.NewValues()
	for _, k := range h.Keys {
		v, _ := h.Get(k)
		vals.Add(k.ToS(), v.ToS())
	}
	return vals
}

// rubyHashToHTTPKV renders a Ruby Hash as the ordered []httprb.KV the chainable
// Headers constructor accepts.
func rubyHashToHTTPKV(h *object.Hash) []httprb.KV {
	kvs := make([]httprb.KV, 0, h.Len())
	for _, k := range h.Keys {
		v, _ := h.Get(k)
		kvs = append(kvs, httprb.KV{Key: k.ToS(), Val: v.ToS()})
	}
	return kvs
}

// httprbHeadersHash renders a library Headers as a Ruby Hash of String→String,
// preserving the first-seen key casing and insertion order.
func httprbHeadersHash(h *httprb.Headers) object.Value {
	rh := object.NewHash()
	for _, p := range h.Pairs() {
		rh.Set(object.NewString(p.Key), object.NewString(p.Val))
	}
	return rh
}

// httprbReqOptions reads a verb's trailing keyword Hash (json:/form:/params:/
// body:) into the library request options. args[0] is the URL; the options Hash,
// when present, is the last argument.
func httprbReqOptions(args []object.Value) []httprb.RequestOption {
	h := httprbHashAt(args, 1)
	if h == nil {
		return nil
	}
	var opts []httprb.RequestOption
	if v, ok := h.Get(object.Symbol("json")); ok {
		opts = append(opts, httprb.JSON(rubyToGoValue(v)))
	}
	if hh := httprbKwHash(h, "form"); hh != nil {
		opts = append(opts, httprb.Form(rubyHashToHTTPValues(hh)))
	}
	if hh := httprbKwHash(h, "params"); hh != nil {
		opts = append(opts, httprb.Params(rubyHashToHTTPValues(hh)))
	}
	if v, ok := h.Get(object.Symbol("body")); ok {
		opts = append(opts, httprb.Body(v.ToS()))
	}
	return opts
}

// httprbHashAt returns args[i] as an *object.Hash, or nil when out of range or not
// a Hash.
func httprbHashAt(args []object.Value, i int) *object.Hash {
	if i < len(args) {
		if h, ok := args[i].(*object.Hash); ok {
			return h
		}
	}
	return nil
}

// httprbKwHash returns a keyword option as a Ruby Hash, or nil when the key is
// absent or its value is not a Hash.
func httprbKwHash(kw *object.Hash, key string) *object.Hash {
	if v, ok := kw.Get(object.Symbol(key)); ok {
		if h, ok := v.(*object.Hash); ok {
			return h
		}
	}
	return nil
}

// httprbErrorResponseValue builds the Ruby value an HTTP::Error exposes through
// #response: the {status:, headers:, body:} Hash for a response error, or nil
// when the error carries no response (a transport or parse error).
func httprbErrorResponseValue(e *httprb.Error) object.Value {
	if e.Response == nil {
		return object.NilV
	}
	h := object.NewHash()
	h.Set(object.Symbol("status"), object.IntValue(int64(e.Response.Code())))
	h.Set(object.Symbol("headers"), httprbHeadersHash(e.Response.Headers()))
	h.Set(object.Symbol("body"), object.NewString(e.Response.Body().String()))
	return h
}

// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"fmt"
	"math"
	"sort"

	faraday "github.com/go-ruby-faraday/faraday"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph (object.Value)
// and the interpreter-independent github.com/go-ruby-faraday/faraday library. The
// library owns the whole HTTP-client abstraction around the wire — the connection
// builder, the middleware stack (url-encoded/JSON encoding, authorization,
// JSON parsing, status→error mapping) and the URL/params/headers handling — while
// the transport itself is a host seam (a faraday.Doer). rbgo wraps each library
// object as a Ruby object reporting the matching Faraday::* class (see faraday.go
// for the class + method registration) and converts values across the boundary
// here. The default adapter is the library's net/http Doer, injected through
// faradayAdapter so tests can point it at an in-process httptest server.

// The wrapper types. Each holds a pointer into the library and reports the
// matching Faraday::* class (see classOf); the methods registered in faraday.go
// operate on the held value.

// FaradayConnection wraps a *faraday.Connection (Faraday::Connection). formEncode
// records whether the url_encoded request middleware was registered, so a Hash
// body is carried as ordered form Params (which UrlEncoded encodes) rather than a
// JSON value.
type FaradayConnection struct {
	c          *faraday.Connection
	formEncode bool
}

// FaradayRequest wraps the mutable *faraday.Request yielded to a per-request
// block (Faraday::Request).
type FaradayRequest struct{ r *faraday.Request }

// FaradayResponse wraps a finished *faraday.Response (Faraday::Response).
type FaradayResponse struct{ r *faraday.Response }

// FaradayParams wraps a *faraday.Params proxy (a request's query params).
type FaradayParams struct{ p *faraday.Params }

// FaradayHeaders wraps a *faraday.Headers proxy (a request's headers).
type FaradayHeaders struct{ h *faraday.Headers }

func (v *FaradayConnection) ToS() string     { return "#<Faraday::Connection>" }
func (v *FaradayConnection) Inspect() string { return "#<Faraday::Connection>" }
func (v *FaradayConnection) Truthy() bool    { return true }
func (v *FaradayRequest) ToS() string        { return "#<Faraday::Request>" }
func (v *FaradayRequest) Inspect() string    { return "#<Faraday::Request>" }
func (v *FaradayRequest) Truthy() bool       { return true }
func (v *FaradayResponse) ToS() string       { return "#<Faraday::Response>" }
func (v *FaradayResponse) Inspect() string   { return "#<Faraday::Response>" }
func (v *FaradayResponse) Truthy() bool      { return true }
func (v *FaradayParams) ToS() string         { return "#<Faraday::Utils::ParamsHash>" }
func (v *FaradayParams) Inspect() string     { return "#<Faraday::Utils::ParamsHash>" }
func (v *FaradayParams) Truthy() bool        { return true }
func (v *FaradayHeaders) ToS() string        { return "#<Faraday::Utils::Headers>" }
func (v *FaradayHeaders) Inspect() string    { return "#<Faraday::Utils::Headers>" }
func (v *FaradayHeaders) Truthy() bool       { return true }

// faradayName coerces a middleware/adapter name argument (a Symbol like :json or a
// String) to its plain string, matching Faraday's Symbol-or-String DSL.
func faradayName(v object.Value) string {
	if s, ok := v.(*object.String); ok {
		return s.Str()
	}
	return v.ToS()
}

// faradayStrArgs maps trailing DSL arguments (e.g. authorization type/value) to
// plain strings.
func faradayStrArgs(args []object.Value) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = a.ToS()
	}
	return out
}

// faradayPath returns the request path argument: the first positional argument
// unless it is an options Hash (Faraday's get/post accept a leading Hash-less
// path, then optional params/body and headers Hashes).
func faradayPath(args []object.Value) string {
	if len(args) > 0 {
		if _, ok := args[0].(*object.Hash); !ok {
			return args[0].ToS()
		}
	}
	return ""
}

// faradayHashAt returns args[i] as an *object.Hash, or nil when the index is out
// of range or the argument is not a Hash.
func faradayHashAt(args []object.Value, i int) *object.Hash {
	if i < len(args) {
		if h, ok := args[i].(*object.Hash); ok {
			return h
		}
	}
	return nil
}

// --- Ruby ⇄ Go value conversion --------------------------------------------

// rubyToGoValue maps a Ruby value to the plain Go value the library's request
// middleware marshal (encoding/json for a JSON body). Containers convert
// recursively; a Hash becomes a string-keyed map with keys taken from each key's
// #to_s, mirroring how Faraday JSON-encodes a Ruby Hash body.
func rubyToGoValue(v object.Value) any {
	switch x := v.(type) {
	case object.Integer:
		return int64(x)
	case *object.Bignum:
		return x.I
	case object.Float:
		return float64(x)
	case object.Bool:
		return bool(x)
	case *object.String:
		return x.Str()
	case object.Symbol:
		return string(x)
	case *object.Array:
		out := make([]any, len(x.Elems))
		for i, e := range x.Elems {
			out[i] = rubyToGoValue(e)
		}
		return out
	case *object.Hash:
		m := make(map[string]any, x.Len())
		for _, k := range x.Keys {
			val, _ := x.Get(k)
			m[k.ToS()] = rubyToGoValue(val)
		}
		return m
	}
	if object.IsNil(v) {
		return nil
	}
	return v.ToS()
}

// goValueToRuby maps a Go value the library produced — a raw response body string
// or a JSON-decoded value (nil / bool / float64 / string / []any / map[string]any)
// — back to a Ruby value. An integral float becomes an Integer, matching how
// JSON.parse renders whole numbers in the Faraday gem.
func goValueToRuby(v any) object.Value {
	switch x := v.(type) {
	case nil:
		return object.NilV
	case bool:
		return object.Bool(x)
	case string:
		return object.NewString(x)
	case float64:
		if x == math.Trunc(x) && x >= math.MinInt64 && x <= math.MaxInt64 {
			return object.IntValue(int64(x))
		}
		return object.Float(x)
	case []any:
		elems := make([]object.Value, len(x))
		for i, e := range x {
			elems[i] = goValueToRuby(e)
		}
		return object.NewArrayFromSlice(elems)
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		h := object.NewHash()
		for _, k := range keys {
			h.Set(object.NewString(k), goValueToRuby(x[k]))
		}
		return h
	}
	return object.NewString(fmt.Sprint(v))
}

// rubyBodyToGo converts a Ruby request body. When the connection registered the
// url_encoded middleware and the body is a Hash, it is carried as ordered form
// Params (which UrlEncoded encodes to application/x-www-form-urlencoded);
// otherwise it converts to a plain Go value for JSON/raw encoding.
func rubyBodyToGo(v object.Value, formEncode bool) any {
	if object.IsNil(v) {
		return nil
	}
	if formEncode {
		if h, ok := v.(*object.Hash); ok {
			return rubyHashToParams(h)
		}
	}
	return rubyToGoValue(v)
}

// --- Ruby Hash ⇄ library Headers/Params ------------------------------------

// rubyHashToHeaders builds a case-insensitive library Headers from a Ruby Hash,
// keying and valuing by each entry's #to_s.
func rubyHashToHeaders(h *object.Hash) *faraday.Headers {
	fh := faraday.NewHeaders()
	applyRubyHeaders(fh, h)
	return fh
}

// rubyHashToParams builds an ordered library Params from a Ruby Hash.
func rubyHashToParams(h *object.Hash) *faraday.Params {
	fp := faraday.NewParams()
	applyRubyParams(fp, h)
	return fp
}

// applyRubyHeaders sets every entry of a Ruby Hash into an existing Headers.
func applyRubyHeaders(dst *faraday.Headers, h *object.Hash) {
	for _, k := range h.Keys {
		v, _ := h.Get(k)
		dst.Set(k.ToS(), v.ToS())
	}
}

// applyRubyParams sets every entry of a Ruby Hash into an existing Params.
func applyRubyParams(dst *faraday.Params, h *object.Hash) {
	for _, k := range h.Keys {
		v, _ := h.Get(k)
		dst.Set(k.ToS(), v.ToS())
	}
}

// headersToRubyHash renders a library Headers as a Ruby Hash of String→String,
// preserving the first-seen key casing and insertion order.
func headersToRubyHash(h *faraday.Headers) object.Value {
	rh := object.NewHash()
	for _, p := range h.Pairs() {
		rh.Set(object.NewString(p.Key), object.NewString(p.Val))
	}
	return rh
}

// paramsToRubyHash renders a library Params as a Ruby Hash of String→String.
func paramsToRubyHash(p *faraday.Params) object.Value {
	rh := object.NewHash()
	for _, pair := range p.Pairs() {
		rh.Set(object.NewString(pair.Key), object.NewString(pair.Val))
	}
	return rh
}

// faradayResponseHash builds the Ruby Hash a Faraday error exposes through
// #response — {status:, headers:, body:} — mirroring the gem's response_values.
func faradayResponseHash(resp *faraday.Response) object.Value {
	h := object.NewHash()
	h.Set(object.Symbol("status"), object.IntValue(int64(resp.Status())))
	h.Set(object.Symbol("headers"), headersToRubyHash(resp.Headers()))
	h.Set(object.Symbol("body"), goValueToRuby(resp.Body()))
	return h
}

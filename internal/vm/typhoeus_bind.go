// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"time"

	typhoeus "github.com/go-ruby-typhoeus/typhoeus"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph (object.Value)
// and the interpreter-independent github.com/go-ruby-typhoeus/typhoeus library —
// the parallel HTTP client (require "typhoeus"). The library owns the whole
// object model around the wire: the Request with its Options, the on-complete
// callbacks, the Response (code/body/headers/total_time/return_code/success?), and
// the gem's signature Hydra runner that executes a queue of requests concurrently.
// Only the round-trip is a host seam (a typhoeus.Transport); rbgo wires the
// library's net/http transport through typhoeusTransport so tests point it at an
// in-process httptest server. rbgo wraps each library object as a Ruby object
// reporting the matching Typhoeus::* class (see typhoeus.go for the class + method
// registration) and converts values across the boundary here.

// The wrapper types. Each holds a pointer into the library and reports the
// matching Typhoeus::* class (see classOf); the methods registered in typhoeus.go
// operate on the held value.

// TyphoeusRequest wraps a *typhoeus.Request (Typhoeus::Request).
type TyphoeusRequest struct{ r *typhoeus.Request }

// TyphoeusResponse wraps a *typhoeus.Response (Typhoeus::Response).
type TyphoeusResponse struct{ r *typhoeus.Response }

// TyphoeusHydra wraps a *typhoeus.Hydra (Typhoeus::Hydra), the parallel runner.
type TyphoeusHydra struct{ h *typhoeus.Hydra }

func (v *TyphoeusRequest) ToS() string      { return "#<Typhoeus::Request>" }
func (v *TyphoeusRequest) Inspect() string  { return "#<Typhoeus::Request>" }
func (v *TyphoeusRequest) Truthy() bool     { return true }
func (v *TyphoeusResponse) ToS() string     { return "#<Typhoeus::Response>" }
func (v *TyphoeusResponse) Inspect() string { return "#<Typhoeus::Response>" }
func (v *TyphoeusResponse) Truthy() bool    { return true }
func (v *TyphoeusHydra) ToS() string        { return "#<Typhoeus::Hydra>" }
func (v *TyphoeusHydra) Inspect() string    { return "#<Typhoeus::Hydra>" }
func (v *TyphoeusHydra) Truthy() bool       { return true }

// typhoeusHashAt returns args[i] as an *object.Hash, or nil when out of range or
// not a Hash — the shape of Typhoeus's optional trailing options Hash.
func typhoeusHashAt(args []object.Value, i int) *object.Hash {
	if i < len(args) {
		if h, ok := args[i].(*object.Hash); ok {
			return h
		}
	}
	return nil
}

// typhoeusName coerces a :method value (a Symbol like :get or a String) to its
// plain, upper-case-ready string.
func typhoeusName(v object.Value) string {
	if s, ok := v.(*object.String); ok {
		return s.Str()
	}
	return v.ToS()
}

// rubyHashToTyphoeusOptions reads Typhoeus's option Hash into the library's
// Options: :params and :headers Hashes, :body (a String sent verbatim, a Hash
// form-encoded, else any JSON-ish value), :userpwd, :timeout (seconds),
// :followlocation and :maxredirs. An absent Hash yields the zero Options.
func rubyHashToTyphoeusOptions(h *object.Hash) typhoeus.Options {
	var o typhoeus.Options
	if h == nil {
		return o
	}
	if p := typhoeusKwHash(h, "params"); p != nil {
		o.Params = rubyHashToTyphoeusParams(p)
	}
	if hh := typhoeusKwHash(h, "headers"); hh != nil {
		o.Headers = rubyHashToTyphoeusHeaders(hh)
	}
	if v, ok := h.Get(object.Symbol("body")); ok {
		o.Body = typhoeusBody(v)
	}
	if v, ok := h.Get(object.Symbol("userpwd")); ok {
		o.UserPwd = v.ToS()
	}
	if v, ok := h.Get(object.Symbol("timeout")); ok {
		o.Timeout = time.Duration(toInt(v)) * time.Second
	}
	if v, ok := h.Get(object.Symbol("followlocation")); ok {
		o.FollowLocation = v.Truthy()
	}
	if v, ok := h.Get(object.Symbol("maxredirs")); ok {
		o.MaxRedirects = int(toInt(v))
	}
	return o
}

// typhoeusBody maps a Ruby :body value to the Go value the library encodes: a
// String verbatim, a Hash form-encoded as ordered Params, else a JSON-ish value.
func typhoeusBody(v object.Value) any {
	switch x := v.(type) {
	case *object.String:
		return x.Str()
	case *object.Hash:
		return rubyHashToTyphoeusParams(x)
	}
	return rubyToGoValue(v)
}

// typhoeusKwHash returns a keyword option as a Ruby Hash, or nil when absent or
// not a Hash.
func typhoeusKwHash(h *object.Hash, key string) *object.Hash {
	if v, ok := h.Get(object.Symbol(key)); ok {
		if hh, ok := v.(*object.Hash); ok {
			return hh
		}
	}
	return nil
}

// rubyHashToTyphoeusParams builds an ordered library Params (query / form) from a
// Ruby Hash, keeping insertion order and stringifying each key/value.
func rubyHashToTyphoeusParams(h *object.Hash) *typhoeus.Params {
	p := typhoeus.NewParams()
	for _, k := range h.Keys {
		v, _ := h.Get(k)
		p.Set(k.ToS(), v.ToS())
	}
	return p
}

// rubyHashToTyphoeusHeaders builds a case-insensitive library Headers from a Ruby
// Hash.
func rubyHashToTyphoeusHeaders(h *object.Hash) *typhoeus.Headers {
	hd := typhoeus.NewHeaders()
	for _, k := range h.Keys {
		v, _ := h.Get(k)
		hd.Set(k.ToS(), v.ToS())
	}
	return hd
}

// typhoeusHeadersHash renders a library Headers as a Ruby Hash of String→String,
// preserving the first-seen key casing and insertion order.
func typhoeusHeadersHash(h *typhoeus.Headers) object.Value {
	rh := object.NewHash()
	for _, p := range h.Pairs() {
		rh.Set(object.NewString(p.Key), object.NewString(p.Val))
	}
	return rh
}

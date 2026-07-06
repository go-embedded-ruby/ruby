// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	excon "github.com/go-ruby-excon/excon"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph (object.Value)
// and the interpreter-independent github.com/go-ruby-excon/excon library — the
// fast, persistent HTTP client (require "excon"). The library owns everything
// Excon does around the socket: merging per-request options over the connection
// defaults, building the absolute URL (path plus an ordered, CGI-escaped query),
// adding the Basic Authorization header, asserting the response status against
// :expects (raising the matching status error), retrying an :idempotent request,
// and the whole Excon::Error tree. Only the round-trip is a host seam (an
// excon.Doer); rbgo wires the library's net/http Doer through exconDoer so tests
// point it at an in-process httptest server. rbgo wraps each library object as a
// Ruby object reporting the matching Excon::* class (see excon.go for the class +
// method registration) and converts values across the boundary here.

// The wrapper types. Each holds a pointer into the library and reports the
// matching Excon::* class (see classOf); the methods registered in excon.go
// operate on the held value.

// ExconConnection wraps a persistent *excon.Connection (Excon::Connection).
type ExconConnection struct{ c *excon.Connection }

// ExconResponse wraps a finished *excon.Response (Excon::Response).
type ExconResponse struct{ r *excon.Response }

func (v *ExconConnection) ToS() string     { return "#<Excon::Connection>" }
func (v *ExconConnection) Inspect() string { return "#<Excon::Connection>" }
func (v *ExconConnection) Truthy() bool    { return true }
func (v *ExconResponse) ToS() string       { return "#<Excon::Response>" }
func (v *ExconResponse) Inspect() string   { return "#<Excon::Response>" }
func (v *ExconResponse) Truthy() bool      { return true }

// exconHashAt returns args[i] as an *object.Hash, or nil when out of range or not
// a Hash — the shape of Excon's optional trailing options Hash.
func exconHashAt(args []object.Value, i int) *object.Hash {
	if i < len(args) {
		if h, ok := args[i].(*object.Hash); ok {
			return h
		}
	}
	return nil
}

// rubyHashToExconOptions reads Excon's option Hash into the library's Options:
// :method/:path/:body strings, :query and :headers Hashes, :expects (an Integer or
// an Array of Integers), :idempotent, the retry and timeout Integers, and the
// :user/:password Basic credentials. An absent Hash yields the zero Options (every
// field unset, inheriting the connection defaults).
func rubyHashToExconOptions(h *object.Hash) excon.Options {
	var o excon.Options
	if h == nil {
		return o
	}
	if v, ok := h.Get(object.Symbol("method")); ok {
		o.Method = exconName(v)
	}
	if v, ok := h.Get(object.Symbol("path")); ok {
		o.Path = v.ToS()
	}
	if v, ok := h.Get(object.Symbol("body")); ok {
		o.Body = v.ToS()
	}
	if q := exconKwHash(h, "query"); q != nil {
		o.Query = rubyHashToExconQuery(q)
	}
	if hh := exconKwHash(h, "headers"); hh != nil {
		o.Headers = rubyHashToExconHeaders(hh)
	}
	if v, ok := h.Get(object.Symbol("expects")); ok {
		o.Expects = exconExpects(v)
	}
	if v, ok := h.Get(object.Symbol("idempotent")); ok {
		o.Idempotent = v.Truthy()
	}
	if v, ok := h.Get(object.Symbol("retry_limit")); ok {
		o.RetryLimit = int(toInt(v))
	}
	if v, ok := h.Get(object.Symbol("retry_interval")); ok {
		o.RetryInterval = int(toInt(v))
	}
	if v, ok := h.Get(object.Symbol("read_timeout")); ok {
		o.ReadTimeout = int(toInt(v))
	}
	if v, ok := h.Get(object.Symbol("write_timeout")); ok {
		o.WriteTimeout = int(toInt(v))
	}
	if v, ok := h.Get(object.Symbol("connect_timeout")); ok {
		o.ConnectTimeout = int(toInt(v))
	}
	if v, ok := h.Get(object.Symbol("user")); ok {
		o.User = v.ToS()
	}
	if v, ok := h.Get(object.Symbol("password")); ok {
		o.Password = v.ToS()
	}
	return o
}

// exconName coerces a :method value (a Symbol like :get or a String) to its plain
// string.
func exconName(v object.Value) string {
	if s, ok := v.(*object.String); ok {
		return s.Str()
	}
	return v.ToS()
}

// exconKwHash returns a keyword option as a Ruby Hash, or nil when absent or not a
// Hash.
func exconKwHash(h *object.Hash, key string) *object.Hash {
	if v, ok := h.Get(object.Symbol(key)); ok {
		if hh, ok := v.(*object.Hash); ok {
			return hh
		}
	}
	return nil
}

// exconExpects reads the :expects option — a single Integer status or an Array of
// them — into the []int the library asserts against.
func exconExpects(v object.Value) []int {
	if arr, ok := v.(*object.Array); ok {
		out := make([]int, 0, len(arr.Elems))
		for _, e := range arr.Elems {
			out = append(out, int(toInt(e)))
		}
		return out
	}
	return []int{int(toInt(v))}
}

// rubyHashToExconQuery builds an ordered library Query from a Ruby Hash, keeping
// insertion order and stringifying each key/value.
func rubyHashToExconQuery(h *object.Hash) *excon.Query {
	q := excon.NewQuery()
	for _, k := range h.Keys {
		v, _ := h.Get(k)
		q.Add(k.ToS(), v.ToS())
	}
	return q
}

// rubyHashToExconHeaders builds a case-insensitive library Headers from a Ruby
// Hash.
func rubyHashToExconHeaders(h *object.Hash) *excon.Headers {
	hd := excon.NewHeaders()
	for _, k := range h.Keys {
		v, _ := h.Get(k)
		hd.Set(k.ToS(), v.ToS())
	}
	return hd
}

// exconHeadersHash renders a library Headers as a Ruby Hash of String→String,
// preserving the first-seen key casing and insertion order.
func exconHeadersHash(h *excon.Headers) object.Value {
	rh := object.NewHash()
	for _, p := range h.Pairs() {
		rh.Set(object.NewString(p.Key), object.NewString(p.Val))
	}
	return rh
}

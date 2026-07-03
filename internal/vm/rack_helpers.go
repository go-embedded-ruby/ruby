// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"strconv"

	rack "github.com/go-ruby-rack/rack"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// rackStr coerces an argument to its String contents: a String yields its bytes,
// a Symbol its name, any other value its to_s.
func rackStr(v object.Value) string {
	switch n := v.(type) {
	case *object.String:
		return n.Str()
	case object.Symbol:
		return string(n)
	}
	return v.ToS()
}

// rackInt reads an argument as an int, falling back to def for a non-integer.
func rackInt(v object.Value, def int) int {
	switch n := v.(type) {
	case object.Integer:
		return int(n)
	case *object.String:
		if i, err := strconv.Atoi(n.Str()); err == nil {
			return i
		}
	}
	return def
}

// rackArg reads the single value argument of a method, defaulting to nil.
func rackArg(args []object.Value) object.Value {
	if len(args) == 0 {
		return object.NilV
	}
	return args[0]
}

// rackEnv converts a Ruby Hash into a rack.Env (map[string]any), stringifying
// keys and mapping scalar values into the generic Go value model rack consumes.
func rackEnv(v object.Value) rack.Env {
	h, ok := v.(*object.Hash)
	if !ok {
		raise("TypeError", "no implicit conversion into Hash")
	}
	env := make(rack.Env, len(h.Keys))
	for _, k := range h.Keys {
		val, _ := h.Get(k)
		env[rackStr(k)] = rackEnvValue(val)
	}
	return env
}

// rackEnvValue maps a Ruby env value into the Go value a rack.Env holds. Env
// entries are almost always strings; scalars are preserved and anything else is
// stringified so the request accessors always see a usable value.
func rackEnvValue(v object.Value) any {
	switch n := v.(type) {
	case nil, object.Nil:
		return nil
	case *object.String:
		return n.Str()
	case object.Symbol:
		return string(n)
	case object.Bool:
		return bool(n)
	case object.Integer:
		return int64(n)
	case object.Float:
		return float64(n)
	}
	return v.ToS()
}

// rackHeadersFrom builds a *rack.Headers from a Ruby Hash (a nil / non-Hash
// argument yields empty headers), stringifying keys and values.
func rackHeadersFrom(v object.Value) *rack.Headers {
	h := rack.NewHeaders()
	hash, ok := v.(*object.Hash)
	if !ok {
		return h
	}
	for _, k := range hash.Keys {
		val, _ := hash.Get(k)
		h.Set(rackStr(k), rackStr(val))
	}
	return h
}

// rackResponseBody reads the body argument of Rack::Response.new: a String is a
// one-part body, an Array is its parts (each stringified), and nil/absent is an
// empty body.
func rackResponseBody(args []object.Value) []string {
	if len(args) == 0 {
		return nil
	}
	switch n := args[0].(type) {
	case object.Nil:
		return nil
	case *object.String:
		return []string{n.Str()}
	case *object.Array:
		out := make([]string, len(n.Elems))
		for i, el := range n.Elems {
			out[i] = rackStr(el)
		}
		return out
	}
	return []string{rackStr(args[0])}
}

// rackBodyArray maps a []string response body into a Ruby Array of Strings.
func rackBodyArray(parts []string) *object.Array {
	arr := object.NewArrayFromSlice(make([]object.Value, len(parts)))
	for i, p := range parts {
		arr.Elems[i] = object.NewString(p)
	}
	return arr
}

// rackParamsToHash maps a *rack.Params into a Ruby Hash keyed by String, in
// insertion order. A nil Params yields an empty Hash.
func rackParamsToHash(p *rack.Params) *object.Hash {
	h := object.NewHash()
	if p == nil {
		return h
	}
	for _, k := range p.Keys() {
		val, _ := p.Get(k)
		h.Set(object.NewString(k), rackFromGo(val))
	}
	return h
}

// rackParamsFromHash builds a *rack.Params from a Ruby Hash (a non-Hash argument
// yields empty params), for Rack::Utils.build_query.
func rackParamsFromHash(v object.Value) *rack.Params {
	p := rack.NewParams()
	hash, ok := v.(*object.Hash)
	if !ok {
		return p
	}
	for _, k := range hash.Keys {
		val, _ := hash.Get(k)
		p.Set(rackStr(k), rackToGo(val))
	}
	return p
}

// rackHeadersToHash maps a *rack.Headers into a Ruby Hash keyed by String, in
// key order.
func rackHeadersToHash(h *rack.Headers) *object.Hash {
	out := object.NewHash()
	if h == nil {
		return out
	}
	for _, k := range h.Keys() {
		out.Set(object.NewString(k), rackFromGo(h.Get(k)))
	}
	return out
}

// rackFromGo maps a generic Go value (as held by rack Params/Headers) back into
// the rbgo object graph.
func rackFromGo(v any) object.Value {
	switch n := v.(type) {
	case nil:
		return object.NilV
	case string:
		return object.NewString(n)
	case bool:
		return object.Bool(n)
	case int:
		return object.IntValue(int64(n))
	case int64:
		return object.IntValue(n)
	case float64:
		return object.Float(n)
	case []any:
		arr := object.NewArrayFromSlice(make([]object.Value, len(n)))
		for i, el := range n {
			arr.Elems[i] = rackFromGo(el)
		}
		return arr
	case []string:
		arr := object.NewArrayFromSlice(make([]object.Value, len(n)))
		for i, el := range n {
			arr.Elems[i] = object.NewString(el)
		}
		return arr
	case map[string]any:
		h := object.NewHash()
		for k, val := range n {
			h.Set(object.NewString(k), rackFromGo(val))
		}
		return h
	case *rack.Params:
		return rackParamsToHash(n)
	}
	return object.NilV
}

// rackToGo maps a Ruby value into the generic Go value model rack consumes
// (nil / bool / int64 / float64 / string / []any / map[string]any).
func rackToGo(v object.Value) any {
	switch n := v.(type) {
	case nil, object.Nil:
		return nil
	case object.Bool:
		return bool(n)
	case object.Integer:
		return int64(n)
	case object.Float:
		return float64(n)
	case *object.String:
		return n.Str()
	case object.Symbol:
		return string(n)
	case *object.Array:
		out := make([]any, len(n.Elems))
		for i, el := range n.Elems {
			out[i] = rackToGo(el)
		}
		return out
	case *object.Hash:
		m := make(map[string]any, len(n.Keys))
		for _, k := range n.Keys {
			val, _ := n.Get(k)
			m[rackStr(k)] = rackToGo(val)
		}
		return m
	}
	return v.ToS()
}

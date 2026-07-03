// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"strconv"
	"strings"

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

// rackToGoNested maps a Ruby value into the *structural* Go model
// rack.BuildNestedQuery consumes: a Hash becomes an insertion-ordered
// *rack.Params, an Array a []any and a scalar its rackToGo form. build_query
// only needs the flat model (rackToGo), but build_nested_query recurses into
// sub-hashes and expects *rack.Params for them — a plain map[string]any would
// lose Ruby's Hash order (and buildNested only recognises *Params for hashes),
// so nested query building goes through this converter instead.
func rackToGoNested(v object.Value) any {
	switch n := v.(type) {
	case *object.Hash:
		p := rack.NewParams()
		for _, k := range n.Keys {
			val, _ := n.Get(k)
			p.Set(rackStr(k), rackToGoNested(val))
		}
		return p
	case *object.Array:
		out := make([]any, len(n.Elems))
		for i, el := range n.Elems {
			out[i] = rackToGoNested(el)
		}
		return out
	}
	return rackToGo(v)
}

// rackStrArray maps a Ruby Array into a []string (each element stringified),
// used for the `available` list of Rack::Utils.best_q_match. A non-Array
// argument yields nil.
func rackStrArray(v object.Value) []string {
	arr, ok := v.(*object.Array)
	if !ok {
		return nil
	}
	out := make([]string, len(arr.Elems))
	for i, el := range arr.Elems {
		out[i] = rackStr(el)
	}
	return out
}

// rackCookieHeader extracts the HTTP_COOKIE entry from a Ruby env Hash for
// Rack::Utils.parse_cookies(env), matching MRI which reads env['HTTP_COOKIE'].
// A non-Hash argument or an absent key yields the empty string (no cookies).
func rackCookieHeader(v object.Value) string {
	h, ok := v.(*object.Hash)
	if !ok {
		return ""
	}
	for _, k := range h.Keys {
		if rackStr(k) == "HTTP_COOKIE" {
			val, _ := h.Get(k)
			return rackStr(val)
		}
	}
	return ""
}

// rackFloat reads an argument as a float64, used for the quality weights of the
// [name, quality] pairs Rack::Utils.select_best_encoding accepts. An Integer
// coerces to its float value; anything non-numeric yields 0.
func rackFloat(v object.Value) float64 {
	switch n := v.(type) {
	case object.Float:
		return float64(n)
	case object.Integer:
		return float64(n)
	}
	return 0
}

// rackQValues maps a Ruby Array of [name, quality] pairs (the pre-parsed
// accept_encoding argument of Rack::Utils.select_best_encoding) into a
// []rack.QValue. A non-Array argument yields nil; elements that are not at least
// two-long arrays are skipped, mirroring MRI's `each { |m, q| }` destructuring.
func rackQValues(v object.Value) []rack.QValue {
	arr, ok := v.(*object.Array)
	if !ok {
		return nil
	}
	out := make([]rack.QValue, 0, len(arr.Elems))
	for _, el := range arr.Elems {
		pair, ok := el.(*object.Array)
		if !ok || len(pair.Elems) < 2 {
			continue
		}
		out = append(out, rack.QValue{Value: rackStr(pair.Elems[0]), Quality: rackFloat(pair.Elems[1])})
	}
	return out
}

// rackAllowedForwarded is the set of Forwarded-header parameters Rack::Utils
// accepts, mirroring rack.ForwardedValues' allow-list.
var rackAllowedForwarded = map[string]bool{"by": true, "for": true, "host": true, "proto": true}

// rackForwardedArg reads the single argument of Rack::Utils.forwarded_values and
// splits it into the (header, present) pair rack.ForwardedValues consumes. A
// falsy argument (nil / false) is absent (present=false), matching MRI's
// `return nil unless forwarded_header`; any other value is stringified (MRI
// applies #to_s before parsing).
func rackForwardedArg(v object.Value) (string, bool) {
	switch n := v.(type) {
	case nil, object.Nil:
		return "", false
	case object.Bool:
		if !bool(n) {
			return "", false
		}
	}
	return rackStr(v), true
}

// rackForwardedOrder returns the allowed Forwarded parameter names in
// first-appearance order, mirroring rack.ForwardedValues' tokeniser (names only)
// so the Ruby Hash reproduces MRI's header-order enumeration — the Go func
// returns an unordered map[string][]string, which alone cannot preserve order.
// It is only ever called after ForwardedValues reports present, so every name it
// yields is a key of that map.
func rackForwardedOrder(header string) []string {
	const seps = " \t;,"
	h := strings.TrimLeft(strings.ReplaceAll(header, "\n", ";"), seps)
	var order []string
	seen := map[string]bool{}
	for {
		eq := strings.IndexByte(h, '=')
		if eq < 0 {
			break
		}
		name := strings.ToLower(strings.TrimSpace(h[:eq]))
		h = h[eq+1:]
		if len(h) > 0 && h[0] == '"' {
			h = h[1:]
			for {
				i := strings.IndexAny(h, "\"\\")
				if i < 0 {
					h = ""
					break
				}
				c := h[i]
				h = h[i+1:]
				if c == '"' {
					break
				}
				if len(h) > 0 {
					h = h[1:]
				}
			}
		} else if i := strings.IndexAny(h, ";,"); i >= 0 {
			h = h[i:]
		} else {
			h = ""
		}
		if rackAllowedForwarded[name] && !seen[name] {
			seen[name] = true
			order = append(order, name)
		}
		h = strings.TrimLeft(h, seps)
	}
	return order
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

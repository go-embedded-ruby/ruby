// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"math/big"

	json "github.com/go-ruby-json/json"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph (object.Value)
// and the interpreter-independent value model of github.com/go-ruby-json/json.
// The parser and generator themselves live in that library (ported, byte-for-byte,
// from rbgo's former internal/vm/json.go); rbgo only translates its values to and
// from the library's `any` model around a single json.Parse / json.Generate /
// json.PrettyGenerate call, so the MRI-compatible behaviour Puppet's multi_json
// fallback relies on is preserved by construction. The library's typed errors are
// re-raised as the matching Ruby exception via their RubyClass().

// jsonParse parses a JSON document into a Ruby value by calling json.Parse with
// the given options and mapping the result back into the rbgo object graph. A
// malformed document raises JSON::ParserError; exceeding the nesting limit
// JSON::NestingError.
func jsonParse(src string, opts ...json.Option) object.Value {
	v, err := json.Parse(src, opts...)
	if err != nil {
		raiseJSONError(err)
	}
	return fromJSON(v)
}

// jsonGenerate renders a Ruby value to a compact JSON document via json.Generate,
// re-raising a non-finite-float / over-deep value as the matching Ruby exception.
func jsonGenerate(v object.Value, opts ...json.Option) string {
	out, err := json.Generate(toJSON(v), opts...)
	if err != nil {
		raiseJSONError(err)
	}
	return out
}

// jsonPrettyGenerate renders a Ruby value with MRI's JSON.pretty_generate layout.
func jsonPrettyGenerate(v object.Value, opts ...json.Option) string {
	out, err := json.PrettyGenerate(toJSON(v), opts...)
	if err != nil {
		raiseJSONError(err)
	}
	return out
}

// raiseJSONError re-raises a library error as the Ruby exception named by its
// RubyClass() (JSON::ParserError / JSON::NestingError / JSON::GeneratorError /
// TypeError). Every error Parse / Generate / PrettyGenerate returns is a
// json.Error, so the assertion is total.
func raiseJSONError(err error) {
	je := err.(json.Error)
	raise(je.RubyClass(), "%s", je.Error())
}

// --- rbgo value -> library value (for Generate) ----------------------------

// toJSON maps a Ruby value to the go-ruby-json value model. The JSON value
// shapes (nil / true / false / Integer / Bignum / Float / String / Symbol /
// Array / ordered Hash) all translate; any other value maps to its Ruby to_s so
// the library emits it as a JSON string, matching the former generator's
// to_s-of-unknown fallback (e.g. a Range serialises as "1..2").
func toJSON(v object.Value) json.Value {
	switch n := v.(type) {
	case nil:
		return nil
	case object.Nil:
		return nil
	case object.Bool:
		return bool(n)
	case object.Integer:
		return int64(n)
	case *object.Bignum:
		return n.I
	case object.Float:
		return float64(n)
	case *object.String:
		return n.Str()
	case object.Symbol:
		return json.Symbol(string(n))
	case *object.Array:
		out := make([]json.Value, len(n.Elems))
		for i, el := range n.Elems {
			out[i] = toJSON(el)
		}
		return out
	case *object.Hash:
		m := json.NewMap()
		for _, k := range n.Keys {
			val, _ := n.Get(k)
			m.Set(jsonKeyString(k), toJSON(val))
		}
		return m
	}
	// Any other value (a Range, an RObject, …): its Ruby to_s, emitted as a JSON
	// string. The former generator did the same via jsonStr(v.ToS()).
	return v.ToS()
}

// jsonKeyString renders a Hash key as the string JSON requires: a String keeps
// its contents, a Symbol its bare name, anything else its Ruby to_s (MRI coerces
// every JSON object key to a string).
func jsonKeyString(k object.Value) string {
	switch key := k.(type) {
	case *object.String:
		return key.Str()
	case object.Symbol:
		return string(key)
	}
	return k.ToS()
}

// --- library value -> rbgo value (for Parse) -------------------------------

// fromJSON maps a value produced by json.Parse back into the rbgo object graph:
// nil / bool / int64 / *big.Int / float64 / string / Symbol / []any / ordered
// *Map cover every shape the parser yields.
func fromJSON(v json.Value) object.Value {
	switch n := v.(type) {
	case nil:
		return object.NilV
	case bool:
		return object.Bool(n)
	case int64:
		return object.Integer(n)
	case *big.Int:
		return object.NormInt(n)
	case float64:
		return object.Float(n)
	case string:
		return object.NewString(n)
	case json.Symbol:
		return object.Symbol(string(n))
	case []json.Value:
		arr := &object.Array{Elems: make([]object.Value, len(n))}
		for i, el := range n {
			arr.Elems[i] = fromJSON(el)
		}
		return arr
	case *json.Map:
		h := object.NewHash()
		for _, p := range n.Pairs() {
			h.Set(fromJSON(p.Key), fromJSON(p.Val))
		}
		return h
	}
	// The parser only produces the cases above; any other value maps to nil
	// defensively.
	return object.NilV
}

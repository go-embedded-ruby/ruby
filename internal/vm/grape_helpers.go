// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"strings"

	grape "github.com/go-ruby-grape/grape"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// grapeStr coerces an argument to its String contents: a String yields its
// bytes, a Symbol its name, any other value its to_s.
func grapeStr(v object.Value) string {
	{
		__sw63 := v
		switch {
		case object.IsKind[*object.String](__sw63):
			n := object.Kind[*object.String](__sw63)
			_ = n
			return n.Str()
		case object.IsKind[object.Symbol](__sw63):
			n := object.Kind[object.Symbol](__sw63)
			_ = n
			return string(n)
		}
	}
	return v.ToS()
}

// grapeCheckFormatErr raises Grape::Exceptions::Base when a formatter call
// failed (an unknown format or a value the serialiser rejects), and is a no-op
// otherwise. It is the single error seam the json / xml / format methods share.
func grapeCheckFormatErr(err error) {
	if err != nil {
		raise("Grape::Exceptions::Base", "%s", err.Error())
	}
}

// grapeArg reads the single value argument of a formatter method, defaulting to
// nil when absent.
func grapeArg(args []object.Value) object.Value {
	if len(args) == 0 {
		return object.NilVal()
	}
	return args[0]
}

// grapeVerbName maps an HTTP method to its lower-case Ruby DSL method name.
func grapeVerbName(method string) string { return strings.ToLower(method) }

// grapeStatusName maps a grape.MatchStatus to its Ruby Symbol name.
func grapeStatusName(s grape.MatchStatus) string {
	switch s {
	case grape.StatusNotFound:
		return "not_found"
	case grape.StatusMethodNotAllowed:
		return "method_not_allowed"
	default:
		return "ok"
	}
}

// grapeOptions reads the trailing options Hash of a requires/optional line
// (returns nil when absent).
func grapeOptions(args []object.Value) *object.Hash {
	if len(args) > 1 {
		if h, ok := object.KindOK[*object.Hash](args[len(args)-1]); ok {
			return h
		}
	}
	return nil
}

// grapeBuildParam builds a grape.Param from a name, a required flag and the
// requires/optional options Hash: type:, values:, regexp:, default:, and the
// length bounds a Range-typed value carries.
func grapeBuildParam(name string, required bool, opts *object.Hash) *grape.Param {
	p := &grape.Param{Name: name, Required: required}
	if opts == nil {
		return p
	}
	for _, k := range opts.Keys {
		val, _ := opts.Get(k)
		switch grapeStr(k) {
		case "type":
			p.Type = grapeType(val)
		case "values":
			p.Values = grapeValueList(val)
		case "default":
			p.HasDefault = true
			p.Default = grapeToGo(val)
		case "regexp":
			p.Regexp = grapeRegexp(val)
		case "allow_blank":
			b := val.Truthy()
			p.AllowBlank = &b
		}
	}
	return p
}

// grapeType maps a Ruby type option (a class constant such as Integer/String, or
// a Symbol/String name) to a grape.Type. An unknown / absent type is "" (raw
// passthrough), matching Grape's untyped default.
func grapeType(v object.Value) grape.Type {
	name := grapeStr(v)
	switch name {
	case "Integer":
		return grape.TypeInteger
	case "Float":
		return grape.TypeFloat
	case "String":
		return grape.TypeString
	case "Boolean":
		return grape.TypeBoolean
	case "Date":
		return grape.TypeDate
	case "Time":
		return grape.TypeTime
	case "Array":
		return grape.TypeArray
	case "Hash":
		return grape.TypeHash
	}
	return grape.Type("")
}

// grapeValueList reads a values: option as a slice of allowed values: an Array
// of literals, or a single literal.
func grapeValueList(v object.Value) []any {
	if arr, ok := object.KindOK[*object.Array](v); ok {
		out := make([]any, len(arr.Elems))
		for i, el := range arr.Elems {
			out[i] = grapeToGo(el)
		}
		return out
	}
	return []any{grapeToGo(v)}
}

// grapeRegexp maps a regexp: option to a grape.Regexp whose Match is backed by
// the rbgo Regexp (so the pattern uses go-ruby-regexp, not a second engine). A
// non-Regexp value yields the zero Regexp (no constraint).
func grapeRegexp(v object.Value) grape.Regexp {
	re, ok := object.KindOK[*Regexp](v)
	if !ok {
		return grape.Regexp{}
	}
	return grape.Regexp{
		Source: re.source,
		Match:  func(s string) bool { return re.re.MatchString(s) },
	}
}

// grapeRawHash reads the raw params argument of Validator#validate into the
// map[string]any the validator consumes (keys stringified, values in the generic
// Go value model).
func grapeRawHash(v object.Value) map[string]any {
	h, ok := object.KindOK[*object.Hash](v)
	if !ok {
		raise("TypeError", "no implicit conversion into Hash")
	}
	m := make(map[string]any, len(h.Keys))
	for _, k := range h.Keys {
		val, _ := h.Get(k)
		m[grapeStr(k)] = grapeToGo(val)
	}
	return m
}

// grapeToGo maps a Ruby value into the generic Go value model grape consumes
// (nil / bool / int64 / float64 / string / []any / map[string]any).
func grapeToGo(v object.Value) any {
	{
		__sw64 := v
		switch {
		case object.IsNil(__sw64) || object.IsNilObj(__sw64):
			n := __sw64
			_ = n
			return nil
		case object.IsBool(__sw64):
			n := object.AsBoolV(__sw64)
			_ = n
			return bool(n)
		case object.IsInt(__sw64):
			n := object.AsInteger(__sw64)
			_ = n
			return int64(n)
		case object.IsKind[*object.Bignum](__sw64):
			n := object.Kind[*object.Bignum](__sw64)
			_ = n
			return n.I
		case object.IsFloat(__sw64):
			n := object.AsFloatV(__sw64)
			_ = n
			return float64(n)
		case object.IsKind[*object.String](__sw64):
			n := object.Kind[*object.String](__sw64)
			_ = n
			return n.Str()
		case object.IsKind[object.Symbol](__sw64):
			n := object.Kind[object.Symbol](__sw64)
			_ = n
			return string(n)
		case object.IsKind[*object.Array](__sw64):
			n := object.Kind[*object.Array](__sw64)
			_ = n
			out := make([]any, len(n.Elems))
			for i, el := range n.Elems {
				out[i] = grapeToGo(el)
			}
			return out
		case object.IsKind[*object.Hash](__sw64):
			n := object.Kind[*object.Hash](__sw64)
			_ = n
			m := make(map[string]any, len(n.Keys))
			for _, k := range n.Keys {
				val, _ := n.Get(k)
				m[grapeStr(k)] = grapeToGo(val)
			}
			return m
		}
	}
	return v.ToS()
}

// grapeCoercedToHash maps the coerced params map back into a Ruby Hash keyed by
// String parameter name, in declaration order (the ParamSet's order), so the
// result Hash mirrors the order the params were declared.
func grapeCoercedToHash(vm *VM, set *grape.ParamSet, m map[string]any) *object.Hash {
	h := object.NewHash()
	seen := make(map[string]bool, len(m))
	for _, p := range set.Params {
		if v, ok := m[p.Name]; ok {
			h.Set(object.Wrap(object.NewString(p.Name)), grapeFromGo(v))
			seen[p.Name] = true
		}
	}
	// Any coerced key not covered by a declaration (defensive) is appended.
	for k, v := range m {
		if !seen[k] {
			h.Set(object.Wrap(object.NewString(k)), grapeFromGo(v))
		}
	}
	return h
}

// grapeFromGo maps a coerced Go value back into the rbgo object graph.
func grapeFromGo(v any) object.Value {
	switch n := v.(type) {
	case nil:
		return object.NilVal()
	case bool:
		return object.BoolValue(bool(object.Bool(n)))
	case int:
		return object.IntValue(int64(n))
	case int64:
		return object.IntValue(n)
	case float64:
		return object.FloatValue(float64(object.Float(n)))
	case string:
		return object.Wrap(object.NewString(n))
	case []any:
		arr := object.NewArrayFromSlice(make([]object.Value, len(n)))
		for i, el := range n {
			arr.Elems[i] = grapeFromGo(el)
		}
		return object.Wrap(arr)
	case map[string]any:
		h := object.NewHash()
		for k, val := range n {
			h.Set(object.Wrap(object.NewString(k)), grapeFromGo(val))
		}
		return object.Wrap(h)
	}
	return object.NilVal()
}

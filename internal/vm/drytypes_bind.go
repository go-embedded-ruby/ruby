// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"math/big"
	"sort"
	"strings"
	stdtime "time"

	gotime "github.com/go-composites/time/src"
	drystruct "github.com/go-ruby-dry-struct/dry-struct"
	drytypes "github.com/go-ruby-dry-types/dry-types"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph and the
// interpreter-independent github.com/go-ruby-dry-types/dry-types library. The
// coercion, validation and combinators live in that library; rbgo maps a Ruby
// value into the library's `any` value model, drives a single Type.Call, and maps
// the coerced result (or the raised *CoercionError / *ConstraintError / schema
// error) back to a Ruby value or the matching Dry::Types exception.

// dryCall applies t to input, returning the coerced result on success and raising
// the mapped Dry::Types exception on failure so `type[input]` rescues as the gem's
// class.
func dryCall(vm *VM, t drytypes.Type, input object.Value) object.Value {
	out, err := t.Call(dryToGo(input))
	if err != nil {
		raise(dryErrorClass(err), "%s", err.Error())
	}
	return dryFromGo(vm, out)
}

// dryErrorClass maps a library coercion error to its Ruby class. A
// *ConstraintError is Dry::Types::ConstraintError, a schema/missing/unknown-key
// error Dry::Types::SchemaError, and any other coercion failure the base
// Dry::Types::CoercionError.
func dryErrorClass(err error) string {
	switch err.(type) {
	case *drytypes.ConstraintError:
		return "Dry::Types::ConstraintError"
	case *drytypes.SchemaError, *drytypes.MissingKeyError, *drytypes.UnknownKeysError:
		return "Dry::Types::SchemaError"
	}
	return "Dry::Types::CoercionError"
}

// dryLookup resolves a dotted type name ("strict.integer", "params.bool", …) to a
// DryType. An unknown name raises ArgumentError, matching the gem's registry miss.
func dryLookup(name string) object.Value {
	if fn, ok := dryRegistry[name]; ok {
		return object.Wrap(&DryType{t: fn()})
	}
	// A bare primitive name resolves to its strict type (the gem's default
	// namespace), so "integer" == "strict.integer".
	if !strings.Contains(name, ".") {
		if fn, ok := dryRegistry["strict."+name]; ok {
			return object.Wrap(&DryType{t: fn()})
		}
	}
	raise("ArgumentError", "Undefined type %q", name)
	return object.NilVal()
}

// dryRegistry maps every dotted type name to its library constructor, mirroring
// the dry-types container's built-in registrations.
var dryRegistry = map[string]func() drytypes.Type{
	"strict.integer":   drytypes.StrictInteger,
	"strict.float":     drytypes.StrictFloat,
	"strict.string":    drytypes.StrictString,
	"strict.symbol":    drytypes.StrictSymbol,
	"strict.bool":      drytypes.StrictBool,
	"strict.nil":       drytypes.StrictNil,
	"strict.array":     drytypes.StrictArray,
	"strict.hash":      drytypes.StrictHash,
	"strict.date":      drytypes.StrictDate,
	"strict.time":      drytypes.StrictTime,
	"strict.date_time": drytypes.StrictDateTime,

	"nominal.integer": drytypes.NominalInteger,
	"nominal.float":   drytypes.NominalFloat,
	"nominal.string":  drytypes.NominalString,
	"nominal.symbol":  drytypes.NominalSymbol,
	"nominal.bool":    drytypes.NominalBool,
	"nominal.array":   drytypes.NominalArray,
	"nominal.hash":    drytypes.NominalHash,

	"coercible.integer": drytypes.CoercibleInteger,
	"coercible.float":   drytypes.CoercibleFloat,
	"coercible.string":  drytypes.CoercibleString,
	"coercible.symbol":  drytypes.CoercibleSymbol,

	"params.integer":   drytypes.ParamsInteger,
	"params.float":     drytypes.ParamsFloat,
	"params.bool":      drytypes.ParamsBool,
	"params.nil":       drytypes.ParamsNil,
	"params.symbol":    drytypes.ParamsSymbol,
	"params.date":      drytypes.ParamsDate,
	"params.time":      drytypes.ParamsTime,
	"params.date_time": drytypes.ParamsDateTime,

	"json.date":      drytypes.JSONDate,
	"json.time":      drytypes.JSONTime,
	"json.date_time": drytypes.JSONDateTime,
	"json.symbol":    drytypes.JSONSymbol,
	"json.nil":       drytypes.JSONNil,
}

// dryConstraints maps the arguments of #constrained into library Constraints. Each
// argument is a Hash of { predicate => arg } (Symbol or String keys: :gt, :size,
// :format, …); non-Hash arguments are ignored.
func dryConstraints(args []object.Value) []drytypes.Constraint {
	var cs []drytypes.Constraint
	for _, a := range args {
		h, ok := object.KindOK[*object.Hash](a)
		if !ok {
			continue
		}
		for _, k := range h.Keys {
			v, _ := h.Get(k)
			cs = append(cs, drytypes.Constraint{Name: dryKeyName(k), Arg: dryToGo(v)})
		}
	}
	return cs
}

// drySchema builds a HashSchema from a Ruby Hash of { name => DryType }. A key
// name ending in "?" marks the member optional (the gem's `key?` schema syntax);
// a value that is not a DryType is skipped.
func drySchema(h *object.Hash) *drytypes.HashSchema {
	var keys []drytypes.SchemaKey
	for _, k := range h.Keys {
		v, _ := h.Get(k)
		t, ok := object.KindOK[*DryType](v)
		if !ok {
			continue
		}
		name := dryKeyName(k)
		opt := false
		if strings.HasSuffix(name, "?") {
			name = strings.TrimSuffix(name, "?")
			opt = true
		}
		keys = append(keys, drytypes.SchemaKey{Key: drytypes.Symbol(name), Type: t.t, Optional: opt})
	}
	return drytypes.NewSchema(keys...)
}

// dryMetaToHash maps a type's metadata (map[string]any) to a Ruby Hash with
// Symbol keys (the gem stores meta under Symbol keys), sorted for a deterministic
// order and with values mapped back through dryFromGo.
func dryMetaToHash(vm *VM, m map[string]any) object.Value {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := object.NewHash()
	for _, k := range keys {
		h.Set(object.SymVal(string(object.Symbol(k))), dryFromGo(vm, m[k]))
	}
	return object.Wrap(h)
}

// dryTypeName renders the Dry::Types[...] lookup argument as its bare name (a
// Symbol or String); any other value falls back to its to_s.
func dryTypeName(v object.Value) string {
	{
		__sw50 := v
		switch {
		case object.IsKind[object.Symbol](__sw50):
			n := object.Kind[object.Symbol](__sw50)
			_ = n
			return string(n)
		case object.IsKind[*object.String](__sw50):
			n := object.Kind[*object.String](__sw50)
			_ = n
			return n.Str()
		}
	}
	return v.ToS()
}

// dryKeyName renders a key (Symbol or String) as its bare name.
func dryKeyName(v object.Value) string {
	{
		__sw51 := v
		switch {
		case object.IsKind[object.Symbol](__sw51):
			n := object.Kind[object.Symbol](__sw51)
			_ = n
			return string(n)
		case object.IsKind[*object.String](__sw51):
			n := object.Kind[*object.String](__sw51)
			_ = n
			return n.Str()
		}
	}
	return v.ToS()
}

// dryToGo maps a Ruby value into the dry-types `any` value model. Scalars map to
// their Go-typed counterparts, a Symbol to drytypes.Symbol, Array / Hash recurse
// (a Hash becoming an ordered *drytypes.Map so key order round-trips), and a Time
// to a Go time.Time. Undefined is modelled by a nil object (the absent input the
// gem's `.default` substitutes for); any other value passes through as-is so the
// library reports the coercion failure.
func dryToGo(v object.Value) any {
	{
		__sw52 := v
		switch {
		case object.IsNil(__sw52):
			// Ruby nil maps to Go nil. (Pre-flip a Go-nil object.Value additionally
			// meant drytypes.Undefined; a tagged-struct Value can never be Go-nil, so
			// that sentinel is unreachable from the Ruby surface — Undefined now only
			// ever originates on the Go side. See TestDryTypesDefaultFn.)
			return nil
		case object.IsBool(__sw52):
			n := object.AsBoolV(__sw52)
			_ = n
			return bool(n)
		case object.IsInt(__sw52):
			n := object.AsInteger(__sw52)
			_ = n
			return int64(n)
		case object.IsKind[*object.Bignum](__sw52):
			n := object.Kind[*object.Bignum](__sw52)
			_ = n
			return n.I
		case object.IsFloat(__sw52):
			n := object.AsFloatV(__sw52)
			_ = n
			return float64(n)
		case object.IsKind[*object.String](__sw52):
			n := object.Kind[*object.String](__sw52)
			_ = n
			return n.Str()
		case object.IsKind[object.Symbol](__sw52):
			n := object.Kind[object.Symbol](__sw52)
			_ = n
			return drytypes.Symbol(string(n))
		case object.IsKind[*object.Array](__sw52):
			n := object.Kind[*object.Array](__sw52)
			_ = n
			out := make([]any, len(n.Elems))
			for i, el := range n.Elems {
				out[i] = dryToGo(el)
			}
			return out
		case object.IsKind[*object.Hash](__sw52):
			n := object.Kind[*object.Hash](__sw52)
			_ = n
			m := drytypes.NewMap()
			for _, k := range n.Keys {
				val, _ := n.Get(k)
				m.Set(dryToGo(k), dryToGo(val))
			}
			return m
		case object.IsKind[*Time](__sw52):
			n := object.Kind[*Time](__sw52)
			_ = n
			return stdtime.Unix(n.t.ToUnix(), 0).UTC()
		}
	}
	return v
}

// dryFromGo maps a value produced by a dry-types coercion back into the rbgo
// object graph: the scalar Go types, drytypes.Symbol, an ordered *drytypes.Map,
// []any, a big.Int, a Date/Time, and Undefined (mapped to nil).
func dryFromGo(vm *VM, v any) object.Value {
	switch n := v.(type) {
	case nil:
		return object.NilVal()
	case bool:
		return object.BoolValue(bool(object.Bool(n)))
	case int:
		return object.IntValue(int64(n))
	case int64:
		return object.IntValue(n)
	case *big.Int:
		return object.NormInt(n)
	case float64:
		return object.FloatValue(float64(object.Float(n)))
	case string:
		return object.Wrap(object.NewString(n))
	case drytypes.Symbol:
		return object.SymVal(string(object.Symbol(string(n))))
	case []any:
		arr := object.NewArrayFromSlice(make([]object.Value, len(n)))
		for i, el := range n {
			arr.Elems[i] = dryFromGo(vm, el)
		}
		return object.Wrap(arr)
	case *drytypes.Map:
		h := object.NewHash()
		for _, p := range n.Pairs() {
			h.Set(dryFromGo(vm, p.Key), dryFromGo(vm, p.Val))
		}
		return object.Wrap(h)
	case drytypes.Date:
		return object.Wrap(object.NewString(n.String()))
	case stdtime.Time:
		return object.Wrap(&Time{t: gotime.FromUnix(n.Unix())})
	case *drystruct.Struct:
		// A nested struct value: wrap it as a DryStruct reporting the Ruby subclass
		// named by its StructType (registered when the subclass was defined).
		cls, _ := object.KindOK[*RClass](vm.consts[n.Type().Name])
		return object.Wrap(&DryStruct{s: n, cls: cls})
	}
	if v == drytypes.Undefined {
		return object.NilVal()
	}
	return object.NilVal()
}

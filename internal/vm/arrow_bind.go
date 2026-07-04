// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"math"
	"math/big"
	"sort"
	stdtime "time"

	gotime "github.com/go-composites/time/src"
	libarrow "github.com/go-ruby-arrow/arrow"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin value bridge between rbgo's Ruby object graph
// (object.Value and the VM's Time shell) and the interpreter-independent value
// model of github.com/go-ruby-arrow/arrow — the pure-Go (CGO=0) port of Ruby's
// red-arrow gem. The columnar format, the typed builders, the schema/record-batch
// machinery and the Arrow IPC (Feather / stream) serialization all live in that
// library; this file only translates Ruby scalars to and from the library's Go
// `any` element model, and resolves a Ruby type spec (a Symbol like :int64 or an
// Arrow::DataType) onto a library DataType. See arrow.go for the class surface.
//
// Scalar mapping (Ruby <-> Arrow element):
//
//	nil            <-> null
//	true / false   <-> Boolean
//	Integer/Bignum <-> Int8..Int64 / UInt8..UInt64 (width per column type)
//	Float          <-> Float32 / Float64
//	String/Symbol  <-> Utf8 (a String reads back; a Symbol is accepted on input)
//	Time           <-> Timestamp(us) / Date32 (second precision, UTC — see below)
//	Array          <-> List
//	Hash           <-> Struct (string keys)
//	String (dec)   <-  Decimal128 (rendered as a decimal String on read)
//
// Time note: rbgo's Time carries second precision (go-composites/time), so a
// Timestamp/Date32 read back materialises a UTC second-resolution Ruby Time; the
// sub-second part of an externally-produced Arrow timestamp is dropped at the
// boundary. Every other common type round-trips exactly.

// arrowValueToScalar maps a Ruby value onto the go `any` element the library's
// typed builders accept. A nil/Nil appends a null on any column. An unmappable
// value raises a Ruby TypeError, matching red-arrow's rejection of a foreign
// element.
func arrowValueToScalar(v object.Value) any {
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
		if n.I.IsInt64() {
			return n.I.Int64()
		}
		if n.I.IsUint64() {
			return n.I.Uint64()
		}
		raise("TypeError", "Integer %s is out of range for an Arrow column", n.I.String())
	case object.Float:
		return float64(n)
	case *object.String:
		return n.Str()
	case object.Symbol:
		return string(n)
	case *Time:
		return stdtime.Unix(n.t.ToUnix(), 0).UTC()
	case *object.Array:
		out := make([]any, len(n.Elems))
		for i, el := range n.Elems {
			out[i] = arrowValueToScalar(el)
		}
		return out
	case *object.Hash:
		m := make(map[string]any, n.Len())
		for _, k := range n.Keys {
			val, _ := n.Get(k)
			m[arrowKeyName(k)] = arrowValueToScalar(val)
		}
		return m
	}
	raise("TypeError", "cannot store a %s in an Arrow column", v.Inspect())
	panic("unreachable")
}

// arrowScalarToRuby maps a go `any` element read back from the library into the
// rbgo object graph. Nulls read back as nil. Integers narrow to Ruby Integer
// (a >int64 UInt64 promotes to a Bignum), a Timestamp/Date32 becomes a UTC Ruby
// Time, a List becomes a Ruby Array and a Struct a Ruby Hash with string keys.
func arrowScalarToRuby(v any) object.Value {
	switch n := v.(type) {
	case nil:
		return object.NilV
	case bool:
		return object.Bool(n)
	case int8:
		return object.IntValue(int64(n))
	case int16:
		return object.IntValue(int64(n))
	case int32:
		return object.IntValue(int64(n))
	case int64:
		return object.IntValue(n)
	case int:
		return object.IntValue(int64(n))
	case uint8:
		return object.IntValue(int64(n))
	case uint16:
		return object.IntValue(int64(n))
	case uint32:
		return object.IntValue(int64(n))
	case uint64:
		if n <= math.MaxInt64 {
			return object.IntValue(int64(n))
		}
		return object.NormInt(new(big.Int).SetUint64(n))
	case uint:
		if uint64(n) <= math.MaxInt64 {
			return object.IntValue(int64(n))
		}
		return object.NormInt(new(big.Int).SetUint64(uint64(n)))
	case float32:
		return object.Float(float64(n))
	case float64:
		return object.Float(n)
	case string:
		return object.NewString(n)
	case stdtime.Time:
		return &Time{t: gotime.FromUnix(n.Unix())}
	case []any:
		out := make([]object.Value, len(n))
		for i, el := range n {
			out[i] = arrowScalarToRuby(el)
		}
		return object.NewArrayFromSlice(out)
	case map[string]any:
		h := object.NewHash()
		for _, key := range structKeyOrder(n) {
			h.Set(object.NewString(key), arrowScalarToRuby(n[key]))
		}
		return h
	}
	// The library's getValue only ever produces the cases above; a value it
	// cannot type it stringifies, which lands on the string case. This defensive
	// tail keeps the mapping total.
	return object.NilV
}

// structKeyOrder returns a struct map's keys in a stable (sorted) order so a
// Struct element renders deterministically regardless of Go map iteration order.
func structKeyOrder(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// arrowKeyName renders a Ruby Hash key (String or Symbol) as a column/field
// name. A non-String/Symbol key raises TypeError, matching red-arrow's requirement
// that a column name be a String or Symbol.
func arrowKeyName(k object.Value) string {
	switch key := k.(type) {
	case *object.String:
		return key.Str()
	case object.Symbol:
		return string(key)
	}
	raise("TypeError", "Arrow column name must be a String or Symbol, got %s", k.Inspect())
	panic("unreachable")
}

// arrowValuesFromArray maps a Ruby Array argument into a []any of library
// elements, raising TypeError for a non-Array argument.
func arrowValuesFromArray(v object.Value) []any {
	arr, ok := v.(*object.Array)
	if !ok {
		raise("TypeError", "expected an Array of values")
	}
	out := make([]any, len(arr.Elems))
	for i, el := range arr.Elems {
		out[i] = arrowValueToScalar(el)
	}
	return out
}

// arrowTypeSpecs maps a Ruby type-name Symbol/String onto a library DataType
// constructor for the primitive (non-parameterised) Arrow types. The
// parameterised types (Decimal128, List, Struct) are built through the
// Arrow::DataType class methods, not this table.
var arrowTypeSpecs = map[string]func() *libarrow.DataType{
	"int8":      libarrow.Int8,
	"int16":     libarrow.Int16,
	"int32":     libarrow.Int32,
	"int64":     libarrow.Int64,
	"uint8":     libarrow.UInt8,
	"uint16":    libarrow.UInt16,
	"uint32":    libarrow.UInt32,
	"uint64":    libarrow.UInt64,
	"float":     libarrow.Float,
	"float32":   libarrow.Float32,
	"float64":   libarrow.Float64,
	"double":    libarrow.Float64,
	"boolean":   libarrow.Boolean,
	"bool":      libarrow.Boolean,
	"string":    libarrow.StringType,
	"utf8":      libarrow.StringType,
	"timestamp": libarrow.Timestamp,
	"time":      libarrow.Timestamp,
	"date":      libarrow.Date,
	"date32":    libarrow.Date,
}

// arrowTypeFromSpec resolves a Ruby type spec into a library DataType. The spec
// is either an Arrow::DataType wrapper (used verbatim) or a Symbol/String naming
// a primitive type (:int64, :string, :boolean, …). An unknown name raises
// ArgumentError; a non-type value raises TypeError.
func arrowTypeFromSpec(v object.Value) *libarrow.DataType {
	switch spec := v.(type) {
	case *ArrowDataType:
		return spec.dt
	case object.Symbol:
		return arrowTypeByName(string(spec))
	case *object.String:
		return arrowTypeByName(spec.Str())
	}
	raise("TypeError", "expected an Arrow::DataType or a type name Symbol, got %s", v.Inspect())
	panic("unreachable")
}

func arrowTypeByName(name string) *libarrow.DataType {
	if ctor, ok := arrowTypeSpecs[name]; ok {
		return ctor()
	}
	raise("ArgumentError", "unknown Arrow data type %q", name)
	panic("unreachable")
}

// raiseArrowErr re-raises a library error as the faithful Ruby exception. The
// library tags each error with an ErrorKind whose RubyClass() names the Ruby
// class red-arrow raises (TypeError / IndexError / ArgumentError /
// NotImplementedError / Arrow::Error::Io / Arrow::Error). A non-arrow error
// falls back to RuntimeError. It never returns when err is non-nil.
func raiseArrowErr(err error) {
	if err == nil {
		return
	}
	var ae *libarrow.Error
	if errors.As(err, &ae) {
		raise(ae.RubyClass(), "%s", err.Error())
	}
	raise("RuntimeError", "%s", err.Error())
}

// arrowArrayOK wraps an (*Array, error) library call: it re-raises any error as
// the matching Ruby exception, then returns the wrapped Ruby value.
func arrowArrayOK(a *libarrow.Array, err error) object.Value {
	raiseArrowErr(err)
	return &ArrowArray{a: a}
}

// arrowTableOK wraps a (*Table, error) library call.
func arrowTableOK(t *libarrow.Table, err error) object.Value {
	raiseArrowErr(err)
	return &ArrowTable{t: t}
}

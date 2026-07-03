// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"math/big"
	stdtime "time"

	gotime "github.com/go-composites/time/src"
	toml "github.com/go-ruby-toml/toml"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph (object.Value
// and the VM's Time shell) and the interpreter-independent value model of
// github.com/go-ruby-toml/toml. The parser and generator themselves live in that
// library; rbgo only translates its values to and from the library's `any` model
// around a single toml.Parse / toml.Dump call, so the toml-rb-faithful behaviour
// the TOML module relies on is preserved by construction.

// tomlParse parses a TOML document string into a Ruby Hash by calling toml.Parse
// and mapping the result back into the rbgo object graph. A malformed document
// raises a Ruby ArgumentError carrying the library's message (toml-rb raises a
// TomlRB::ParseError, an ArgumentError-family error).
func tomlParse(vm *VM, src string) object.Value {
	m, err := toml.Parse(src)
	if err != nil {
		raise("ArgumentError", "%s", err.Error())
	}
	return fromTOMLMap(vm, m)
}

// tomlDump renders a Ruby value (a Hash at the root) to a TOML document by
// mapping it into the library value model and calling toml.Dump. A value with no
// TOML representation raises a Ruby ArgumentError, matching toml-rb's contract.
func tomlDump(v object.Value) string {
	out, err := toml.Dump(toTOML(v))
	if err != nil {
		raise("ArgumentError", "%s", err.Error())
	}
	return out
}

// --- rbgo value -> library value (for Dump) --------------------------------

// toTOML maps a Ruby value to the go-ruby-toml value model. A Symbol maps to its
// name (TOML has string keys/values only), a Time to a Go time.Time (the library
// emits it as an offset date-time), and Array / Hash recurse. An unmapped value
// is returned as-is so the library raises the dump error tomlDump turns into a
// Ruby ArgumentError.
func toTOML(v object.Value) toml.Value {
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
		return string(n)
	case *object.Array:
		out := make([]toml.Value, len(n.Elems))
		for i, el := range n.Elems {
			out[i] = toTOML(el)
		}
		return out
	case *object.Hash:
		m := toml.NewMap()
		for _, k := range n.Keys {
			val, _ := n.Get(k)
			m.Set(tomlKey(k), toTOML(val))
		}
		return m
	case *Time:
		return stdtime.Unix(n.t.ToUnix(), 0).UTC()
	}
	// An unmapped value: hand it to the library, which returns the dump error
	// tomlDump turns into a Ruby ArgumentError.
	return v
}

// tomlKey renders a Ruby Hash key as a TOML table key: a Symbol by its name, any
// other value by its to_s (TOML keys are strings).
func tomlKey(k object.Value) string {
	switch n := k.(type) {
	case object.Symbol:
		return string(n)
	case *object.String:
		return n.Str()
	}
	return k.ToS()
}

// --- library value -> rbgo value (for Parse) -------------------------------

// fromTOML maps a value produced by toml.Parse back into the rbgo object graph.
// The four TOML datetime shapes collapse onto a Ruby Time (the offset-less
// variants materialise as UTC, the host applying no further zone guess), and
// *Map / []any recurse.
func fromTOML(vm *VM, v toml.Value) object.Value {
	switch n := v.(type) {
	case nil:
		return object.NilV
	case bool:
		return object.Bool(n)
	case int64:
		return object.IntValue(n)
	case *big.Int:
		return object.NormInt(n)
	case float64:
		return object.Float(n)
	case string:
		return object.NewString(n)
	case []toml.Value:
		arr := object.NewArrayFromSlice(make([]object.Value, len(n)))
		for i, el := range n {
			arr.Elems[i] = fromTOML(vm, el)
		}
		return arr
	case *toml.Map:
		return fromTOMLMap(vm, n)
	case toml.OffsetDateTime:
		return &Time{t: gotime.FromUnix(n.Time.Unix())}
	case toml.LocalDateTime:
		t := stdtime.Date(n.Year, stdtime.Month(n.Month), n.Day, n.Hour, n.Minute, n.Second, n.Nanosecond, stdtime.UTC)
		return &Time{t: gotime.FromUnix(t.Unix())}
	case toml.LocalDate:
		t := stdtime.Date(n.Year, stdtime.Month(n.Month), n.Day, 0, 0, 0, 0, stdtime.UTC)
		return &Time{t: gotime.FromUnix(t.Unix())}
	case toml.LocalTime:
		t := stdtime.Date(1970, 1, 1, n.Hour, n.Minute, n.Second, n.Nanosecond, stdtime.UTC)
		return &Time{t: gotime.FromUnix(t.Unix())}
	}
	// The parser only ever produces the cases above; anything else is nil.
	return object.NilV
}

// fromTOMLMap maps a library ordered *Map to a Ruby Hash with String keys,
// preserving insertion order.
func fromTOMLMap(vm *VM, m *toml.Map) object.Value {
	h := object.NewHash()
	for _, p := range m.Pairs() {
		h.Set(object.NewString(p.Key), fromTOML(vm, p.Val))
	}
	return h
}

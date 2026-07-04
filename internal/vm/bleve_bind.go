// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"math/big"
	stdtime "time"

	gotime "github.com/go-composites/time/src"
	libbleve "github.com/go-ruby-bleve/bleve"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin value bridge between rbgo's Ruby object graph
// (object.Value and the VM's Time shell) and the interpreter-independent value
// model of github.com/go-ruby-bleve/bleve — the pure-Go (CGO=0) full-text search
// library over blevesearch/bleve/v2. The index format, query DSL and search
// machinery all live in that library; this file only translates Ruby Hash-shaped
// documents into the map[string]interface{} bleve indexes, maps stored result
// fields back into Ruby values, parses the Ruby search keyword options into the
// library's functional SearchOption values, and re-raises the library's error
// tree as the Bleve::Error exceptions. See bleve.go for the class surface.
//
// Document scalar mapping (Ruby -> bleve element):
//
//	nil            -> nil
//	true / false   -> bool
//	Integer/Bignum -> float64  (bleve's numeric fields are float64)
//	Float          -> float64
//	String/Symbol  -> string
//	Time           -> time.Time (second precision, UTC)
//	Array          -> []interface{}
//	Hash           -> map[string]interface{} (nested document, String keys)
//
// Reading a stored field back, bleve yields string / float64 / bool and slices
// of them (numbers always widen to float64), which bleveFieldToValue maps to the
// Ruby String / Float / Bool / Array counterparts.

// bleveDoc converts a Ruby Hash document into the map[string]interface{} the
// library indexes. A non-Hash argument raises TypeError, matching a search
// library's rejection of a non-document.
func bleveDoc(v object.Value) map[string]interface{} {
	h, ok := v.(*object.Hash)
	if !ok {
		raise("TypeError", "Bleve document must be a Hash, got %s", v.Inspect())
	}
	return bleveHashToMap(h)
}

// bleveHashToMap converts a Ruby Hash into a Go string-keyed map of bleve
// elements (used for the top-level document and any nested Hash value).
func bleveHashToMap(h *object.Hash) map[string]interface{} {
	m := make(map[string]interface{}, h.Len())
	for _, k := range h.Keys {
		val, _ := h.Get(k)
		m[bleveKeyName(k)] = bleveValueToGo(val)
	}
	return m
}

// bleveValueToGo maps a Ruby value onto the Go element bleve stores/indexes. An
// unmappable value raises a Ruby TypeError.
func bleveValueToGo(v object.Value) interface{} {
	switch n := v.(type) {
	case nil:
		return nil
	case object.Nil:
		return nil
	case object.Bool:
		return bool(n)
	case object.Integer:
		return float64(int64(n))
	case *object.Bignum:
		f, _ := new(big.Float).SetInt(n.I).Float64()
		return f
	case object.Float:
		return float64(n)
	case *object.String:
		return n.Str()
	case object.Symbol:
		return string(n)
	case *Time:
		return stdtime.Unix(n.t.ToUnix(), 0).UTC()
	case *object.Array:
		out := make([]interface{}, len(n.Elems))
		for i, el := range n.Elems {
			out[i] = bleveValueToGo(el)
		}
		return out
	case *object.Hash:
		return bleveHashToMap(n)
	}
	raise("TypeError", "cannot index a %s in a Bleve document", v.Inspect())
	panic("unreachable")
}

// bleveFieldsToHash renders a stored-field map (as returned on a Hit or by
// Index#document) into a Ruby Hash of String keys to Ruby values.
func bleveFieldsToHash(fields map[string]interface{}) object.Value {
	h := object.NewHash()
	for k, val := range fields {
		h.Set(object.NewString(k), bleveFieldToValue(val))
	}
	return h
}

// bleveFieldToValue maps a Go stored-field element read back from bleve into the
// rbgo object graph. bleve stores text as string, numbers as float64, booleans
// as bool, and multi-valued fields as a slice; the defensive tail keeps the
// mapping total for any element type a future bleve version might yield.
func bleveFieldToValue(v interface{}) object.Value {
	switch n := v.(type) {
	case nil:
		return object.NilV
	case bool:
		return object.Bool(n)
	case float64:
		return object.Float(n)
	case string:
		return object.NewString(n)
	case stdtime.Time:
		return &Time{t: gotime.FromUnix(n.Unix())}
	case []interface{}:
		out := make([]object.Value, len(n))
		for i, el := range n {
			out[i] = bleveFieldToValue(el)
		}
		return object.NewArrayFromSlice(out)
	}
	return object.NilV
}

// bleveFragmentsToHash renders a Hit's per-field highlighted fragments into a
// Ruby Hash of String field name to an Array of fragment Strings.
func bleveFragmentsToHash(frags map[string][]string) object.Value {
	h := object.NewHash()
	for field, parts := range frags {
		out := make([]object.Value, len(parts))
		for i, p := range parts {
			out[i] = object.NewString(p)
		}
		h.Set(object.NewString(field), object.NewArrayFromSlice(out))
	}
	return h
}

// bleveFacetResultToHash renders a single computed facet into a Ruby Hash with
// the aggregate counts and the term / numeric-range / date-range buckets, each a
// small Hash, so a facet reads back as plain Ruby data.
func bleveFacetResultToHash(fr *libbleve.FacetResult) object.Value {
	h := object.NewHash()
	h.Set(object.NewString("field"), object.NewString(fr.Field))
	h.Set(object.NewString("total"), object.IntValue(int64(fr.Total)))
	h.Set(object.NewString("missing"), object.IntValue(int64(fr.Missing)))
	h.Set(object.NewString("other"), object.IntValue(int64(fr.Other)))
	h.Set(object.NewString("terms"), bleveTermBuckets(fr.Terms))
	h.Set(object.NewString("numeric_ranges"), bleveRangeBuckets(fr.NumericRanges))
	h.Set(object.NewString("date_ranges"), bleveRangeBuckets(fr.DateRanges))
	return h
}

// bleveTermBuckets renders a facet's term buckets as an Array of {term, count}.
func bleveTermBuckets(terms []libbleve.TermCount) object.Value {
	out := make([]object.Value, len(terms))
	for i, t := range terms {
		b := object.NewHash()
		b.Set(object.NewString("term"), object.NewString(t.Term))
		b.Set(object.NewString("count"), object.IntValue(int64(t.Count)))
		out[i] = b
	}
	return object.NewArrayFromSlice(out)
}

// bleveRangeBuckets renders a facet's numeric- or date-range buckets as an Array
// of {name, count}.
func bleveRangeBuckets(ranges []libbleve.RangeCount) object.Value {
	out := make([]object.Value, len(ranges))
	for i, r := range ranges {
		b := object.NewHash()
		b.Set(object.NewString("name"), object.NewString(r.Name))
		b.Set(object.NewString("count"), object.IntValue(int64(r.Count)))
		out[i] = b
	}
	return object.NewArrayFromSlice(out)
}

// bleveKeyName renders a Ruby Hash key (String or Symbol) as a field name. A
// non-String/Symbol key raises TypeError.
func bleveKeyName(k object.Value) string {
	switch key := k.(type) {
	case *object.String:
		return key.Str()
	case object.Symbol:
		return string(key)
	}
	raise("TypeError", "Bleve field name must be a String or Symbol, got %s", k.Inspect())
	panic("unreachable")
}

// bleveMappingArg resolves an optional Bleve::Mapping argument at position i. A
// missing or nil argument selects the library's default dynamic mapping; any
// other type raises TypeError.
func bleveMappingArg(args []object.Value, i int) *libbleve.Mapping {
	if i >= len(args) || object.IsNil(args[i]) {
		return nil
	}
	m, ok := args[i].(*BleveMapping)
	if !ok {
		raise("TypeError", "expected a Bleve::Mapping, got %s", args[i].Inspect())
	}
	return m.m
}

// bleveQueryArg resolves a search query argument: a Bleve::Query is used as-is,
// a String is parsed through bleve's query-string mini-language, and anything
// else raises TypeError.
func bleveQueryArg(v object.Value) libbleve.Query {
	switch q := v.(type) {
	case *BleveQuery:
		return q.q
	case *object.String:
		return libbleve.QueryString(q.Str())
	}
	raise("TypeError", "expected a Bleve::Query or a String, got %s", v.Inspect())
	panic("unreachable")
}

// bleveSearchOpts turns the trailing keyword Hash of Bleve::Index#search into the
// library's functional SearchOption values: size:, from:, fields:, highlight:,
// highlight_style:, sort: and facets:.
func bleveSearchOpts(rest []object.Value) []libbleve.SearchOption {
	h := bleveOptsHash(rest)
	if h == nil {
		return nil
	}
	var opts []libbleve.SearchOption
	if v, ok := h.Get(object.Symbol("size")); ok {
		opts = append(opts, libbleve.Size(int(intArg(v))))
	}
	if v, ok := h.Get(object.Symbol("from")); ok {
		opts = append(opts, libbleve.From(int(intArg(v))))
	}
	if v, ok := h.Get(object.Symbol("fields")); ok {
		opts = append(opts, libbleve.Fields(bleveStringList(v)...))
	}
	if v, ok := h.Get(object.Symbol("highlight")); ok && v.Truthy() {
		opts = append(opts, libbleve.Highlight())
	}
	if v, ok := h.Get(object.Symbol("highlight_style")); ok {
		opts = append(opts, libbleve.HighlightStyle(strArg(v)))
	}
	if v, ok := h.Get(object.Symbol("sort")); ok {
		opts = append(opts, libbleve.SortBy(bleveStringList(v)...))
	}
	if v, ok := h.Get(object.Symbol("facets")); ok {
		fh, fok := v.(*object.Hash)
		if !fok {
			raise("TypeError", "facets: must be a Hash of name => Bleve::Facet")
		}
		for _, k := range fh.Keys {
			fv, _ := fh.Get(k)
			f, ok := fv.(*BleveFacet)
			if !ok {
				raise("TypeError", "facets: value must be a Bleve::Facet, got %s", fv.Inspect())
			}
			opts = append(opts, libbleve.WithFacet(bleveKeyName(k), f.f))
		}
	}
	return opts
}

// bleveStringList coerces a search option that is either a single String or an
// Array of Strings into a []string, raising TypeError otherwise.
func bleveStringList(v object.Value) []string {
	switch x := v.(type) {
	case *object.String:
		return []string{x.Str()}
	case *object.Array:
		out := make([]string, len(x.Elems))
		for i, e := range x.Elems {
			out[i] = strArg(e)
		}
		return out
	}
	raise("TypeError", "expected a String or an Array of Strings, got %s", v.Inspect())
	panic("unreachable")
}

// bleveOptsHash returns the trailing keyword Hash of a Bleve entry point (the
// search options or the Bleve::Query.bool must:/should:/must_not: Arrays), or nil
// when the last argument is not a Hash.
func bleveOptsHash(rest []object.Value) *object.Hash {
	if len(rest) == 0 {
		return nil
	}
	h, ok := rest[len(rest)-1].(*object.Hash)
	if !ok {
		return nil
	}
	return h
}

// bleveQuerySlice reads a symbol-keyed option (must / should / must_not) from a
// Bleve::Query.bool Hash into a []Query. A missing key yields nil; a present
// value must be an Array of Bleve::Query.
func bleveQuerySlice(h *object.Hash, key string) []libbleve.Query {
	if h == nil {
		return nil
	}
	v, ok := h.Get(object.Symbol(key))
	if !ok {
		return nil
	}
	arr, ok := v.(*object.Array)
	if !ok {
		raise("TypeError", "%s: must be an Array of Bleve::Query", key)
	}
	out := make([]libbleve.Query, len(arr.Elems))
	for i, e := range arr.Elems {
		q, ok := e.(*BleveQuery)
		if !ok {
			raise("TypeError", "%s: element must be a Bleve::Query, got %s", key, e.Inspect())
		}
		out[i] = q.q
	}
	return out
}

// bleveFloat coerces a Ruby numeric (Integer, Float or Bignum) to a float64,
// raising TypeError for a non-numeric value.
func bleveFloat(v object.Value) float64 {
	switch n := v.(type) {
	case object.Integer:
		return float64(int64(n))
	case object.Float:
		return float64(n)
	case *object.Bignum:
		f, _ := new(big.Float).SetInt(n.I).Float64()
		return f
	}
	raise("TypeError", "expected a Numeric, got %s", v.Inspect())
	panic("unreachable")
}

// bleveBound resolves an open-ended numeric range bound: a Ruby nil is an
// unbounded side (nil pointer), any numeric becomes a *float64.
func bleveBound(v object.Value) *float64 {
	if object.IsNil(v) {
		return nil
	}
	return libbleve.F64(bleveFloat(v))
}

// bleveTime coerces a Ruby Time argument to a Go time.Time (second precision,
// UTC), raising TypeError for a non-Time value.
func bleveTime(v object.Value) stdtime.Time {
	t, ok := v.(*Time)
	if !ok {
		raise("TypeError", "expected a Time, got %s", v.Inspect())
	}
	return stdtime.Unix(t.t.ToUnix(), 0).UTC()
}

// bleveArity raises a Ruby ArgumentError when fewer than want arguments were
// given, matching MRI's arity error for the native method.
func bleveArity(args []object.Value, want int, name string) {
	if len(args) < want {
		raise("ArgumentError", "wrong number of arguments (given %d, expected %d) for %s",
			len(args), want, name)
	}
}

// raiseBleveErr re-raises a library error as the faithful Ruby exception: a
// closed-index failure becomes Bleve::ClosedError, a missing-document failure
// Bleve::NotFoundError, and every other bleve failure Bleve::Error. It never
// returns when err is non-nil.
func raiseBleveErr(err error) {
	if err == nil {
		return
	}
	switch {
	case errors.Is(err, libbleve.ErrClosed):
		raise("Bleve::ClosedError", "%s", err.Error())
	case errors.Is(err, libbleve.ErrNotFound):
		raise("Bleve::NotFoundError", "%s", err.Error())
	}
	raise("Bleve::Error", "%s", err.Error())
}

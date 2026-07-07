// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"fmt"
	"sort"
	"strings"

	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-ruby-ransack/ransack"
)

// newRansackSearch parses params into a ransack.Search against a Context derived
// from the subject's allowlist seams (ransackable_attributes /
// ransackable_associations) and wraps the pair in a RansackSearch. A nil / empty
// params yields an empty search that matches every row.
func (vm *VM) newRansackSearch(subject, params object.Value) *RansackSearch {
	var h *object.Hash
	if hh, ok := params.(*object.Hash); ok {
		h = hh
	}
	ctx := vm.ransackContext(subject)
	s := ransack.New(ctx, ransackParams(h))
	return &RansackSearch{vm: vm, search: s, subject: subject, params: h}
}

// ransackContext builds the allowlist Context for a subject. A subject that
// answers ransackable_attributes restricts the searchable/sortable columns to
// that list (ActiveRecord's ransackable_attributes seam); one that answers
// ransackable_associations registers each association as an open sub-context so
// association-qualified keys (author_name_cont) resolve. A subject that answers
// neither yields an open context that allows every attribute.
func (vm *VM) ransackContext(subject object.Value) *ransack.Context {
	ctx := ransack.NewContext()
	if vm.respondsTo(subject, "ransackable_attributes") {
		if arr, ok := vm.send(subject, "ransackable_attributes", nil, nil).(*object.Array); ok {
			attrs := make([]string, len(arr.Elems))
			for i, e := range arr.Elems {
				attrs[i] = ransackKey(e)
			}
			ctx = ransack.NewContext(attrs...)
		}
	}
	if vm.respondsTo(subject, "ransackable_associations") {
		if arr, ok := vm.send(subject, "ransackable_associations", nil, nil).(*object.Array); ok {
			for _, e := range arr.Elems {
				ctx.Associate(ransackKey(e), ransack.NewContext())
			}
		}
	}
	return ctx
}

// ransackBackend is the record-source seam: a Search's #result is handed this
// Backend, whose Result runs the engine's own in-memory evaluator (Search.Apply)
// over the subject's fetched rows and returns the matching Ruby records in the
// evaluated order. Each attrs map is paired (by index) with the Ruby record it
// was reflected from, so the engine evaluates over plain Go values while #result
// still returns the live record objects.
type ransackBackend struct {
	attrs    []map[string]any
	rubies   []object.Value
	distinct bool
}

// ransackIdxKey is the private per-row marker the backend injects so the record
// order Apply produces can be mapped back to the originating Ruby object. The
// NUL prefix keeps it clear of any real attribute name (and of the allowlisted
// names the predicates ever inspect), so it rides through Match/Apply untouched.
const ransackIdxKey = "\x00ransack_row_idx"

// Result evaluates the search over the fetched rows via the engine's in-memory
// Apply (filter + sort), then maps the surviving rows back to their Ruby
// records. When distinct is set, duplicate attribute rows are collapsed
// (keeping the first in evaluated order), the in-memory analogue of DISTINCT.
func (b *ransackBackend) Result(s *ransack.Search) (any, error) {
	rows := make([]map[string]any, len(b.attrs))
	for i, a := range b.attrs {
		m := make(map[string]any, len(a)+1)
		for k, v := range a {
			m[k] = v
		}
		m[ransackIdxKey] = i
		rows[i] = m
	}
	applied := s.Apply(rows)
	seen := map[string]bool{}
	out := make([]object.Value, 0, len(applied))
	for _, m := range applied {
		i := m[ransackIdxKey].(int)
		if b.distinct {
			key := ransackCanonicalKey(b.attrs[i])
			if seen[key] {
				continue
			}
			seen[key] = true
		}
		out = append(out, b.rubies[i])
	}
	return out, nil
}

// ransackResult fetches the subject's rows, reflects each into a Go attrs map
// paired with its Ruby record, and hands the pair to the engine through the
// ransackBackend seam. The result is a Ruby Array of the surviving records in
// evaluated order.
func (vm *VM) ransackResult(rs *RansackSearch, distinct bool) object.Value {
	records := vm.ransackRows(rs.subject)
	b := &ransackBackend{
		attrs:    make([]map[string]any, len(records)),
		rubies:   records,
		distinct: distinct,
	}
	for i, rec := range records {
		b.attrs[i] = vm.ransackAttrs(rec)
	}
	// The ransackBackend seam is infallible (it evaluates in memory), so Result
	// never returns an error; the value is the []object.Value it built.
	res, _ := rs.search.Result(b)
	return object.NewArrayFromSlice(res.([]object.Value))
}

// ransackRows fetches the subject's records. A model (a Class, e.g. a
// `Post < ActiveRecord::Base`) is queried through its own #all — the relation /
// query entry the gem searches over — which shadows any inherited helper; a bare
// Array subject is the row set itself; any other collection answering #to_a
// (an ActiveRecord::Relation, a Set, …) is coerced through it. A subject that
// resolves to none of these raises TypeError.
func (vm *VM) ransackRows(subject object.Value) []object.Value {
	if arr, ok := subject.(*object.Array); ok {
		return arr.Elems
	}
	coll := subject
	if _, isClass := subject.(*RClass); isClass {
		coll = vm.send(subject, "all", nil, nil)
	}
	if arr, ok := coll.(*object.Array); ok {
		return arr.Elems
	}
	if vm.respondsTo(coll, "to_a") {
		coll = vm.send(coll, "to_a", nil, nil)
	}
	if arr, ok := coll.(*object.Array); ok {
		return arr.Elems
	}
	raise("TypeError", "ransack subject #all must yield an Array-like collection")
	return nil
}

// ransackAttrs reflects a single record into the column -> value map the engine
// evaluates. A record answering #attributes yields its attribute Hash (the
// ActiveRecord::Record shape); a bare Hash record is used directly; anything
// else is an empty row.
func (vm *VM) ransackAttrs(rec object.Value) map[string]any {
	if h, ok := rec.(*object.Hash); ok {
		return ransackGoHash(h)
	}
	if vm.respondsTo(rec, "attributes") {
		if h, ok := vm.send(rec, "attributes", nil, nil).(*object.Hash); ok {
			return ransackGoHash(h)
		}
	}
	return map[string]any{}
}

// ransackParams converts a params Hash into the string-keyed map[string]any the
// engine parses. A nil Hash yields an empty map (an all-matching search).
func ransackParams(h *object.Hash) map[string]any {
	if h == nil {
		return map[string]any{}
	}
	return ransackGoHash(h)
}

// ransackGoHash converts a Ruby Hash into a map[string]any, recursively
// narrowing nested values, so g[] groups and association hashes reach the engine
// as native Go maps.
func ransackGoHash(h *object.Hash) map[string]any {
	m := make(map[string]any, len(h.Keys))
	for _, k := range h.Keys {
		val, _ := h.Get(k)
		m[ransackKey(k)] = ransackGoVal(val)
	}
	return m
}

// ransackGoVal narrows a Ruby value to the plain Go value model the engine's
// predicates compare (nil / bool / int64 / float64 / string / []any /
// map[string]any); anything unrecognised falls back to its to_s.
func ransackGoVal(v object.Value) any {
	switch n := v.(type) {
	case nil, object.Nil:
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
		out := make([]any, len(n.Elems))
		for i, e := range n.Elems {
			out[i] = ransackGoVal(e)
		}
		return out
	case *object.Hash:
		return ransackGoHash(n)
	}
	return v.ToS()
}

// ransackKey renders a Ruby Hash key (Symbol or String, else to_s) as the string
// key the engine expects.
func ransackKey(k object.Value) string {
	switch n := k.(type) {
	case *object.String:
		return n.Str()
	case object.Symbol:
		return string(n)
	}
	return k.ToS()
}

// ransackCanonicalKey builds a deterministic identity string for an attrs row,
// used to collapse duplicates under distinct.
func ransackCanonicalKey(rec map[string]any) string {
	keys := make([]string, 0, len(rec))
	for k := range rec {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=%v;", k, rec[k])
	}
	return b.String()
}

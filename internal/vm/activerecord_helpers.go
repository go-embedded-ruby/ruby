// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	activerecord "github.com/go-ruby-activerecord/activerecord"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// arStr coerces an argument to its String contents: a String yields its bytes, a
// Symbol its name, any other value its to_s.
func arStr(v object.Value) string {
	switch n := v.(type) {
	case *object.String:
		return n.Str()
	case object.Symbol:
		return string(n)
	}
	return v.ToS()
}

// arInt reads the first argument as an Integer (0 when absent / non-integer).
func arInt(args []object.Value) int {
	if len(args) == 0 {
		return 0
	}
	if n, ok := args[0].(object.Integer); ok {
		return int(n)
	}
	return 0
}

// arToGo maps a Ruby value into the generic Go value model activerecord consumes
// (nil / bool / int64 / *big.Int / float64 / string / Symbol / []any).
func arToGo(v object.Value) any {
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
		return activerecord.Symbol(string(n))
	case *object.Array:
		out := make([]any, len(n.Elems))
		for i, el := range n.Elems {
			out[i] = arToGo(el)
		}
		return out
	}
	return v.ToS()
}

// arAnyArgs maps a variadic list of Ruby column/name arguments to []any (Symbols
// and Strings pass through as their names).
func arAnyArgs(args []object.Value) []any {
	out := make([]any, len(args))
	for i, a := range args {
		switch v := a.(type) {
		case object.Symbol:
			out[i] = string(v)
		case *object.String:
			out[i] = v.Str()
		default:
			out[i] = arToGo(a)
		}
	}
	return out
}

// arCondArgs maps the arguments of where/not/having to the condition form
// activerecord accepts: a single Hash yields a map[string]any (column => value);
// otherwise the arguments pass through (a "sql", binds… fragment).
func arCondArgs(args []object.Value) []any {
	if len(args) == 1 {
		if h, ok := args[0].(*object.Hash); ok {
			return []any{arCondHash(h)}
		}
	}
	out := make([]any, len(args))
	for i, a := range args {
		if s, ok := a.(*object.String); ok {
			out[i] = s.Str()
			continue
		}
		out[i] = arToGo(a)
	}
	return out
}

// arCondHash maps a where(col: val) Hash to a map[string]any keyed by column name.
func arCondHash(h *object.Hash) map[string]any {
	m := make(map[string]any, len(h.Keys))
	for _, k := range h.Keys {
		val, _ := h.Get(k)
		m[arStr(k)] = arToGo(val)
	}
	return m
}

// arAttrs reads a build/create/new attributes Hash into map[string]any, or an
// empty map when absent.
func arAttrs(args []object.Value) map[string]any {
	if len(args) == 0 {
		return map[string]any{}
	}
	if h, ok := args[0].(*object.Hash); ok {
		return arCondHash(h)
	}
	return map[string]any{}
}

// arStrings maps a []string to a Ruby Array of Strings.
func arStrings(ss []string) *object.Array {
	arr := object.NewArrayFromSlice(make([]object.Value, len(ss)))
	for i, s := range ss {
		arr.Elems[i] = object.NewString(s)
	}
	return arr
}

// arStrList reads an add_index columns argument (a single name or an Array of
// names) into a []string.
func arStrList(v object.Value) []string {
	if arr, ok := v.(*object.Array); ok {
		out := make([]string, len(arr.Elems))
		for i, el := range arr.Elems {
			out[i] = arStr(el)
		}
		return out
	}
	return []string{arStr(v)}
}

// arLengthOpts reads a validates_length_of options Hash into LengthOpts
// (minimum: / maximum: / is:).
func arLengthOpts(args []object.Value) activerecord.LengthOpts {
	o := activerecord.LengthOpts{}
	if len(args) < 2 {
		return o
	}
	h, ok := args[1].(*object.Hash)
	if !ok {
		return o
	}
	for _, k := range h.Keys {
		val, _ := h.Get(k)
		n, isInt := val.(object.Integer)
		if !isInt {
			continue
		}
		i := int(n)
		switch arStr(k) {
		case "minimum":
			o.Minimum = &i
		case "maximum":
			o.Maximum = &i
		case "is":
			o.Is = &i
		}
	}
	return o
}

// arInList reads a validates_inclusion_of `in:` option into a []any allowed set.
func arInList(args []object.Value) []any {
	if len(args) < 2 {
		return nil
	}
	h, ok := args[1].(*object.Hash)
	if !ok {
		return nil
	}
	if v, ok := h.Get(object.Symbol("in")); ok {
		if arr, ok := v.(*object.Array); ok {
			out := make([]any, len(arr.Elems))
			for i, el := range arr.Elems {
				out[i] = arToGo(el)
			}
			return out
		}
	}
	return nil
}

// arClassName reads a belongs_to / has_many `class_name:` option, defaulting to
// the association name capitalised (the gem's inference is a host concern; the
// name is enough for the join SQL).
func arClassName(args []object.Value) string {
	if len(args) > 1 {
		if h, ok := args[1].(*object.Hash); ok {
			if v, ok := h.Get(object.Symbol("class_name")); ok {
				return arStr(v)
			}
		}
	}
	return arStr(args[0])
}

// arPluck extracts the requested columns from loaded records into a Ruby Array:
// one column yields a flat Array of values; several yield an Array of Arrays,
// matching ActiveRecord::Relation#pluck.
func arPluck(recs []*activerecord.Record, args []object.Value) *object.Array {
	cols := make([]string, len(args))
	for i, a := range args {
		cols[i] = arStr(a)
	}
	out := object.NewArrayFromSlice(make([]object.Value, len(recs)))
	for i, rec := range recs {
		if len(cols) == 1 {
			val, _ := rec.Get(cols[0])
			out.Elems[i] = arValueToRuby(val)
			continue
		}
		row := object.NewArrayFromSlice(make([]object.Value, len(cols)))
		for j, c := range cols {
			val, _ := rec.Get(c)
			row.Elems[j] = arValueToRuby(val)
		}
		out.Elems[i] = row
	}
	return out
}

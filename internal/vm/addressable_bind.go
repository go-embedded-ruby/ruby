// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"sort"

	addressable "github.com/go-ruby-addressable/addressable"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's object graph and the
// github.com/go-ruby-addressable/addressable library. The RFC 3986 / 6570 work
// lives in that library; rbgo only translates *string components, template
// variable maps and extract results.

// strPtrToRuby maps an addressable component (*string, nil = absent) to a Ruby
// String or nil.
func strPtrToRuby(p *string) object.Value {
	if p == nil {
		return object.NilV
	}
	return object.NewString(*p)
}

// addressableURIStr coerces a URI-ish argument (a String or an Addressable::URI
// wrapper) to its string form.
func addressableURIStr(v object.Value) string {
	if u, ok := object.KindOK[*AddressableURI](v); ok {
		return u.u.String()
	}
	return strArg(v)
}

// addressableVars maps a Ruby Hash of template variables to the library's
// map[string]addressable.Value. A String value maps to a Go string; an Array
// value maps to a []string (RFC 6570 list expansion). Any other value is coerced
// via to_s.
func addressableVars(v object.Value) map[string]addressable.Value {
	h, ok := object.KindOK[*object.Hash](v)
	if !ok {
		raise("TypeError", "expected a Hash of template variables")
	}
	out := make(map[string]addressable.Value, h.Len())
	for _, k := range h.Keys {
		val, _ := h.Get(k)
		out[addressableKey(k)] = addressableVal(val)
	}
	return out
}

// addressableKey renders a Hash key as a template variable name: a Symbol by its
// name, any other value by to_s.
func addressableKey(k object.Value) string {
	if s, ok := object.KindOK[object.Symbol](k); ok {
		return string(s)
	}
	return k.ToS()
}

// addressableVal maps a Ruby template-variable value to an addressable.Value: an
// Array to a []string list, anything else to a scalar string.
func addressableVal(v object.Value) addressable.Value {
	if arr, ok := object.KindOK[*object.Array](v); ok {
		list := make([]string, len(arr.Elems))
		for i, e := range arr.Elems {
			list[i] = e.ToS()
		}
		return list
	}
	return v.ToS()
}

// anyMapToHash maps a Template#extract result (map[string]any of strings and
// []string) to a Ruby Hash, sorting keys for a deterministic order.
func anyMapToHash(m map[string]any) object.Value {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := object.NewHash()
	for _, k := range keys {
		h.Set(object.NewString(k), anyToRuby(m[k]))
	}
	return h
}

// anyToRuby maps an extract value (a string or a []string) to a Ruby value.
func anyToRuby(v any) object.Value {
	switch x := v.(type) {
	case string:
		return object.NewString(x)
	case []string:
		return strSliceToArray(x)
	}
	return object.NilV
}

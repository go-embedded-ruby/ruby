// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	pagy "github.com/go-ruby-pagy/pagy"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph and the
// interpreter-independent github.com/go-ruby-pagy/pagy library. The page-set
// arithmetic and the series algorithm live in that library; rbgo only maps the
// keyword-option Hash to pagy.Vars, translates the *OverflowError / *VariableError
// to the matching Ruby error, and renders the series ([]any of int / string /
// Gap) back into a Ruby Array.

// pagyOf returns the receiver's wrapped library Pagy.
func pagyOf(v object.Value) *pagy.Pagy { return v.(*Pagy).p }

// pagyNew computes a Pagy, raising Pagy::OverflowError when the requested page is
// past the last page (the only error New reports).
func pagyNew(v pagy.Vars) *pagy.Pagy {
	p, err := pagy.New(v)
	if err != nil {
		raise("Pagy::OverflowError", "%s", err.Error())
	}
	return p
}

// pagyVars reads the trailing keyword Hash of a call into a pagy.Vars.
func pagyVars(args []object.Value) pagy.Vars {
	return pagyVarsFromHash(pagyKwargs(args))
}

// pagyVarsFromHash maps a keyword-options Hash to pagy.Vars. The keys mirror the
// gem's options: count:, page:, items: (alias limit:), outset:, size:, max_pages:,
// cycle: and ends:. An absent key keeps pagy's default for that variable.
func pagyVarsFromHash(h *object.Hash) pagy.Vars {
	v := pagy.Vars{}
	pagyEachOpt(h, func(key string, val object.Value) {
		switch key {
		case "count":
			v.Count = int(intArg(val))
		case "page":
			v.Page = int(intArg(val))
		case "items", "limit":
			v.Items = int(intArg(val))
		case "outset":
			v.Outset = int(intArg(val))
		case "size":
			v.Size = int(intArg(val))
		case "max_pages":
			v.MaxPages = int(intArg(val))
		case "cycle":
			v.Cycle = val.Truthy()
		case "ends":
			b := val.Truthy()
			v.Ends = &b
		}
	})
	return v
}

// pagyHelper implements the top-level pagy(collection, **vars) helper: it
// paginates an in-memory Array and returns [pagy, page_items]. The item count
// defaults to the collection's length unless count: is given, and page_items is
// the slice of the collection for the current page's offset/limit window.
func pagyHelper(args []object.Value) object.Value {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1+)")
	}
	arr, ok := args[0].(*object.Array)
	if !ok {
		raise("TypeError", "no implicit conversion of %s into Array", args[0].Inspect())
	}
	h := pagyKwargs(args[1:])
	v := pagyVarsFromHash(h)
	if !pagyHasKey(h, "count") {
		v.Count = len(arr.Elems)
	}
	p := pagyNew(v)

	lo := pagyCap(p.Offset, len(arr.Elems))
	hi := pagyCap(p.Offset+p.Limit(), len(arr.Elems))
	page := make([]object.Value, hi-lo)
	copy(page, arr.Elems[lo:hi])
	return object.NewArrayFromSlice([]object.Value{
		&Pagy{p: p},
		object.NewArrayFromSlice(page),
	})
}

// pagyCap caps a (non-negative) window bound i to the collection length n, so a
// window past the collection yields an empty slice rather than panicking.
func pagyCap(i, n int) int {
	if i > n {
		return n
	}
	return i
}

// pagySeries renders a pagy navigation series ([]any of int page links, the
// current page as a string, and Gap for elided ranges) into a Ruby Array of
// Integers, a String for the current page, and :gap for each gap.
func pagySeries(s []any) object.Value {
	elems := make([]object.Value, len(s))
	for i, e := range s {
		switch x := e.(type) {
		case int:
			elems[i] = object.IntValue(int64(x))
		case string:
			elems[i] = object.NewString(x)
		case pagy.SeriesGap:
			elems[i] = object.Symbol("gap")
		}
	}
	return object.NewArrayFromSlice(elems)
}

// pagyPageOrNil renders a page number as an Integer, or nil when it is 0 (the
// library's "no such page"), matching the gem's nil prev/next at the edges.
func pagyPageOrNil(page int) object.Value {
	if page == 0 {
		return object.NilV
	}
	return object.IntValue(int64(page))
}

// pagyKwargs returns the trailing keyword Hash of a call, or an empty Hash.
func pagyKwargs(args []object.Value) *object.Hash {
	if len(args) > 0 {
		if h, ok := args[len(args)-1].(*object.Hash); ok {
			return h
		}
	}
	return object.NewHash()
}

// pagyHasKey reports whether the options Hash carries a key with the given bare
// name (a Symbol or String), so the helper can tell an explicit count: 0 from an
// absent count:.
func pagyHasKey(h *object.Hash, name string) bool {
	for _, k := range h.Keys {
		if pagyKey(k) == name {
			return true
		}
	}
	return false
}

// pagyEachOpt iterates a (possibly empty) options Hash, calling fn with each
// key's bare name and value.
func pagyEachOpt(h *object.Hash, fn func(key string, val object.Value)) {
	for _, k := range h.Keys {
		val, _ := h.Get(k)
		fn(pagyKey(k), val)
	}
}

// pagyKey renders an option key (a Symbol or String) as its bare name.
func pagyKey(k object.Value) string {
	switch n := k.(type) {
	case object.Symbol:
		return string(n)
	case *object.String:
		return n.Str()
	}
	return k.ToS()
}

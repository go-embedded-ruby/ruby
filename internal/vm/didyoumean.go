// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	didyoumean "github.com/go-ruby-did-you-mean/did-you-mean"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file binds github.com/go-ruby-did-you-mean/did-you-mean — the pure-Go,
// MRI-4.0.5-faithful port of the deterministic core of Ruby's did_you_mean
// stdlib — into rbgo. The library owns the spell-suggestion matcher
// (the Jaro–Winkler / Levenshtein ranking that DidYouMean::SpellChecker#correct
// returns); this file is the thin shell that exposes the SpellChecker class,
// maps the Ruby dictionary (Strings and/or Symbols) onto the library's []string
// model, and maps each ranked result back to the dictionary entry's ORIGINAL
// Ruby type — so a String dictionary yields String suggestions and a Symbol
// dictionary yields Symbol suggestions, exactly as MRI's #correct does.
//
// The interpreter-tied half of did_you_mean (the NameError/NoMethodError
// auto-suggestion hooks, the per-error checkers and the formatter) is out of
// scope: rbgo has no such hooks, and MRI's lib only ties them to error display,
// not to the standalone matcher. rbgo had no prior did_you_mean code, so this is
// a pure addition.

// SpellChecker is the Ruby wrapper around the library's Correct matcher. It
// holds the dictionary twice: once as the []string the library ranks over, and
// once as the parallel slice of the original Ruby values, so each ranked result
// (returned by the library as a name string) is mapped back to the dictionary
// entry it came from — preserving its String/Symbol type and the library's
// result order.
type SpellChecker struct {
	names  []string                // dictionary entries as the library sees them (String bytes / Symbol name)
	byName map[string]object.Value // library name -> the first dictionary entry's original Ruby value
}

func (s *SpellChecker) ToS() string     { return "#<DidYouMean::SpellChecker>" }
func (s *SpellChecker) Inspect() string { return s.ToS() }
func (s *SpellChecker) Truthy() bool    { return true }

// dictName renders one dictionary entry as the library's input name: a Symbol
// contributes its name, a String its bytes. Any other value contributes its
// #to_s-equivalent byte view via Inspect-free coercion is avoided — MRI's
// SpellChecker stringifies each entry, so a non-String/Symbol entry keys by its
// ToS() rendering and maps back to that same original Ruby value.
func dictName(v object.Value) string {
	switch x := v.(type) {
	case object.Symbol:
		return string(x)
	case *object.String:
		return string(x.Bytes())
	default:
		return x.ToS()
	}
}

// correct ranks input against the dictionary and returns the matches as Ruby
// values of the dictionary entries' original type, preserving the library's
// order. The library returns names drawn from the dictionary; each name maps
// back through the name→value table to the first dictionary entry that produced
// it, so a Symbol dictionary yields Symbols and a String dictionary yields
// Strings (an Integer dictionary yields Integers, etc.).
func (s *SpellChecker) correct(input string) object.Value {
	hits := didyoumean.Correct(input, s.names)
	out := make([]object.Value, 0, len(hits))
	for _, h := range hits {
		out = append(out, s.byName[h])
	}
	return object.NewArrayFromSlice(out)
}

// registerDidYouMean installs the DidYouMean module and its nested SpellChecker
// class eagerly (like Set), so DidYouMean::SpellChecker is usable after
// `require "did_you_mean"` returns true. The module is a plain namespace; the
// SpellChecker class carries the matcher's constructor (.new) and #correct.
func (vm *VM) registerDidYouMean() {
	mod := newClass("DidYouMean", nil)
	mod.isModule = true
	vm.cDidYouMean = mod
	vm.consts["DidYouMean"] = mod

	sc := newClass("SpellChecker", vm.cObject)
	sc.lexParent = mod
	vm.cSpellChecker = sc
	mod.consts["SpellChecker"] = sc
	vm.consts["DidYouMean::SpellChecker"] = sc

	// SpellChecker.new(dictionary:) — dictionary: is a required keyword. The kwargs
	// arrive as a trailing *object.Hash; a missing :dictionary raises ArgumentError
	// with MRI's exact message. A positional argument (with no kwargs) is the MRI
	// "wrong number of arguments" arity error.
	sc.smethods["new"] = &Method{name: "new", owner: sc,
		native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			h, ok := trailingHash(args)
			if !ok {
				// Any non-kwargs argument is a positional one: MRI reports the arity error.
				if len(args) > 0 {
					raise("ArgumentError", "wrong number of arguments (given %d, expected 0; required keyword: dictionary)", len(args))
				}
				raise("ArgumentError", "missing keyword: :dictionary")
			}
			dict, ok := h.Get(object.Symbol("dictionary"))
			if !ok {
				raise("ArgumentError", "missing keyword: :dictionary")
			}
			return newSpellChecker(dict)
		}}

	// SpellChecker#correct(input) — the ranked matches in the dictionary entries'
	// original type, or [] when nothing is close enough.
	sc.define("correct", func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return v.(*SpellChecker).correct(dictName(args[0]))
	})
}

// trailingHash returns the last argument as a *object.Hash (the kwargs bundle)
// when present. SpellChecker.new takes only keywords, so the entire argument
// list is the trailing hash; an empty list or a non-Hash last argument is not a
// kwargs call.
func trailingHash(args []object.Value) (*object.Hash, bool) {
	if len(args) == 0 {
		return nil, false
	}
	h, ok := args[len(args)-1].(*object.Hash)
	return h, ok
}

// newSpellChecker builds the wrapper from the Ruby dictionary value, which MRI
// requires to be an Array. Each entry is recorded both as its library name and
// as its original Ruby value, index-aligned, so #correct can round-trip the
// entry's String/Symbol type.
func newSpellChecker(dict object.Value) *SpellChecker {
	arr, ok := dict.(*object.Array)
	if !ok {
		raise("TypeError", "no implicit conversion of %s into Array", dict.Inspect())
	}
	sc := &SpellChecker{
		names:  make([]string, 0, len(arr.Elems)),
		byName: make(map[string]object.Value, len(arr.Elems)),
	}
	for _, e := range arr.Elems {
		n := dictName(e)
		sc.names = append(sc.names, n)
		// First entry wins for a given name, so a result maps back to the earliest
		// dictionary entry that produced it (matching how MRI's #correct dedups).
		if _, seen := sc.byName[n]; !seen {
			sc.byName[n] = e
		}
	}
	return sc
}

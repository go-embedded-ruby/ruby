// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	abbrev "github.com/go-ruby-abbrev/abbrev"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file binds github.com/go-ruby-abbrev/abbrev — the pure-Go, MRI-4.0.5
// faithful port of Ruby's `abbrev` stdlib — into rbgo. The library owns the
// abbreviation algorithm: given a set of words it returns the unambiguous
// prefixes (every prefix that is a prefix of exactly one word), plus each full
// word, and applies the optional String-prefix filter. This file is the thin
// shell that maps rbgo's Array/String value model onto the library's []string
// model, reconstructs MRI's Hash insertion order over the library's keyset and
// registers the Ruby-visible surface: Abbrev.abbrev and the Array#abbrev core
// extension. rbgo had no prior abbrev implementation, so this is a pure
// addition (no inline abbrev existed to replace).
//
// The library exposes only the String-prefix form of MRI's optional `pattern`
// argument — the documented and common case (a String is regarded as a
// /\A.../ anchored prefix). MRI also accepts a Regexp pattern; that rarer form
// is handled host-side in abbrevWords below by filtering the input words
// through the Ruby Regexp first and then asking the library for the
// unfiltered abbreviations of the survivors, so the library's prefix-only API
// still backs the algorithm.

// registerAbbrev records the require "abbrev" feature hook. Nothing is
// installed eagerly: the hook (run once by doRequire on the first `require
// "abbrev"`) creates the Abbrev module and adds Array#abbrev, mirroring MRI
// where lib/abbrev.rb defines them only when loaded. Before then
// `defined?(Abbrev)` is nil and Array does not respond to #abbrev. The
// featureHooks map is already created by registerPrime (which runs first), so
// this only records the hook.
func (vm *VM) registerAbbrev() {
	vm.featureHooks["abbrev"] = vm.installAbbrev
}

// installAbbrev installs the Abbrev module and the Array#abbrev core extension —
// the body MRI's lib/abbrev.rb runs on load. Abbrev.abbrev(words[, pattern]) is
// the module function; Array#abbrev(pattern = nil) delegates to it on self.
func (vm *VM) installAbbrev() {
	mod := newClass("Abbrev", nil)
	mod.isModule = true
	vm.consts["Abbrev"] = mod

	// Abbrev.abbrev(words, pattern = nil) — the module function (module_function
	// in MRI). words must be an Array; pattern is an optional String prefix or
	// Regexp (or nil).
	mod.smethods["abbrev"] = &Method{name: "abbrev", owner: mod, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		words := arrArg(args[0])
		var pattern object.Value
		if len(args) > 1 {
			pattern = args[1]
		}
		return abbrevHash(vm, words, pattern)
	}}

	// Array#abbrev(pattern = nil) — the core extension `require "abbrev"` adds; it
	// calls Abbrev.abbrev(self, pattern).
	vm.cArray.define("abbrev", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		var pattern object.Value
		if len(args) > 0 {
			pattern = args[0]
		}
		return abbrevHash(vm, self.(*object.Array), pattern)
	})
}

// abbrevHash computes the abbreviation table for a Ruby Array of words and
// returns it as a Ruby Hash in MRI's exact insertion order. The library owns
// which keys survive (the unambiguous set); this function reproduces MRI's
// ordering over that keyset.
func abbrevHash(vm *VM, arr *object.Array, pattern object.Value) object.Value {
	words := abbrevWords(vm, arr)

	var got map[string]string
	var order []string
	if re, ok := abbrevRegexp(pattern); ok {
		// The Regexp form is the rarer case the library does not express: MRI
		// applies the pattern to every *abbreviation* (not just whole words), so
		// the ambiguity set itself differs from a plain word-filter. We therefore
		// replay MRI's exact two-pass algorithm host-side for this branch only,
		// asking the Ruby Regexp whether each candidate matches; the common
		// String-prefix and no-pattern forms stay backed by the library below.
		got, order = abbrevRegexpTable(vm, words, re)
	} else {
		// The String-prefix form maps directly onto the library (a String pattern
		// is an anchored /\A.../ prefix); a nil/absent pattern is the plain form.
		if prefix, ok := abbrevPrefix(pattern); ok {
			got = abbrev.Abbrev(words, prefix)
		} else {
			got = abbrev.Abbrev(words)
		}
		order = abbrevOrder(words, got)
	}

	h := object.NewHash()
	for _, key := range order {
		h.Set(object.NewString(key), object.NewString(got[key]))
	}
	return h
}

// abbrevWords coerces a Ruby Array of words to a []string. Each element must be
// a String (matching MRI, which calls String methods on the elements).
func abbrevWords(vm *VM, arr *object.Array) []string {
	words := make([]string, len(arr.Elems))
	for i, e := range arr.Elems {
		words[i] = strArg(e)
	}
	return words
}

// abbrevPrefix reports whether pattern is a String prefix (MRI regards a String
// pattern as an anchored /\A.../ prefix), returning the prefix text.
func abbrevPrefix(pattern object.Value) (string, bool) {
	if s, ok := pattern.(*object.String); ok {
		return s.Str(), true
	}
	return "", false
}

// abbrevRegexp reports whether pattern is a Ruby Regexp, returning it so the
// rarer pattern form can be applied host-side.
func abbrevRegexp(pattern object.Value) (object.Value, bool) {
	if pattern == nil {
		return nil, false
	}
	if _, isNil := pattern.(object.Nil); isNil {
		return nil, false
	}
	if _, ok := pattern.(*object.String); ok {
		return nil, false
	}
	return pattern, true
}

// abbrevRegexpTable replays MRI's lib/abbrev.rb algorithm with a Regexp
// pattern: pass 1 walks each word's prefixes longest->shortest, skips those the
// pattern does NOT match, and runs the seen-counter state machine (insert on
// first sight, delete on second, break thereafter) on the survivors; pass 2
// appends each full word the pattern matches. It returns the surviving key->word
// table and the keys in MRI insertion order. This is the faithful host-side
// path for the form the library cannot express (the pattern filters
// abbreviations, not just whole words).
func abbrevRegexpTable(vm *VM, words []string, re object.Value) (map[string]string, []string) {
	table := map[string]string{}
	order := make([]string, 0)
	pos := map[string]int{} // key -> index in order, -1 once deleted
	seen := map[string]int{}

	insert := func(k, word string) {
		table[k] = word
		pos[k] = len(order)
		order = append(order, k)
	}
	del := func(k string) {
		delete(table, k)
		if i := pos[k]; i >= 0 {
			order = append(order[:i], order[i+1:]...)
			for kk, ii := range pos {
				if ii > i {
					pos[kk] = ii - 1
				}
			}
		}
		pos[k] = -1
	}

	for _, word := range words {
		if word == "" {
			continue
		}
		r := []rune(word)
		for l := len(r); l >= 1; l-- {
			ab := string(r[:l])
			if !abbrevMatch(vm, re, ab) {
				continue
			}
			seen[ab]++
			switch seen[ab] {
			case 1:
				insert(ab, word)
			case 2:
				del(ab)
			default:
				l = 0 // break out of the downto loop
			}
		}
	}
	for _, word := range words {
		if !abbrevMatch(vm, re, word) {
			continue
		}
		if i, ok := pos[word]; ok && i >= 0 {
			table[word] = word // already present, keep position
			continue
		}
		insert(word, word)
	}
	return table, order
}

// abbrevMatch reports whether the Ruby Regexp matches s, via Regexp#match?.
func abbrevMatch(vm *VM, re object.Value, s string) bool {
	r := vm.send(re, "match?", []object.Value{object.NewString(s)}, nil)
	return r.Truthy()
}

// abbrevOrder reconstructs MRI's Hash insertion order for the surviving keyset
// `got`. MRI builds the table in two passes (lib/abbrev.rb): first, for each
// word, prefixes from longest down to length 1, inserting on first sight and
// deleting on the second (and breaking thereafter); then a second pass appends
// each full word. We replay that exact bookkeeping but only emit keys the
// library kept, so the library remains the authority for membership while the
// order matches MRI byte-for-byte.
func abbrevOrder(words []string, got map[string]string) []string {
	order := make([]string, 0, len(got))
	pos := map[string]int{} // key -> index in order, -1 once deleted
	seen := map[string]int{}

	insert := func(k string) {
		if i, ok := pos[k]; ok && i >= 0 {
			return // already present, keep position
		}
		pos[k] = len(order)
		order = append(order, k)
	}
	del := func(k string) {
		if i, ok := pos[k]; ok && i >= 0 {
			order = append(order[:i], order[i+1:]...)
			for kk, ii := range pos {
				if ii > i {
					pos[kk] = ii - 1
				}
			}
		}
		pos[k] = -1
	}

	// Pass 1: prefixes longest -> shortest, with the seen-counter state machine.
	for _, word := range words {
		if word == "" {
			continue
		}
		r := []rune(word)
		for l := len(r); l >= 1; l-- {
			ab := string(r[:l])
			seen[ab]++
			switch seen[ab] {
			case 1:
				if _, keep := got[ab]; keep {
					insert(ab)
				}
			case 2:
				del(ab)
			default:
				l = 0 // break out of the downto loop
			}
		}
	}
	// Pass 2: re-assert each full word (appends if absent, keeps position if not).
	for _, word := range words {
		if _, keep := got[word]; keep {
			insert(word)
		}
	}
	return order
}

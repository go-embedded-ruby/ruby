// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	fastgettext "github.com/go-ruby-fast-gettext/fast-gettext"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// fgStrArg reads the single string argument, raising ArgumentError when it is
// missing.
func fgStrArg(args []object.Value) string {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
	}
	return args[0].ToS()
}

// fgIntArg reads an integer count, treating a non-integer as zero.
func fgIntArg(v object.Value) int {
	if n, ok := v.(object.Integer); ok {
		return int(n)
	}
	return 0
}

// fgStrSliceArg reads an Array argument as a slice of strings (each element's
// #to_s), raising ArgumentError when it is missing or not an Array.
func fgStrSliceArg(args []object.Value) []string {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
	}
	arr, ok := args[0].(*object.Array)
	if !ok {
		raise("ArgumentError", "expected an Array of locales")
	}
	return arrToStrings(arr)
}

// arrToStrings maps an Array's elements to their #to_s.
func arrToStrings(arr *object.Array) []string {
	out := make([]string, len(arr.Elems))
	for i, e := range arr.Elems {
		out[i] = e.ToS()
	}
	return out
}

// strOrNil maps an empty string to Ruby nil, any other to a String.
func strOrNil(s string) object.Value {
	if s == "" {
		return object.NilV
	}
	return object.NewString(s)
}

// strSliceToRuby maps a slice of strings to a Ruby Array of Strings.
func strSliceToRuby(ss []string) object.Value {
	out := make([]object.Value, len(ss))
	for i, s := range ss {
		out[i] = object.NewString(s)
	}
	return object.NewArrayFromSlice(out)
}

// fastGettextAddDomain implements FastGettext.add_text_domain(name,
// translations:, plurals:): it builds an in-memory TestRepository from the two
// keyword Hashes and registers it under name on the per-VM instance.
func (vm *VM) fastGettextAddDomain(args []object.Value) {
	name := fgStrArg(args)
	repo := fastgettext.NewTestRepository(name)

	kw := fgKwHash(args[1:])
	if kw != nil {
		if tr, ok := fgKwGet(kw, "translations"); ok {
			fgStoreTranslations(repo, tr)
		}
		if pl, ok := fgKwGet(kw, "plurals"); ok {
			fgStorePlurals(repo, pl)
		}
	}
	vm.fastGettext.AddTextDomain(name, repo)
}

// fgStoreTranslations stores {locale => {key => value}} singular entries.
func fgStoreTranslations(repo *fastgettext.TestRepository, v object.Value) {
	byLocale, ok := v.(*object.Hash)
	if !ok {
		return
	}
	for _, lk := range byLocale.Keys {
		locale := lk.ToS()
		entries, _ := byLocale.Get(lk)
		eh, ok := entries.(*object.Hash)
		if !ok {
			continue
		}
		for _, k := range eh.Keys {
			val, _ := eh.Get(k)
			repo.Store(locale, k.ToS(), val.ToS())
		}
	}
}

// fgStorePlurals stores {locale => {[singular, plural] => [form0, form1, ...]}}
// plural entries.
func fgStorePlurals(repo *fastgettext.TestRepository, v object.Value) {
	byLocale, ok := v.(*object.Hash)
	if !ok {
		return
	}
	for _, lk := range byLocale.Keys {
		locale := lk.ToS()
		entries, _ := byLocale.Get(lk)
		eh, ok := entries.(*object.Hash)
		if !ok {
			continue
		}
		for _, k := range eh.Keys {
			keys, kok := k.(*object.Array)
			forms, _ := eh.Get(k)
			fa, fok := forms.(*object.Array)
			if !kok || !fok {
				continue
			}
			repo.StorePlural(locale, arrToStrings(keys), arrToStrings(fa))
		}
	}
}

// fgKwHash returns the trailing Hash argument (keyword options), or nil.
func fgKwHash(args []object.Value) *object.Hash {
	if n := len(args); n > 0 {
		if h, ok := args[n-1].(*object.Hash); ok {
			return h
		}
	}
	return nil
}

// fgKwGet reads a keyword by symbol or string key from a kwargs Hash.
func fgKwGet(h *object.Hash, key string) (object.Value, bool) {
	if v, ok := h.Get(object.Symbol(key)); ok {
		return v, true
	}
	return h.Get(object.NewString(key))
}

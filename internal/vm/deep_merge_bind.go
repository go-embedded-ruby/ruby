// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"math/big"
	"sort"

	deepmerge "github.com/go-ruby-deep-merge/deep-merge"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// deepMergeModule implements DeepMerge.deep_merge!(source, dest, opts={}) and its
// non-bang form. The bang form mutates a Hash dest in place and returns it.
func deepMergeModule(args []object.Value, bang bool) object.Value {
	if len(args) < 2 || len(args) > 3 {
		raise("ArgumentError", "wrong number of arguments (given %d, expected 2..3)", len(args))
	}
	source, dest := args[0], args[1]
	o := deepMergeOptions(args, 2, bang)
	merged := deepMergeRun(rubyToGoValue(dest), rubyToGoValue(source), o)
	reg := map[string]object.Value{}
	collectHashKeys(dest, reg)
	collectHashKeys(source, reg)
	if bang {
		if h, ok := dest.(*object.Hash); ok {
			deepMergeReplaceHash(h, merged, reg)
			return h
		}
	}
	return deepMergeToRuby(merged, reg)
}

// deepMergeHashBang implements Hash#deep_merge!(source, opts={}): it merges source
// into the receiver Hash in place and returns the receiver.
func deepMergeHashBang(self object.Value, args []object.Value) object.Value {
	if len(args) < 1 || len(args) > 2 {
		raise("ArgumentError", "wrong number of arguments (given %d, expected 1..2)", len(args))
	}
	source := args[0]
	o := deepMergeOptions(args, 1, true)
	merged := deepMergeRun(rubyToGoValue(self), rubyToGoValue(source), o)
	reg := map[string]object.Value{}
	collectHashKeys(self, reg)
	collectHashKeys(source, reg)
	h := self.(*object.Hash)
	deepMergeReplaceHash(h, merged, reg)
	return h
}

// deepMergeOptions builds the library Options from an optional trailing options
// Hash. preserve_unmergeables defaults to the non-bang choice (bang overwrites,
// non-bang preserves); an explicit key in the hash overrides it. A non-Hash,
// non-nil options argument is an ArgumentError.
func deepMergeOptions(args []object.Value, idx int, bang bool) deepmerge.Options {
	o := deepmerge.Options{PreserveUnmergeables: !bang}
	if idx >= len(args) || object.IsNil(args[idx]) {
		return o
	}
	h, ok := args[idx].(*object.Hash)
	if !ok {
		raise("ArgumentError", "deep_merge options must be a Hash")
	}
	for _, k := range h.Keys {
		v, _ := h.Get(k)
		switch k.ToS() {
		case "preserve_unmergeables":
			o.PreserveUnmergeables = deepMergeTruthy(v)
		case "knockout_prefix":
			o.KnockoutPrefix = v.ToS()
		case "overwrite_arrays":
			o.OverwriteArrays = deepMergeTruthy(v)
		case "sort_merged_arrays":
			o.SortMergedArrays = deepMergeTruthy(v)
		case "unpack_arrays":
			o.UnpackArrays = v.ToS()
		case "merge_hash_arrays":
			o.MergeHashArrays = deepMergeTruthy(v)
		case "extend_existing_arrays":
			o.ExtendExistingArrays = deepMergeTruthy(v)
		case "keep_array_duplicates":
			o.KeepArrayDuplicates = deepMergeTruthy(v)
		case "merge_nil_values":
			o.MergeNilValues = deepMergeTruthy(v)
		}
	}
	return o
}

// deepMergeRun runs the merge. It pre-validates the one option combination the
// library rejects — a knockout prefix without overwrite — raising a Ruby
// DeepMerge::InvalidParameter, so the library's resolve() (its only panic site)
// never fires.
func deepMergeRun(dest, source any, o deepmerge.Options) any {
	if o.KnockoutPrefix != "" && !o.Overwrite && o.PreserveUnmergeables {
		raise("DeepMerge::InvalidParameter", "deep_merge: knockout_prefix requires overwrite; do not set preserve_unmergeables")
	}
	return deepmerge.DeepMerge(dest, source, o)
}

// deepMergeTruthy reports Ruby truthiness: every value but nil and false.
func deepMergeTruthy(v object.Value) bool {
	if object.IsNil(v) {
		return false
	}
	if b, ok := v.(object.Bool); ok {
		return bool(b)
	}
	return true
}

// collectHashKeys records the original Ruby key object for every hash key
// (recursively) under its #to_s, so the merged result can restore symbol-vs-string
// key identity instead of stringifying every key.
func collectHashKeys(v object.Value, reg map[string]object.Value) {
	switch x := v.(type) {
	case *object.Hash:
		for _, k := range x.Keys {
			if _, seen := reg[k.ToS()]; !seen {
				reg[k.ToS()] = k
			}
			val, _ := x.Get(k)
			collectHashKeys(val, reg)
		}
	case *object.Array:
		for _, e := range x.Elems {
			collectHashKeys(e, reg)
		}
	}
}

// deepMergeToRuby converts a merged Go value back to Ruby, restoring original key
// identity from reg and preserving integer types (the generic JSON converter maps
// only float64). The type set is exactly what rubyToGoValue produces, so the
// map[string]any case is the default arm.
func deepMergeToRuby(v any, reg map[string]object.Value) object.Value {
	switch x := v.(type) {
	case nil:
		return object.NilV
	case bool:
		return object.Bool(x)
	case string:
		return object.NewString(x)
	case int64:
		return object.IntValue(x)
	case float64:
		return object.Float(x)
	case *big.Int:
		return &object.Bignum{I: x}
	case []any:
		elems := make([]object.Value, len(x))
		for i, e := range x {
			elems[i] = deepMergeToRuby(e, reg)
		}
		return object.NewArrayFromSlice(elems)
	default:
		m := v.(map[string]any)
		h := object.NewHash()
		for _, k := range deepMergeSortedKeys(m) {
			// Every result map key comes from the dest or source hashes, so reg
			// always holds its original Ruby key object (merge never invents keys).
			h.Set(reg[k], deepMergeToRuby(m[k], reg))
		}
		return h
	}
}

// deepMergeReplaceHash rewrites h in place to the merged result, preserving the
// receiver's object identity so the bang form mutates its argument.
func deepMergeReplaceHash(h *object.Hash, merged any, reg map[string]object.Value) {
	m, ok := merged.(map[string]any)
	if !ok {
		// A non-hash source overwrote the whole hash (e.g. a scalar with
		// overwrite on); the receiver's Hash identity cannot become that value,
		// so leave it unchanged rather than emptying it.
		return
	}
	h.Clear()
	for _, k := range deepMergeSortedKeys(m) {
		h.Set(reg[k], deepMergeToRuby(m[k], reg))
	}
}

// deepMergeSortedKeys returns a map's keys in sorted order so a converted Hash is
// deterministic.
func deepMergeSortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

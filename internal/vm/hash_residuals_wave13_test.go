// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// TestHashNewCapacityKwarg covers Hash.new / Hash#initialize accepting a
// trailing {capacity: n} keyword hash as a preallocation hint that rbgo ignores:
// it is stripped before the positional default is read, so it never becomes the
// default value and a following block still installs a default_proc. A trailing
// hash that is not exactly {capacity: …} stays a positional default value.
// Expectations match MRI Ruby 4.0 (ruby/spec core/hash/new).
func TestHashNewCapacityKwarg(t *testing.T) {
	cases := []struct{ src, want string }{
		// capacity: alone -> no positional default.
		{`p Hash.new(capacity: 42).default`, "nil\n"},
		// A positional default plus capacity: keeps the default.
		{`p Hash.new(5, capacity: 42).default`, "5\n"},
		// capacity: with a block installs the default_proc, no arity error.
		{`p Hash.new(capacity: 42) { 1 }.default_proc.class`, "Proc\n"},
		{`p Hash.new(capacity: 42) { |_h, k| k }[:z]`, ":z\n"},
		// Negative capacity is ignored, not an error.
		{`p Hash.new(capacity: -42).default`, "nil\n"},
		// A single-key hash that is not capacity stays a positional default
		// (rbgo cannot tell braces from keywords, so this mirrors the default
		// hash cases already relied on elsewhere).
		{`p Hash.new({foo: 1}).default`, "{foo: 1}\n"},
		// A multi-key hash is a positional default (Len != 1, not stripped).
		{`p Hash.new({a: 1, b: 2}).default`, "{a: 1, b: 2}\n"},
		// A non-hash last argument is a positional default (not a Hash).
		{`p Hash.new(5).default`, "5\n"},
		// No arguments at all (n == 0): nil default.
		{`p Hash.new.default`, "nil\n"},
		// #initialize routes through the same stripping.
		{`h = {}; h.send(:initialize, capacity: 8); p h.default`, "nil\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestHashAllocate covers Hash.allocate returning a fully-formed empty Hash (its
// #size is 0 and it is mutable) and, on a subclass, an instance that keeps the
// subclass identity while backed by a real Hash. Matches MRI Ruby 4.0
// (ruby/spec core/hash/allocate).
func TestHashAllocate(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p Hash.allocate.size`, "0\n"},
		{`h = Hash.allocate; h[:a] = 1; p(h == {a: 1})`, "true\n"},
		{`p Hash.allocate.instance_of?(Hash)`, "true\n"},
		// Subclass: identity preserved, backed by a working Hash.
		{`c = Class.new(Hash); h = c.allocate; h[:a] = 1; p [h.size, h.class == c]`, "[1, true]\n"},
		{`c = Class.new(Hash); p c.allocate.instance_of?(c)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestHashEachMutateDuringIteration covers Hash#each/#each_pair/#each_key/
// #each_value walking a snapshot so a delete/shift in the block neither
// double-visits nor skips a surviving key, while a key removed ahead of the
// cursor is skipped exactly as MRI does (ruby/spec core/hash/delete, shift).
func TestHashEachMutateDuringIteration(t *testing.T) {
	cases := []struct{ src, want string }{
		// Plain enumeration (the three closure bodies).
		{`ps = []; {a: 1, b: 2}.each { |k, v| ps << [k, v] }; p ps`, "[[:a, 1], [:b, 2]]\n"},
		{`ks = []; {a: 1, b: 2}.each_key { |k| ks << k }; p ks`, "[:a, :b]\n"},
		{`vs = []; {a: 1, b: 2}.each_value { |v| vs << v }; p vs`, "[1, 2]\n"},
		// Deleting the current key visits every key once and empties the hash.
		{`h = {a: 1, b: 2, c: 3, d: 4}; v = []; h.each_pair { |k, _| v << k; h.delete(k) }; p [v, h]`,
			"[[:a, :b, :c, :d], {}]\n"},
		// Shifting while iterating visits each original key once.
		{`h = {a: 1, b: 2, c: 3}; v = []; s = []; h.each_pair { |k, _| v << k; s << h.shift }; p [v, s, h]`,
			"[[:a, :b, :c], [[:a, 1], [:b, 2], [:c, 3]], {}]\n"},
		// A key removed ahead of the cursor is skipped (exercises the presence
		// check / continue in hashEachLive).
		{`h = {a: 1, b: 2, c: 3, d: 4}; v = []; h.each { |k, _| v << k; h.delete(:c) if k == :a }; p v`,
			"[:a, :b, :d]\n"},
		{`h = {a: 1, b: 2, c: 3}; v = []; h.each_key { |k| v << k; h.delete(:c) if k == :a }; p v`,
			"[:a, :b]\n"},
		{`h = {a: 1, b: 2, c: 3}; v = []; h.each_value { |val| v << val; h.delete(:c) if val == 1 }; p v`,
			"[1, 2]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestHashTransformKeysBangInPlace covers Hash#transform_keys! transforming one
// key at a time in place: a break leaves the already-processed keys rewritten and
// the rest untouched, a key rewritten onto an already re-keyed slot is preserved,
// and the surviving key order matches MRI (ruby/spec core/hash/transform_keys).
func TestHashTransformKeysBangInPlace(t *testing.T) {
	cases := []struct{ src, want string }{
		// break part-way: :a,:b rewritten, :c (break) and :d untouched.
		{`h = {a: 1, b: 2, c: 3, d: 4}; h.transform_keys! { |v| break if v == :c; v.succ }; p h`,
			"{b: 1, c: 2, d: 4}\n"},
		// Full transform: every key succ'd; the seen-set keeps a rewritten slot
		// from being deleted by a later key (exercises the "seen" true branch).
		{`h = {a: 1, b: 2, c: 3, d: 4}; h.transform_keys! { |v| v.succ }; p h`,
			"{b: 1, c: 2, d: 3, e: 4}\n"},
		// Mapping argument moves a key in place, keeping its original position.
		{`h = {a: 1, b: 2}; h.transform_keys!({a: :x}); p h`, "{x: 1, b: 2}\n"},
		{`h = {a: 1, b: 2, c: 3}; h.transform_keys!({b: :z}); p h`, "{a: 1, z: 2, c: 3}\n"},
		// All keys map onto one: last value wins, single entry.
		{`h = {a: 1, b: 2}; h.transform_keys! { |_| :z }; p h`, "{z: 2}\n"},
		// break on the very first key leaves the hash unchanged.
		{`h = {a: 1, b: 2, c: 3}; h.transform_keys! { |v| break if v == :a; v.succ }; p h`,
			"{a: 1, b: 2, c: 3}\n"},
		// No block and no mapping yields an Enumerator.
		{`p({a: 1}.transform_keys!.class)`, "Enumerator\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

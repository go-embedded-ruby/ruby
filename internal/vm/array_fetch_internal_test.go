// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestArrayFetch covers Array#fetch: in-bounds access, negative indices, the
// default-value form, the block form (which receives the original index object
// and supersedes a default), #to_int coercion, the out-of-bounds IndexError
// message, and the TypeError for a non-coercible argument. Verified against
// ruby 4.0.6.
func TestArrayFetch(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p [1, 2, 3].fetch(1)`, `2`},
		{`p [nil].fetch(0)`, `nil`},
		{`p [1, 2, 3, 4].fetch(-1)`, `4`},
		{`p [1, 2, 3].fetch(5, :d)`, `:d`},
		{`p [1, 2, 3].fetch(5, nil)`, `nil`},
		{`p [1, 2, 3].fetch(-4, :d)`, `:d`},
		{`p [1, 2, 3].fetch(9) { |i| i * i }`, `81`},
		{`p [1, 2, 3].fetch(-9) { |i| i * i }`, `81`},
		// The block receives the original index object, not the #to_int result.
		{`o = Object.new; def o.to_int; 5; end; p [1, 2, 3].fetch(o) { |i| i.equal?(o) }`, `true`},
		// The block supersedes a default argument.
		{`p [1, 2, 3].fetch(9, :foo) { |i| i * i }`, `81`},
		// #to_int coercion of an in-bounds index.
		{`o = Object.new; def o.to_int; 2; end; p ["a", "b", "c"].fetch(o)`, `"c"`},
		// Out-of-bounds IndexError messages use the original (pre-adjustment) index.
		{`begin; [1, 2, 3].fetch(3); rescue IndexError => e; p e.message; end`, `"index 3 outside of array bounds: -3...3"`},
		{`begin; [1, 2, 3].fetch(-4); rescue IndexError => e; p e.message; end`, `"index -4 outside of array bounds: -3...3"`},
		{`begin; [].fetch(0); rescue IndexError => e; p e.message; end`, `"index 0 outside of array bounds: 0...0"`},
		// A non-coercible argument raises TypeError naming its class.
		{`begin; [].fetch("cat"); rescue TypeError => e; p e.message; end`, `"no implicit conversion of String into Integer"`},
		{`begin; [1, 2, 3].fetch(1..2); rescue TypeError => e; p e.message; end`, `"no implicit conversion of Range into Integer"`},
		// Wrong arity.
		{`begin; [1, 2, 3].fetch; rescue ArgumentError => e; p e.class; end`, `ArgumentError`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}

// TestArrayFetchValues covers Array#fetch_values: results in requested order,
// negative indices, the no-argument empty result, the block form (which
// receives the original index), the out-of-bounds IndexError without a block,
// #to_int coercion, and the Range TypeError. Verified against ruby 4.0.6.
func TestArrayFetchValues(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p [:a, :b, :c].fetch_values(0, 2)`, `[:a, :c]`},
		{`p [:a, :b, :c].fetch_values(2, 0)`, `[:c, :a]`},
		{`p [:a, :b, :c].fetch_values(-1)`, `[:c]`},
		{`p [:a, :b, :c].fetch_values`, `[]`},
		{`p [:a, :b, :c].fetch_values(44) { |i| "x#{i}" }`, `["x44"]`},
		{`p [:a, :b, :c].fetch_values(0, 44) { |i| "x#{i}" }`, `[:a, "x44"]`},
		{`o = Object.new; def o.to_int; 2; end; p [:a, :b, :c].fetch_values(o)`, `[:c]`},
		{`begin; [:a, :b, :c].fetch_values(0, 1, 44); rescue IndexError => e; p e.message; end`, `"index 44 outside of array bounds: -3...3"`},
		{`begin; [:a, :b, :c].fetch_values(1..2); rescue TypeError => e; p e.message; end`, `"no implicit conversion of Range into Integer"`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}

// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestArrayAssoc covers Array#assoc and Array#rassoc: a match on the first
// (assoc) or second (rassoc) element, the first match winning, non-array and
// too-short elements being skipped, #to_ary coercion of non-array elements
// (including a #to_ary that yields a non-array, which is skipped), a
// user-defined == being dispatched, and nil when nothing matches. Verified
// against ruby 4.0.6.
func TestArrayAssoc(t *testing.T) {
	cases := []struct{ src, want string }{
		// assoc: first element compared, first match wins.
		{`p [["a", 1], ["b", 2], ["a", 3]].assoc("a")`, `["a", 1]`},
		{`p [["a", 1]].assoc("z")`, `nil`},
		// rassoc: second element compared.
		{`p [[1, "a"], [2, "b"]].rassoc("b")`, `[2, "b"]`},
		{`p [[1, "a"]].rassoc("z")`, `nil`},
		// Non-array elements and too-short arrays are skipped.
		{`p [1, 2, ["k", 9]].assoc("k")`, `["k", 9]`},
		{`p [[], ["k", 9]].assoc("k")`, `["k", 9]`},
		{`p [[1], [1, 2]].rassoc(2)`, `[1, 2]`},
		// #to_ary coercion of a non-array element; the coerced array is returned.
		{`class AC; def initialize(*x); @x = x; end; def to_ary; @x; end; end
p [AC.new("k", 7)].assoc("k")`, `["k", 7]`},
		{`class AC2; def initialize(*x); @x = x; end; def to_ary; @x; end; end
p [AC2.new(1, "v")].rassoc("v")`, `[1, "v"]`},
		// A #to_ary that returns a non-array is skipped (no match -> nil).
		{`class Bad; def to_ary; 42; end; end
p [Bad.new].assoc(1)`, `nil`},
		// A user-defined == on the compared element is dispatched.
		{`class Eqish; def ==(o); o == "match"; end; end
p [[Eqish.new, 1]].assoc("match").class`, `Array`},
		// Genuine arrays are returned by identity (never coerced via #to_ary).
		{`s = ["k", 1]; p [s].assoc("k").equal?(s)`, `true`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}

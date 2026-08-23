// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestRangeBsearch covers Range#bsearch: the find-minimum (true/false block) and
// find-any (numeric block) modes over integer and float ranges, inclusive vs
// exclusive endpoints, empty and reversed ranges, endless and beginless ranges,
// infinity bounds, the no-block Enumerator (with a nil #size), and the
// TypeErrors for a non-numeric bound or block result. Verified against ruby
// 4.0.6.
func TestRangeBsearch(t *testing.T) {
	cases := []struct{ src, want string }{
		// Integer, find-minimum.
		{`p (0..10).bsearch { |x| x >= 5 }`, `5`},
		{`p (0..10).bsearch { |x| x >= 100 }`, `nil`},
		{`p (0..10).bsearch { |x| true }`, `0`},
		{`p (0..10).bsearch { |x| false }`, `nil`},
		// Integer, find-any.
		{`p (0..10).bsearch { |x| 5 - x }`, `5`},
		{`p (0..10).bsearch { |x| 1 }`, `nil`},
		{`p (0..10).bsearch { |x| -1 }`, `nil`},
		// A nil block result behaves like false.
		{`p (0..10).bsearch { |x| nil }`, `nil`},
		// find-any exact hit while probing an endless / beginless bound, and a float.
		{`p (5..).bsearch { |x| 5 - x }`, `5`},
		{`p (..10).bsearch { |x| 10 - x }`, `10`},
		{`p (2.0..2.0).bsearch { |x| 0 }`, `2.0`},
		// A Bignum bound does not crash.
		{`p (0..10**20).bsearch { |x| x >= 5 }`, `5`},
		// Empty / reversed ranges.
		{`p (0...0).bsearch { true }`, `nil`},
		{`p (4..2).bsearch { true }`, `nil`},
		// Exclusive end.
		{`p (0...5).bsearch { |x| x >= 4 }`, `4`},
		// Float, find-minimum, inclusive/exclusive endpoints.
		{`p (1.0..3.0).bsearch { |x| x >= 3.0 }`, `3.0`},
		{`p (0..4.2).bsearch { |x| x >= 2 }`, `2.0`},
		{`p (-1.2..4.3).bsearch { |x| x >= 1 }`, `1.0`},
		{`p (0.1...2.3).bsearch { |x| x > 3 }`, `nil`},
		// Endless ranges.
		{`p (1..).bsearch { |x| x >= 5 }`, `5`},
		{`p (1.0..).bsearch { |x| x >= 5.5 }`, `5.5`},
		// Beginless ranges.
		{`p (..10).bsearch { |x| x >= 5 }`, `5`},
		{`p (..5.0).bsearch { |x| x >= 2 }`, `2.0`},
		// Infinity bounds.
		{`p (0..Float::INFINITY).bsearch { |x| x >= 3 }`, `3.0`},
		{`p (0..Float::INFINITY).bsearch { |x| x == Float::INFINITY }`, `Infinity`},
		{`p (0...Float::INFINITY).bsearch { |x| x == Float::INFINITY }`, `nil`},
		{`p (-Float::INFINITY..0).bsearch { |x| x >= -3 }`, `-3.0`},
		// No-block Enumerator with a nil size.
		{`p (0..1).bsearch.class`, `Enumerator`},
		{`p (0..1).bsearch.size`, `nil`},
		// TypeErrors.
		{`begin; (0..1).bsearch { Object.new }; rescue TypeError => e; p e.class; end`, `TypeError`},
		{`begin; ("a".."e").bsearch { true }; rescue TypeError => e; p e.message; end`, `"can't do binary search for String"`},
		{`begin; ("a".."e").bsearch; rescue TypeError => e; p e.class; end`, `TypeError`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}

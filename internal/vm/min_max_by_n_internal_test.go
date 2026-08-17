// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestMinMaxByN covers the n-argument form of Enumerable#min_by / #max_by:
// min_by(n) returns the n elements with the smallest block value in ascending
// order, max_by(n) the n largest in descending order. n clamps to the size, a
// nil n behaves like no argument (a single element), a negative n raises
// ArgumentError, and without a block an Enumerator is returned. Verified against
// ruby 4.0.6.
func TestMinMaxByN(t *testing.T) {
	cases := []struct{ src, want string }{
		// Array min_by(n) / max_by(n).
		{`p [1, 2, 3, 4, 5].min_by(2) { |x| x }`, `[1, 2]`},
		{`p [1, 2, 3, 4, 5].max_by(2) { |x| x }`, `[5, 4]`},
		{`p [1, 2, 3, 4, 5].max_by(2) { |x| -x }`, `[1, 2]`},
		// n larger than the collection returns all, sorted.
		{`p [3, 1, 2].min_by(5) { |x| x }`, `[1, 2, 3]`},
		// n == 0 returns an empty array.
		{`p [1, 2, 3].min_by(0) { |x| x }`, `[]`},
		// A nil n behaves like no argument (single element, not an array).
		{`p [3, 1, 2].min_by(nil) { |x| x }`, "1"},
		{`p [3, 1, 2].min_by { |x| x }`, "1"},
		// Any Enumerable gains it (Range, Hash) via the prelude.
		{`p (1..5).min_by(2) { |x| x }`, `[1, 2]`},
		{`p({ a: 3, b: 1, c: 2 }.min_by(2) { |k, v| v })`, `[[:b, 1], [:c, 2]]`},
		{`p({ a: 3, b: 1, c: 2 }.max_by(2) { |k, v| v })`, `[[:a, 3], [:c, 2]]`},
		// A negative n raises ArgumentError.
		{`p ([1].min_by(-1) { |x| x }) rescue p $!.class`, "ArgumentError"},
		{`p ([1].max_by(-1) { |x| x }) rescue p $!.class`, "ArgumentError"},
		// Without a block an Enumerator is returned (n is carried).
		{`p [1, 2, 3].min_by(2).class`, "Enumerator"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}

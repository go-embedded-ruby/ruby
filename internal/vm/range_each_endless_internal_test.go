// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestRangeEachEndless covers Range#each over an endless range, which cannot be
// materialised and so is iterated lazily: an Integer or Bignum begin counts up
// and a String begin walks by #succ, in both cases until the block breaks; a
// begin with no successor (e.g. a Float) raises a TypeError. Enumerable methods
// layered on #each (each_with_index, select, …) inherit the behaviour. Bounded
// ranges are unaffected. Verified against ruby 4.0.6.
func TestRangeEachEndless(t *testing.T) {
	cases := []struct{ src, want string }{
		// Endless Integer range counts up until the block breaks.
		{`a = []; (1..).each { |x| break if x > 4; a << x }; p a`, `[1, 2, 3, 4]`},
		{`a = []; (10..).each { |x| break if x > 12; a << x }; p a`, `[10, 11, 12]`},
		// Endless String range walks by #succ.
		{`a = []; ("a"..).each { |x| break if x > "c"; a << x }; p a`, `["a", "b", "c"]`},
		// A Bignum begin counts up too.
		{`a = []; (2**64..).each { |x| break if x > 2**64 + 2; a << x }; p a`, `[18446744073709551616, 18446744073709551617, 18446744073709551618]`},
		// Enumerable methods built on #each work on an endless range.
		{`a = []; (1..).each_with_index { |x, i| break if i > 2; a << x }; p a`, `[1, 2, 3]`},
		{`p (1..).lazy.select(&:even?).first(3)`, `[2, 4, 6]`},
		{`p (1..).first(4)`, `[1, 2, 3, 4]`},
		// A begin with no successor cannot be iterated.
		{`begin; (1.0..).each { |x| break }; rescue TypeError => e; p e.class; end`, `TypeError`},
		// Bounded ranges are unchanged and #each returns the range.
		{`a = []; (1..3).each { |x| a << x }; p a`, `[1, 2, 3]`},
		{`p (1..3).each { |x| }.class`, `Range`},
		// No block yields an Enumerator.
		{`p (1..).each.class`, `Enumerator`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}

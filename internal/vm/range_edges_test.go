// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestRangeCoverRange exercises Range#cover? with a Range argument (range-in-range
// containment) plus the value form and the sibling predicates that do NOT treat a
// Range specially (#include?/#member?/#===). Semantics mirror MRI 3.4/4.0.
func TestRangeCoverRange(t *testing.T) {
	tests := []struct{ name, src, want string }{
		// completely inside
		{"inside", `p((0..10).cover?(3..7))`, "true\n"},
		{"same_begin", `p((0..10).cover?(0..6))`, "true\n"},
		{"same_end", `p((0..10).cover?(4..10))`, "true\n"},
		{"identical", `p((0..10).cover?(0..10))`, "true\n"},
		{"identical_excl", `p((0...10).cover?(0...10))`, "true\n"},
		// exclusive-end fix-ups
		{"incl_covers_excl_same", `p((0..10).cover?(0...10))`, "true\n"},
		{"excl_not_incl_same", `p((0...10).cover?(0..10))`, "false\n"},
		{"excl_end_reducible", `p((0..10).cover?(4...11))`, "true\n"},
		{"excl_end_float_not_reducible", `p((0..10.0).cover?(5...11.0))`, "false\n"},
		{"excl_end_too_far", `p((0..10).cover?(4...100))`, "false\n"},
		{"widen_excl_self", `p((0...11).cover?(0..10))`, "true\n"},
		// beginless / endless self and other
		{"beginless_self", `p((...10).cover?(4..6))`, "true\n"},
		{"beginless_both", `p((...10).cover?(...6))`, "true\n"},
		{"beginless_excl_reducible", `p((..10).cover?(4...11))`, "true\n"},
		{"endless_self", `p((0..).cover?(4..6))`, "true\n"},
		{"endless_both_eq", `p((0..).cover?(0..))`, "true\n"},
		{"endless_both_excl_eq", `p((0...).cover?(4...))`, "true\n"},
		{"endless_excl_vs_incl", `p((0...).cover?(0..))`, "false\n"},
		{"nilnil_finite", `p((nil..nil).cover?(4..6))`, "true\n"},
		{"nilnil_endless", `p((nil..nil).cover?(4..))`, "true\n"},
		// bounded self cannot hold unbounded other
		{"bounded_vs_endless_other", `p((0..10).cover?(4..))`, "false\n"},
		{"bounded_vs_beginless_other", `p((0..10).cover?(..4))`, "false\n"},
		// interleaved / disjoint
		{"overlap_low", `p((4..10).cover?(0..6))`, "false\n"},
		{"disjoint_above", `p((6..10).cover?(0..4))`, "false\n"},
		{"disjoint_below", `p((0..4).cover?(6..10))`, "false\n"},
		{"endless_self_begin_too_high", `p((4..).cover?(0..6))`, "false\n"},
		// backward / empty other
		{"backward_other", `p((0..10).cover?(6..4))`, "false\n"},
		{"empty_excl_other", `p((0..10).cover?(4...4))`, "false\n"},
		// incomparable ends (rbgo permits construction MRI rejects)
		{"incomparable_ends", `p((..10).cover?(.."z"))`, "false\n"},
		{"incomparable_types", `p((0..10).cover?("a".."z"))`, "false\n"},
		// float boundaries
		{"float_inside", `p((1.1..7.9).cover?(2.5..6.5))`, "true\n"},
		{"float_over", `p((1.1..7.9).cover?(2.5..8.5))`, "false\n"},
		{"mixed_types", `p((0..10).cover?(3.1..7.9))`, "true\n"},
		// value form still works
		{"value_in", `p((0..10).cover?(5))`, "true\n"},
		{"value_excl_end", `p((1...5).cover?(5))`, "false\n"},
		{"value_excl_last", `p((1...5).cover?(4))`, "true\n"},
		{"value_below", `p((3..7).cover?(1))`, "false\n"},
		{"value_incomparable", `p((1..5).cover?("a"))`, "false\n"},
		{"value_endless", `p((1..).cover?(5))`, "true\n"},
		{"value_beginless_incomparable", `p((..5).cover?("a"))`, "false\n"},
		// include?/member?/=== do NOT do range containment
		{"include_range", `p((0..10).include?(3..7))`, "false\n"},
		{"member_range", `p((0..10).member?(3..7))`, "false\n"},
		{"tripleeq_range", `p((0..10) === (3..7))`, "false\n"},
		{"tripleeq_value", `p((0..10) === 5)`, "true\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestRangeSize covers Range#size with MRI 3.4/4.0 semantics: integer counts,
// endless/infinite -> Infinity, big counts, finite-Float ends, and the nil cases.
func TestRangeSize(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"int_incl", `p((1..16).size)`, "16\n"},
		{"int_excl", `p((1...16).size)`, "15\n"},
		{"backward", `p((16..0).size)`, "0\n"},
		{"endless", `p((1..).size)`, "Infinity\n"},
		{"int_inf_end", `p((0..Float::INFINITY).size)`, "Infinity\n"},
		{"string_begin", `p(("a".."z").size)`, "nil\n"},
		{"symbol_begin", `p((:a..:z).size)`, "nil\n"},
		{"string_endless", `p(("z"..).size)`, "nil\n"},
		{"float_end_incl", `p((1..3.3).size)`, "3\n"},
		{"float_end_excl", `p((1...3.3).size)`, "3\n"},
		{"float_end_exact_excl", `p((1...16.0).size)`, "15\n"},
		{"float_end_exact_incl", `p((1..16.0).size)`, "16\n"},
		{"float_end_below", `p((5..3.0).size)`, "0\n"},
		{"bignum", `p((1..(2**70)).size)`, "1180591620717411303424\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestRangeSizeErrors covers the size TypeError branches (Ruby 4.0 makes Float and
// nil begins non-iterable), naming the offending class.
func TestRangeSizeErrors(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"float_begin", `(1.0..16.0).size`, "can't iterate from Float"},
		{"beginless", `(..1).size`, "can't iterate from NilClass"},
		{"nilnil", `(nil..nil).size`, "can't iterate from NilClass"},
		{"nonint_end", `(1.."z").size`, "can't iterate from String"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := runErr(t, tc.src)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("src=%q: got err=%v, want containing %q", tc.src, err, tc.want)
			}
		})
	}
}

// TestRangeReverseEach covers Range#reverse_each: the block walk, the returned
// value, the sized Enumerator, and the entries alias.
func TestRangeReverseEach(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"int_incl", "a=[]\n(1..3).reverse_each{|i| a<<i}\np a", "[3, 2, 1]\n"},
		{"int_excl", "a=[]\n(1...3).reverse_each{|i| a<<i}\np a", "[2, 1]\n"},
		{"returns_self", "r=(1..3)\np r.reverse_each{|x| x}.equal?(r)", "true\n"},
		{"string", "a=[]\n('A'..'D').reverse_each{|i| a<<i}\np a", "[\"D\", \"C\", \"B\", \"A\"]\n"},
		{"enum_to_a", `p((1..3).reverse_each.to_a)`, "[3, 2, 1]\n"},
		{"enum_size", `p((1..3).reverse_each.size)`, "3\n"},
		{"enum_size_excl", `p((1...3).reverse_each.size)`, "2\n"},
		{"enum_size_float_end", `p((1..3.3).reverse_each.size)`, "3\n"},
		{"entries", `p((1..4).entries)`, "[1, 2, 3, 4]\n"},
		{"entries_string", `p(("a".."c").entries)`, "[\"a\", \"b\", \"c\"]\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestRangeReverseEachErrors covers the non-iterable reverse_each cases: an endless
// range and a begin that is neither Integer nor String, each naming its class.
func TestRangeReverseEachErrors(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"endless", `(1..).reverse_each.take(3)`, "can't iterate from NilClass"},
		{"float_begin", `(1.5..2.5).reverse_each{|x| x}`, "can't iterate from Float"},
		{"beginless", `(..5).reverse_each{|x| x}`, "can't iterate from NilClass"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := runErr(t, tc.src)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("src=%q: got err=%v, want containing %q", tc.src, err, tc.want)
			}
		})
	}
}

// TestRangeOverlap covers Range#overlap? — shared-element detection with
// inclusive/exclusive ends, unbounded sides, empty/backward ranges and the
// non-Range TypeError. Asserted against MRI Ruby 4.0.6.
func TestRangeOverlap(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p (1..5).overlap?(3..7)`, "true\n"},
		{`p (1..5).overlap?(6..8)`, "false\n"},
		{`p (1..5).overlap?(5..8)`, "true\n"},   // inclusive ends touch
		{`p (1...5).overlap?(5..8)`, "false\n"}, // exclusive end just misses
		{`p (1..5).overlap?(10..)`, "false\n"},  // endless other, disjoint
		{`p (..5).overlap?(3..)`, "true\n"},     // beginless meets endless
		{`p (1..3).overlap?(4..2)`, "false\n"},  // backward (empty) other
		{`p (1..5).overlap?(5..1)`, "false\n"},  // empty other touching self's end
		{`p (1...1).overlap?(0..2)`, "false\n"}, // empty self
		{`p ("a".."c").overlap?("b".."d")`, "true\n"},
		{`p (1.0..2.0).overlap?(1.5..3.0)`, "true\n"},
		{`p (1..5).overlap?("a".."z")`, "false\n"}, // incomparable bounds
		{`p ("a".."z").overlap?(1..5)`, "false\n"},
		{`p (1..).overlap?("a".."z")`, "false\n"}, // endless self, incomparable
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	if err := runErr(t, `(1..5).overlap?(3)`); err == nil ||
		!strings.Contains(err.Error(), "wrong argument type Integer (expected Range)") {
		t.Errorf("overlap?(non-Range): err=%v", err)
	}
}

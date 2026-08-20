// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestArrayRepeatedCombination covers Array#repeated_combination: the tuples
// themselves, sizes larger than the array, length 0 ([[]]), negative length
// (nothing), an empty receiver, Float truncation of the argument, the block form
// returning self, and the no-block Enumerator's combinatorial #size (including
// the empty-array edges). Verified against ruby 4.0.6.
func TestArrayRepeatedCombination(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p [1, 2, 3].repeated_combination(2).to_a`, `[[1, 1], [1, 2], [1, 3], [2, 2], [2, 3], [3, 3]]`},
		{`p [1, 2].repeated_combination(3).to_a`, `[[1, 1, 1], [1, 1, 2], [1, 2, 2], [2, 2, 2]]`},
		{`p [1, 2, 3].repeated_combination(0).to_a`, `[[]]`},
		{`p [1, 2, 3].repeated_combination(-1).to_a`, `[]`},
		{`p [].repeated_combination(0).to_a`, `[[]]`},
		{`p [].repeated_combination(2).to_a`, `[]`},
		{`p [1, 2].repeated_combination(2.9).to_a`, `[[1, 1], [1, 2], [2, 2]]`},
		{`r = []; [1, 2].repeated_combination(2) { |x| r << x }; p r`, `[[1, 1], [1, 2], [2, 2]]`},
		// The block form returns the receiver itself.
		{`a = [1, 2]; p a.repeated_combination(1) {}.equal?(a)`, `true`},
		// Enumerator#size = C(n + k - 1, k), with the empty-array edges.
		{`p [1, 2, 3].repeated_combination(2).size`, `6`},
		{`p [1, 2, 3].repeated_combination(5).size`, `21`},
		{`p [1, 2, 3].repeated_combination(0).size`, `1`},
		{`p [1, 2, 3].repeated_combination(-1).size`, `0`},
		{`p [].repeated_combination(0).size`, `1`},
		{`p [].repeated_combination(1).size`, `0`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}

// TestArrayRepeatedPermutation covers Array#repeated_permutation: the tuples
// (n**k of them, in lexicographic order), length 0, negative length, an empty
// receiver, the block form, the lazy Enumerator re-reading a mutated receiver,
// and the n**k #size (with empty-array edges). Verified against ruby 4.0.6.
func TestArrayRepeatedPermutation(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p [1, 2].repeated_permutation(2).to_a`, `[[1, 1], [1, 2], [2, 1], [2, 2]]`},
		{`p [1, 2].repeated_permutation(3).to_a`, `[[1, 1, 1], [1, 1, 2], [1, 2, 1], [1, 2, 2], [2, 1, 1], [2, 1, 2], [2, 2, 1], [2, 2, 2]]`},
		{`p [1, 2].repeated_permutation(0).to_a`, `[[]]`},
		{`p [1, 2].repeated_permutation(-1).to_a`, `[]`},
		{`p [].repeated_permutation(0).to_a`, `[[]]`},
		{`p [].repeated_permutation(3).to_a`, `[]`},
		{`p [1, 2].repeated_permutation(2.9).to_a`, `[[1, 1], [1, 2], [2, 1], [2, 2]]`},
		{`a = [1, 2]; p a.repeated_permutation(2) {}.equal?(a)`, `true`},
		{`r = []; [1, 2].repeated_permutation(2) { |x| r << x }; p r`, `[[1, 1], [1, 2], [2, 1], [2, 2]]`},
		// The no-block Enumerator re-reads the receiver when iterated.
		{`a = [1, 2, 3]; a.shift; e = a.repeated_permutation(2); a.unshift 1; p e.to_a.length`, `9`},
		{`p [1, 2, 3].repeated_permutation(2).size`, `9`},
		{`p [1, 2, 3].repeated_permutation(4).size`, `81`},
		{`p [1, 2, 3].repeated_permutation(0).size`, `1`},
		{`p [1, 2, 3].repeated_permutation(-1).size`, `0`},
		{`p [].repeated_permutation(0).size`, `1`},
		{`p [].repeated_permutation(2).size`, `0`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}

// TestEnumerableSort covers the Enumerable#sort added to the prelude: over a
// non-Array Enumerable (an Enumerator and a Range), with and without a
// comparison block. Verified against ruby 4.0.6.
func TestEnumerableSort(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p [3, 1, 2].each.sort`, `[1, 2, 3]`},
		{`p [3, 1, 2].each.sort { |a, b| b <=> a }`, `[3, 2, 1]`},
		{`p (1..5).sort { |a, b| b <=> a }`, `[5, 4, 3, 2, 1]`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}

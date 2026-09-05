// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestArrayAliasesAndArity covers the wave-12 residual fixes that align Array
// method identity and argument checking with MRI 4.0.6: #length is the same
// method record as #size, #clear/#map reject stray arguments, and Array.allocate
// yields a fully-formed empty Array while rejecting arguments.
func TestArrayAliasesAndArity(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		// #length is a true alias of #size (identical UnboundMethod record).
		{`p(Array.instance_method(:length) == Array.instance_method(:size))`, "true\n"},
		{`p([1,2,3].length)`, "3\n"},
		// Array.allocate is a real, mutable, empty Array.
		{`a = Array.allocate; p a.instance_of?(Array); p a.size; a << 1; p a`,
			"true\n0\n[1]\n"},
	})
}

// TestArrayArityErrors covers the ArgumentError paths added in wave 12: #clear
// and #map take no positional arguments, and Array.allocate takes none either.
func TestArrayArityErrors(t *testing.T) {
	for _, src := range []string{
		`[1].clear(true)`,
		`[1,2,3].map(:foo)`,
		`Array.allocate(1)`,
	} {
		if err := runErr(t, src); err == nil || !strings.Contains(err.Error(), "ArgumentError") {
			t.Errorf("src=%q expected ArgumentError, got %v", src, err)
		}
	}
}

// TestArrayArefArithmeticSequence covers Array#[] indexed by an
// Enumerator::ArithmeticSequence: positive and negative steps, endless and
// beginless ranges, exclusive endpoints, negative indices, an empty result, and
// the begin-past-end nil result. Every expectation matches MRI 4.0.6.
func TestArrayArefArithmeticSequence(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		{`p([0,1,2,3,4,5][(0..).step(2)])`, "[0, 2, 4]\n"},
		{`p([0,1,2,3,4,5][(2..).step(-1)])`, "[2, 1, 0]\n"},
		{`p([0,1,2,3,4,5][(...0).step(-1)])`, "[5, 4, 3, 2, 1]\n"},
		{`p([0,1,2,3,4,5][(..0).step(-1)])`, "[5, 4, 3, 2, 1, 0]\n"},
		{`p([0,1,2,3,4,5][(1...3).step(2)])`, "[1]\n"},
		{`p([0,1,2,3,4,5][(-4..4).step(2)])`, "[2, 4]\n"},
		{`p([0,1,2,3,4,5][(1..3).step(-1)])`, "[]\n"},  // positive range, negative step
		{`p([0,1,2,3,4,5][(6..).step(1)])`, "[]\n"},    // begin == length
		{`p([0,1,2,3,4,5][(7..).step(1)])`, "nil\n"},   // begin past the end (step > 0)
		{`p([0,1,2,3,4,5][(0..8).step(-1)])`, "nil\n"}, // begin past the end (step < 0)
		{`p([0,1,2,3,4,5][(9..).step(-1)])`, "[5, 4, 3, 2, 1, 0]\n"},
		{`p([0,1,2,3,4,5][(-3..).step(-2)])`, "[3, 1]\n"},
	})
}

// TestArrayArefArithSeqErrors covers the RangeError guard for a strided slice
// whose declared span reaches past the array (|step| > 1) and the ArgumentError
// for a step that truncates to zero (a fractional step). Verified against MRI.
func TestArrayArefArithSeqErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		{`[0,1,2,3,4,5][(0..6).step(2)]`, "RangeError"},
		{`[0,1,2,3,4,5][(2..8).step(2)]`, "RangeError"},
		{`[0,1,2,3,4,5][(8..2).step(-2)]`, "RangeError"},
		{`[0,1,2,3,4,5][(7..).step(2)]`, "RangeError"},
		{`[0,1,2,3,4,5][(0..5).step(0.5)]`, "slice step cannot be zero"},
	}
	for _, c := range cases {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q expected %q, got %v", c.src, c.want, err)
		}
	}
}

// TestArrayArefRangeAndIndexEdges covers the remaining Array#[] residuals: a user
// Range subclass slices like a plain Range, a Range with a length argument is a
// TypeError, and an index or length beyond the Fixnum range raises RangeError
// (while an ordinary Float index truncates). Verified against MRI 4.0.6.
func TestArrayArefRangeAndIndexEdges(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		{`class MyR < Range; end; p([1,2,3,4][MyR.new(1,2)])`, "[2, 3]\n"},
		{`class MyR2 < Range; end; p([1,2,3,4][MyR2.new(-3,-1,true)])`, "[2, 3]\n"},
		{`p([10,20,30][1.9])`, "20\n"}, // ordinary Float index truncates
	})
	errCases := []struct{ src, want string }{
		{`[1,2,3][1..2, 1]`, "TypeError"},
		{`[1,2,3,4,5,6][2.0**63]`, "RangeError"},
		{`[1,2,3,4,5,6][1, 8e19]`, "RangeError"},
		{`[1,2,3,4,5,6][1, -8e19]`, "RangeError"},
	}
	for _, c := range errCases {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q expected %q, got %v", c.src, c.want, err)
		}
	}
}

// TestArrayEnumeratorUnknownSize covers the wave-12 fix that gives the block-less
// #bsearch / #bsearch_index / #rindex Enumerators an unknown (nil) size, as MRI
// does, while the ordinary #map Enumerator keeps its known size.
func TestArrayEnumeratorUnknownSize(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		{`p([1,2,3].bsearch.size)`, "nil\n"},
		{`p([1,2,3].bsearch_index.size)`, "nil\n"},
		{`p([1,2,3].rindex.size)`, "nil\n"},
		{`p([1,2,3].map.size)`, "3\n"},
		// The unsized Enumerators still drive their search when iterated.
		{`p([1,2,3,4].bsearch.each { |x| x >= 3 })`, "3\n"},
		{`p([1,2,3,4].bsearch_index.each { |x| x >= 3 })`, "2\n"},
	})
}

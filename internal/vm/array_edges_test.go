// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// TestArrayBsearch covers Array#bsearch in both MRI modes. find-minimum: the
// block returns true/false/nil and the first satisfying element is returned (nil
// when none satisfies). find-any: the block returns a Numeric and the element for
// which it returns 0 is returned (nil when the search converges without a hit,
// exercising both the positive→left and negative→right branches). Verified
// against MRI Ruby 4.0.
func TestArrayBsearch(t *testing.T) {
	cases := []struct{ src, want string }{
		// find-minimum mode (true/false).
		{`p([0, 4, 7, 10, 12].bsearch { |x| x >= 4 })`, "4\n"},
		{`p([0, 4, 7, 10, 12].bsearch { |x| x >= 6 })`, "7\n"},
		{`p([0, 4, 7, 10, 12].bsearch { |x| x >= 100 })`, "nil\n"},
		{`p([1, 2, 3, 4].bsearch { |x| x >= 3 })`, "3\n"}, // block returns false then true
		{`p([].bsearch { |x| true })`, "nil\n"},           // empty → nil
		// find-minimum with nil (behaves like false → look right).
		{`p([1, 2, 3, 4].bsearch { |x| x >= 3 ? true : nil })`, "3\n"},
		// find-any mode (Numeric): 0 hits, positive→left, negative→right.
		{`p([1, 3, 5, 7].bsearch { |x| x - 5 })`, "5\n"},   // exact hit
		{`p([1, 3, 7, 9].bsearch { |x| x - 5 })`, "nil\n"}, // converges without a hit
		{`p([0, 4, 7, 10, 12].bsearch { |x| 1 - x / 4 })`, "7\n"},
		{`p([0, 4, 7, 10, 12].bsearch { |x| 4 - x / 2 })`, "nil\n"},
		// Float block results are numeric too (find-any exact hit returns the Float).
		{`p([1.0, 2.0, 3.0].bsearch { |x| 2.0 - x })`, "2.0\n"},
		// bsearch_index projects the index (or nil).
		{`p([0, 4, 7, 10, 12].bsearch_index { |x| x >= 6 })`, "2\n"},
		{`p([0, 4, 7, 10, 12].bsearch_index { |x| x >= 100 })`, "nil\n"},
		{`p([1, 3, 5, 7].bsearch_index { |x| x - 5 })`, "2\n"},
		{`p([1, 3, 7, 9].bsearch_index { |x| x - 5 })`, "nil\n"},
		// Without a block, both return an Enumerator.
		{`p([1, 2, 3].bsearch.class)`, "Enumerator\n"},
		{`p([1, 2, 3].bsearch_index.class)`, "Enumerator\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestArrayBsearchErrors covers the TypeError branch when the block returns a
// value that is neither numeric nor true/false/nil.
func TestArrayBsearchErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		{`begin; [1, 2, 3].bsearch { |x| "foo" }; rescue TypeError => e; puts e.message; end`,
			"wrong argument type String (must be numeric, true, false or nil)\n"},
		{`begin; [1, 2, 3].bsearch_index { |x| :sym }; rescue TypeError; puts "TE"; end`, "TE\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestArraySetOps covers the multi-argument set operations #difference, #union and
// #intersection plus the #intersect? predicate. All compare with eql? (so 1 and
// 1.0 are distinct), difference preserves the receiver's duplicates, while union
// and intersection de-duplicate keeping first-seen order. Verified against MRI
// Ruby 4.0.
func TestArraySetOps(t *testing.T) {
	cases := []struct{ src, want string }{
		// difference: keeps receiver duplicates, removes eql? matches from any arg.
		{`p([1, 1, 2, 2, 3, 3, 4, 5].difference([1, 2, 4]))`, "[3, 3, 5]\n"},
		{`p([1, 2, 3].difference([2], [3]))`, "[1]\n"},
		{`p([1, 2, 3].difference)`, "[1, 2, 3]\n"},  // no args → copy
		{`p([1, 2].difference([1.0]))`, "[1, 2]\n"}, // eql?: 1 != 1.0
		// union: concatenate then de-duplicate.
		{`p([1, 2, 3].union([3, 4], [5, 1]))`, "[1, 2, 3, 4, 5]\n"},
		{`p([1, 1, 3, 5].union)`, "[1, 3, 5]\n"}, // no args → uniq
		{`p([1, 2].union)`, "[1, 2]\n"},
		// intersection: elements common to receiver and every arg, de-duplicated.
		{`p([1, 2, 3, 4].intersection([2, 3, 4], [3, 4, 5]))`, "[3, 4]\n"},
		{`p([1, 1, 2, 3].intersection([1, 2, 3]))`, "[1, 2, 3]\n"}, // result de-duplicated
		{`p([1, 2, 3].intersection)`, "[1, 2, 3]\n"},               // no args → uniq
		{`p([1, 2].intersection([1.0]))`, "[]\n"},                  // eql?: no overlap
		// intersect? predicate.
		{`p([1, 2, 3].intersect?([3, 4]))`, "true\n"},
		{`p([1, 2, 3].intersect?([4, 5]))`, "false\n"},
		{`p([1, 2].intersect?([1.0]))`, "false\n"}, // eql?
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestArraySetOpsErrors covers the TypeError branches when a set operation is
// given a non-Array argument.
func TestArraySetOpsErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		{`begin; [1, 2, 3].difference(1); rescue TypeError; puts "TE"; end`, "TE\n"},
		{`begin; [1, 2, 3].union(1); rescue TypeError; puts "TE"; end`, "TE\n"},
		{`begin; [1, 2, 3].intersection(1); rescue TypeError; puts "TE"; end`, "TE\n"},
		{`begin; [1, 2, 3].intersect?(1); rescue TypeError; puts "TE"; end`, "TE\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestArrayEachIndex covers Array#each_index: the block form yields every valid
// index in order and returns the receiver, while the no-block form returns a
// sized Enumerator.
func TestArrayEachIndex(t *testing.T) {
	cases := []struct{ src, want string }{
		{`r = []; [10, 20, 30].each_index { |i| r << i }; p(r)`, "[0, 1, 2]\n"},
		{`a = [10, 20, 30]; p(a.each_index { |i| }.equal?(a))`, "true\n"}, // returns self
		{`p([10, 20, 30].each_index.to_a)`, "[0, 1, 2]\n"},
		{`p([10, 20, 30].each_index.class)`, "Enumerator\n"},
		{`p([10, 20, 30].each_index.size)`, "3\n"},
		{`p([].each_index.size)`, "0\n"},
		{`r = []; [].each_index { |i| r << i }; p(r)`, "[]\n"}, // empty: no yields
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestArrayCycle covers Array#cycle: bounded (n given) and unbounded (n omitted or
// nil, ended by break) block forms, the no-op paths for empty arrays and
// non-positive counts, #to_int coercion of the count, and the no-block sized
// Enumerator (n*length, 0 for non-positive, infinity for unbounded). Verified
// against MRI Ruby 4.0.
func TestArrayCycle(t *testing.T) {
	cases := []struct{ src, want string }{
		// Bounded block form.
		{`r = []; [1, 2, 3].cycle(2) { |x| r << x }; p(r)`, "[1, 2, 3, 1, 2, 3]\n"},
		{`p([1, 2, 3].cycle(2) { |x| })`, "nil\n"}, // returns nil when a finite cycle completes
		// Non-positive / empty no-ops return nil without yielding.
		{`r = []; [1, 2, 3].cycle(0) { |x| r << x }; p([r, [1, 2, 3].cycle(0) { |x| }])`, "[[], nil]\n"},
		{`r = []; [1, 2, 3].cycle(-1) { |x| r << x }; p(r)`, "[]\n"},
		{`p([].cycle { |x| })`, "nil\n"}, // empty unbounded → nil immediately
		// Unbounded forms ended by break (n omitted, and explicit nil).
		{`r = []; [1, 2].cycle { |x| r << x; break if r.size >= 5 }; p(r)`, "[1, 2, 1, 2, 1]\n"},
		{`r = []; [1, 2].cycle(nil) { |x| r << x; break if r.size >= 3 }; p(r)`, "[1, 2, 1]\n"},
		// #to_int coercion of the count.
		{`o = Object.new; def o.to_int; 2; end; r = []; [1, 2].cycle(o) { |x| r << x }; p(r)`,
			"[1, 2, 1, 2]\n"},
		// No-block sized Enumerator.
		{`p([1, 2, 3].cycle.class)`, "Enumerator\n"},
		{`p([1, 2, 3].cycle(2).to_a)`, "[1, 2, 3, 1, 2, 3]\n"},
		{`p([1, 2, 3].cycle(2).size)`, "6\n"},
		{`p([1, 2, 3].cycle.size)`, "Infinity\n"},                                   // unbounded
		{`p([1, 2, 3].cycle(0).size)`, "0\n"},                                       // non-positive count
		{`p([1, 2, 3].cycle(-4).size)`, "0\n"},                                      // negative count
		{`p([].cycle.size)`, "0\n"},                                                 // empty, unbounded
		{`p([].cycle(3).size)`, "0\n"},                                              // empty, bounded
		{`o = Object.new; def o.to_int; 2; end; p([1, 2, 3].cycle(o).size)`, "6\n"}, // coerced size
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

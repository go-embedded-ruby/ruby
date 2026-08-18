// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestRangeMinMax covers Range#min / #max / #minmax computed from the endpoints
// (without iterating, so Float ranges work): the empty, inclusive, exclusive
// (Integer, Bignum, String, Float end), beginless and endless cases, the block
// form, and the n-argument form's beginless/endless guards. Verified against
// ruby 4.0.6.
func TestRangeMinMax(t *testing.T) {
	cases := []struct{ src, want string }{
		// Integer ranges.
		{`p (1..10).min`, `1`},
		{`p (1..10).max`, `10`},
		{`p (1...10).max`, `9`},
		{`p (0...2**64).max`, `18446744073709551615`}, // Bignum end excludes to end-1
		{`p (1..10).minmax`, `[1, 10]`},
		// Float ranges are computed, not iterated.
		{`p (1.0..2.0).min`, `1.0`},
		{`p (1.0..2.0).max`, `2.0`},
		{`p (1.0..2.0).minmax`, `[1.0, 2.0]`},
		// Empty ranges yield nil.
		{`p (5..1).min`, `nil`},
		{`p (5..1).max`, `nil`},
		{`p (5...5).max`, `nil`},
		{`p (5..1).minmax`, `[nil, nil]`},
		// Inclusive keeps the endpoint even when begin == end.
		{`p (5..5).max`, `5`},
		// String ranges: inclusive is the end, exclusive iterates via #succ.
		{`p ("a".."f").max`, `"f"`},
		{`p ("a"..."f").max`, `"e"`},
		{`p ("a".."f").min`, `"a"`},
		// Beginless/endless.
		{`p (1..).min`, `1`},
		{`p (..5).max`, `5`},
		// The n-argument and block forms still work.
		{`p (1..10).min(2)`, `[1, 2]`},
		{`p (1..10).max(2)`, `[10, 9]`},
		{`p (1..5).max { |a, b| a <=> b }`, `5`},
		{`p (1..5).min { |a, b| a <=> b }`, `1`},
		{`p (5..1).min { |a, b| a <=> b }`, `nil`}, // empty with a block
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
	// Error cases across every guarded branch.
	errs := []struct{ src, cls string }{
		{`(1..).max`, "RangeError"},      // endless max
		{`(..5).min`, "RangeError"},      // beginless min
		{`(1..).max(2)`, "RangeError"},   // endless max(n)
		{`(..5).min(2)`, "RangeError"},   // beginless min(n)
		{`(1.0...2.0).max`, "TypeError"}, // exclusive numeric end
		{`(...1.0).max`, "TypeError"},    // exclusive beginless numeric end
	}
	for _, c := range errs {
		if got := eval(t, `p ((`+c.src+`; :no) rescue $!.class)`); got != c.cls+"\n" {
			t.Errorf("%s: got=%q want %s", c.src, got, c.cls)
		}
	}
}

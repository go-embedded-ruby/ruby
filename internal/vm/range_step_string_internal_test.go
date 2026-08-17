// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestRangeStepString covers Range#step over a String range: it walks begin..end
// by #succ and yields every step-th element, for both bounded and endless ranges
// (the latter runs forever until the block breaks). MRI requires an Integer step
// (a Float or other type raises TypeError) and rejects a zero or negative step.
// Verified against ruby 4.0.6.
func TestRangeStepString(t *testing.T) {
	cases := []struct{ src, want string }{
		// Bounded ranges, default and explicit Integer step.
		{`a = []; ("A".."E").step { |x| a << x }; p a`, `["A", "B", "C", "D", "E"]`},
		{`a = []; ("A".."G").step(2) { |x| a << x }; p a`, `["A", "C", "E", "G"]`},
		{`a = []; ("a".."e").step(2) { |x| a << x }; p a`, `["a", "c", "e"]`},
		// Exclusive end.
		{`a = []; ("A"..."E").step { |x| a << x }; p a`, `["A", "B", "C", "D"]`},
		// Endless ranges walk forever; the block breaks.
		{`a = []; ("A"..).step { |x| break if x > "D"; a << x }; p a`, `["A", "B", "C", "D"]`},
		{`a = []; ("A"...).step { |x| break if x > "D"; a << x }; p a`, `["A", "B", "C", "D"]`},
		{`a = []; ("A"..).step(2) { |x| break if x > "F"; a << x }; p a`, `["A", "C", "E"]`},
		// A Float or other non-Integer step raises TypeError.
		{`p (("A".."G").step(2.0) {}) rescue p $!.class`, "TypeError"},
		{`p (("A".."G").step([]) {}) rescue p $!.class`, "TypeError"},
		{`p (("A"..).step(2.0) {}) rescue p $!.class`, "TypeError"},
		// A zero or negative step is rejected.
		{`p (("A".."G").step(0) {}) rescue p $!.class`, "ArgumentError"},
		{`p (("A".."G").step(-1) {}) rescue p $!.class`, "ArgumentError"},
		// A numeric range is unaffected (still uses the arithmetic path).
		{`a = []; (1..7).step(2) { |x| a << x }; p a`, `[1, 3, 5, 7]`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}

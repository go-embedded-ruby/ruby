// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestRangeStepEndlessNumeric covers Range#step over an endless numeric range
// (m..): it walks lo, lo+step, lo+2*step, … forever until the block breaks.
// Integer begin with an Integer step yields Integers; any Float participant
// yields Floats computed as lo+i*step (not accumulated). A zero step raises
// ArgumentError and a non-numeric begin raises TypeError. Verified against
// ruby 4.0.6.
func TestRangeStepEndlessNumeric(t *testing.T) {
	cases := []struct{ src, want string }{
		// Integer begin, default and Integer step → Integer values.
		{`a = []; (-2..).step { |x| break if x > 2; a << x }; p a`, `[-2, -1, 0, 1, 2]`},
		{`a = []; (-5..).step(2) { |x| break if x > 3; a << x }; p a`, `[-5, -3, -1, 1, 3]`},
		{`a = []; (-2...).step { |x| break if x > 2; a << x }; p a`, `[-2, -1, 0, 1, 2]`},
		// Integer begin, Float step → Float values.
		{`a = []; (-2..).step(1.5) { |x| break if x > 1.0; a << x }; p a`, `[-2.0, -0.5, 1.0]`},
		// Float begin → Float values.
		{`a = []; (-2.0..).step { |x| break if x > 1.5; a << x }; p a`, `[-2.0, -1.0, 0.0, 1.0]`},
		{`a = []; (-5.0..).step(2) { |x| break if x > 3.5; a << x }; p a`, `[-5.0, -3.0, -1.0, 1.0, 3.0]`},
		// Values are computed independently (float error does not accumulate).
		{`a = []; (0.0..).step(0.1) { |x| a << x; break if a.size == 4 }; p a`, `[0.0, 0.1, 0.2, 0.30000000000000004]`},
		// Infinite begin stays infinite.
		{`a = []; (-Float::INFINITY..).step(2) { |x| a << x; break if a.size == 3 }; p a`, `[-Infinity, -Infinity, -Infinity]`},
		// A zero step raises ArgumentError (Integer and Float paths).
		{`p ((1..).step(0) { |x| }) rescue p $!.class`, "ArgumentError"},
		{`p ((1.0..).step(0.0) { |x| }) rescue p $!.class`, "ArgumentError"},
		// A non-numeric begin cannot be stepped.
		{`p ((:a..).step(1) { |x| }) rescue p $!.class`, "TypeError"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}

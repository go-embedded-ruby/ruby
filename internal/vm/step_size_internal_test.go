// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestStepEnumeratorKnowsItsSize covers Integer#step's enumerator reporting how
// many values it will yield instead of being counted.
//
// Enumerator#size falls back to enumerating whatever cannot tell it its own
// length. That is right for a sequence that ends and fatal for one that does
// not: 1.step(Float::INFINITY).size allocated 30.9 GB and killed a CI runner,
// one of the three reasons the ruby/spec ratchet lane was red for three days.
//
// Verified against ruby 4.0.6.
func TestStepEnumeratorKnowsItsSize(t *testing.T) {
	cases := []struct{ src, want string }{
		// The case that used to take the machine.
		{`p 1.step(Float::INFINITY).size`, `Infinity`},
		{`p 1.step(Float::INFINITY, 2).size`, `Infinity`},
		// Travelling away from an endless limit yields nothing.
		{`p 1.step(-Float::INFINITY).size`, `0`},
		// Finite sequences count exactly, and the count is not an estimate:
		// it agrees with what the block form yields.
		{`p 1.step(10).size`, `10`},
		{`p 1.step(10, 3).size`, `4`},
		{`n = 0; 1.step(10, 3) { n += 1 }; p n`, `4`},
		{`p 1.step(10, 100).size`, `1`},
		{`p 10.step(1).size`, `0`},
		{`p 10.step(1, -3).size`, `4`},
		{`n = 0; 10.step(1, -3) { n += 1 }; p n`, `4`},
		{`p 1.step(1).size`, `1`},
		// A step of zero raises, and asking the size is enough to get the
		// error — MRI does not wait for the sequence to be run.
		{`begin; 1.step(10, 0).size; rescue ArgumentError => e; p e.message; end`,
			`"step can't be 0"`},
		{`begin; 1.step(10, 0) { }; rescue ArgumentError => e; p e.message; end`,
			`"step can't be 0"`},
		// The enumerator still yields exactly what it always did, floats and
		// all — a Float limit makes the sequence a Float one, which is MRI's
		// behaviour and not something this changes.
		{`p 1.step(7, 2).to_a`, `[1, 3, 5, 7]`},
		{`p 1.step(Float::INFINITY).first(4)`, `[1.0, 2.0, 3.0, 4.0]`},

		// Range#step has the same enumerator and the same hazard:
		// (1..Float::INFINITY).step is the other half of what killed the
		// runner, in core/enumerator/arithmetic_sequence/size_spec.rb.
		{`p (1..Float::INFINITY).step.size`, `Infinity`},
		{`p (1..10).step.size`, `10`},
		{`p (1..10).step(3).size`, `4`},
		// An excluded end is one short, and only where the step lands on it.
		{`p (1...10).step.size`, `9`},
		{`p (1...10).step(3).size`, `3`},
		{`p (1..10).step(3).to_a`, `[1, 4, 7, 10]`},
		{`p (1...10).step(3).to_a`, `[1, 4, 7]`},
		// A String range steps by #succ, which has no size to give.
		{`p ("a".."e").step(2).size`, `nil`},
		// An endless range is endless whatever it holds.
		{`p (1..).step(2).size`, `Infinity`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}

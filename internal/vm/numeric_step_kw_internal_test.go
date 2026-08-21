// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestNumericStepKeyword covers the keyword (to:/by:) and mixed argument forms of
// Integer#step and Float#step, an unbounded (endless) walk when no limit is
// given, the combinatorial Enumerator#size (including the Float::INFINITY edges),
// an infinite step yielding the start once, and the ArgumentError validation
// (zero step, a String step, a limit or step given both positionally and by
// keyword, an unknown keyword). Verified against ruby 4.0.6.
func TestNumericStepKeyword(t *testing.T) {
	cases := []struct{ src, want string }{
		// Keyword forms.
		{`p 1.step(by: 2, to: 10).to_a`, `[1, 3, 5, 7, 9]`},
		{`p 1.step(to: 5).to_a`, `[1, 2, 3, 4, 5]`},
		{`p 10.step(by: -2, to: 1).to_a`, `[10, 8, 6, 4, 2]`},
		{`p 1.5.step(by: 0.5, to: 3).to_a`, `[1.5, 2.0, 2.5, 3.0]`},
		// Mixed positional limit + keyword step.
		{`p 1.step(10, by: 3).to_a`, `[1, 4, 7, 10]`},
		// Endless (no limit): the block breaks out; the enumerator is infinite.
		{`r = []; 1.step(by: 2) { |x| r << x; break if x >= 5 }; p r`, `[1, 3, 5]`},
		{`p 1.step(by: 2).first(3)`, `[1, 3, 5]`},
		{`p 1.5.step(by: 0.5).first(3)`, `[1.5, 2.0, 2.5]`},
		// Bare step (no args) defaults to an unbounded step of 1.
		{`r = []; 1.step { |x| r << x; break if x >= 4 }; p r`, `[1, 2, 3, 4]`},
		// Enumerator#size, including the Float::INFINITY edges.
		{`p 1.step(by: 2, to: 10).size`, `5`},
		{`p 1.step(by: 42).size`, `Infinity`},
		{`p 1.step(to: Float::INFINITY, by: 42).size`, `Infinity`},
		{`p 1.step(to: -Float::INFINITY, by: -42).size`, `Infinity`},
		{`p 1.step(to: Float::INFINITY, by: Float::INFINITY).size`, `1`},
		{`p 1.step(to: -Float::INFINITY, by: -Float::INFINITY).size`, `1`},
		{`p 1.step(to: 10, by: Float::INFINITY).size`, `1`},
		{`p 1.step(to: -10, by: Float::INFINITY).size`, `0`},
		{`p 1.step(to: Float::INFINITY, by: -42).size`, `0`},
		// An unreachable limit (walking the wrong way) has size 0.
		{`p 1.step(to: -5).size`, `0`},
		{`p 1.step(to: -5, by: -1).size`, `7`},
		// An infinite step yields the start value exactly once.
		{`r = []; (1.0/0).step(1.0/0, 1.0/0) { |x| r << x }; p r`, `[Infinity]`},
		// Validation: zero step, a String step, doubled limit/step, unknown keyword.
		{`begin; 1.step(5, by: 0) {}; rescue ArgumentError => e; p e.class; end`, `ArgumentError`},
		{`begin; 1.step(2, by: 0.0) {}; rescue ArgumentError => e; p e.class; end`, `ArgumentError`},
		{`begin; 1.step(5, "2") {}; rescue ArgumentError => e; p e.class; end`, `ArgumentError`},
		{`begin; 1.step(5, "2").size; rescue ArgumentError => e; p e.class; end`, `ArgumentError`},
		{`begin; 1.step(5, 1, to: 5) {}; rescue ArgumentError => e; p e.class; end`, `ArgumentError`},
		{`begin; 1.step(5, 1, by: 5) {}; rescue ArgumentError => e; p e.class; end`, `ArgumentError`},
		{`begin; 1.step(foo: 1) {}; rescue ArgumentError => e; p e.class; end`, `ArgumentError`},
		{`begin; 1.step(1, 2, 3) {}; rescue ArgumentError => e; p e.class; end`, `ArgumentError`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}

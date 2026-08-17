// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestEnumerableCycleSize covers Enumerable#cycle's Enumerator reporting its size
// without materialising the (endless) cycle: Float::INFINITY for an unbounded
// cycle of a non-empty sized receiver, size*n for a bounded one, 0 for an empty
// receiver or a non-positive n, and nil when the receiver has no #size. Verified
// against ruby 4.0.6.
func TestEnumerableCycleSize(t *testing.T) {
	// A custom Enumerable that knows its size.
	sized := "class CSized; include Enumerable; def each; yield 1; yield 2; yield 3; end; def size; 3; end; end; "
	empty := "class CEmpty; include Enumerable; def each; end; def size; 0; end; end; "
	nosize := "class CNoSize; include Enumerable; def each; yield 1; end; end; "
	cases := []struct{ src, want string }{
		// Unbounded cycle of a non-empty sized receiver → Infinity (no hang).
		{sized + `p CSized.new.cycle.size`, "Infinity"},
		// Bounded cycle → size * n.
		{sized + `p CSized.new.cycle(2).size`, "6"},
		// Non-positive n → 0.
		{sized + `p CSized.new.cycle(0).size`, "0"},
		{sized + `p CSized.new.cycle(-3).size`, "0"},
		// Empty receiver → 0 even unbounded.
		{empty + `p CEmpty.new.cycle.size`, "0"},
		// No #size → unknown (nil).
		{nosize + `p CNoSize.new.cycle.size`, "nil"},
		// The block form still iterates correctly (unchanged).
		{sized + `a = []; CSized.new.cycle(2) { |x| a << x }; p a`, `[1, 2, 3, 1, 2, 3]`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}

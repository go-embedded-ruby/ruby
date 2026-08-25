// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestIntegerBitwiseCoerce covers the coerce protocol of Integer#&, #| and #^:
// an Integer or Bignum operand is combined directly, any other operand that
// defines #coerce is coerced (rhs.coerce(self) -> [x, y], then the operator is
// re-dispatched on the pair), and a Float — or an operand without #coerce —
// raises a TypeError. Verified against ruby 4.0.6.
func TestIntegerBitwiseCoerce(t *testing.T) {
	const c = `class C; def coerce(o); [o, 5]; end; end; `
	cases := []struct{ src, want string }{
		// Plain integer operands still work, promoting to Bignum as needed.
		{`p 6 & 3`, `2`},
		{`p 6 | 3`, `7`},
		{`p 6 ^ 3`, `5`},
		{`p (2**64) | 1`, `18446744073709551617`},
		{`p (2**70) & (2**70 - 1)`, `0`},
		// A non-Integer operand with #coerce is coerced, then re-dispatched.
		{c + `p 6 | C.new`, `7`},
		{c + `p 6 & C.new`, `4`},
		{c + `p 3 ^ C.new`, `6`},
		// A Float never runs the coerce protocol here; it is a TypeError.
		{`begin; 3 | 3.4; rescue TypeError => e; p e.class; end`, `TypeError`},
		{`begin; 3 & 3.4; rescue TypeError => e; p e.class; end`, `TypeError`},
		// An operand without #coerce is a TypeError naming its class.
		{`begin; 3 | Object.new; rescue TypeError => e; p e.message; end`, `"Object can't be coerced into Integer"`},
		{`class D; end; begin; 3 ^ D.new; rescue TypeError => e; p e.message; end`, `"D can't be coerced into Integer"`},
		{`begin; 3 & "x"; rescue TypeError => e; p e.class; end`, `TypeError`},
		// A #coerce that does not return a two-element array is a TypeError.
		{`class E; def coerce(o); [1]; end; end; begin; 3 | E.new; rescue TypeError => e; p e.message; end`, `"coerce must return [x, y]"`},
	}
	for _, tc := range cases {
		if got := eval(t, tc.src); got != tc.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want+"\n")
		}
	}
}

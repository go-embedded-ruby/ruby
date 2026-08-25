// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestIntegerShift covers Integer#<< and Integer#>> for shift amounts that a
// machine int cannot hold and for a #to_int-coercible argument, alongside the
// ordinary small/Bignum shifts. A shift amount beyond a machine int is
// degenerate: a huge right shift collapses to 0 (or -1 for a negative receiver),
// a huge left shift of zero stays 0, and any other huge left shift raises
// RangeError. Verified against ruby 4.0.6.
func TestIntegerShift(t *testing.T) {
	cases := []struct{ src, want string }{
		// Ordinary shifts still work, promoting to Bignum as needed.
		{`p 5 << 2`, `20`},
		{`p 20 >> 2`, `5`},
		{`p (2**64) << 4`, `295147905179352825856`},
		{`p (2**70) >> 1`, `590295810358705651712`},
		// A huge negative shift amount (right shift by a Bignum): 0, or -1 for a
		// negative receiver.
		{`p 5 << -(2**64)`, `0`},
		{`p (-5) << -(2**64)`, `-1`},
		{`p 5 >> (2**64)`, `0`},
		{`p (-5) >> (2**64)`, `-1`},
		{`p (2**100) >> (2**64)`, `0`},
		// A huge positive left shift of zero stays zero.
		{`p 0 << (2**64)`, `0`},
		{`p 0 >> -(2**64)`, `0`},
		// Any other huge positive left shift raises RangeError (would exceed
		// addressable memory).
		{`begin; 1 << (2**67); rescue RangeError => e; p e.class; end`, `RangeError`},
		{`begin; (-1) << (2**67); rescue RangeError => e; p e.class; end`, `RangeError`},
		{`begin; 3 >> -(2**67); rescue RangeError => e; p e.class; end`, `RangeError`},
		// A non-Integer argument is coerced with #to_int.
		{`class I; def to_int; 3; end; end; p 5 << I.new`, `40`},
		{`class I; def to_int; 3; end; end; p 160 >> I.new`, `20`},
		// A missing #to_int, or one returning a non-Integer, raises TypeError.
		{`begin; 5 << Object.new; rescue TypeError => e; p e.message; end`, `"no implicit conversion of Object into Integer"`},
		{`class B; def to_int; "x"; end; end; begin; 5 << B.new; rescue TypeError => e; p e.message; end`, `"can't convert B to Integer (B#to_int gives String)"`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}

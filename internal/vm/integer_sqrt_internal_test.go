// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestIntegerSqrt covers Integer.sqrt: the floored integer square root, exact
// squares, a Bignum argument and result, #to_int coercion of a Float and of a
// user object, the Math::DomainError for a negative argument, and the TypeError
// for a non-coercible argument (and a #to_int returning a non-Integer). Verified
// against ruby 4.0.6.
func TestIntegerSqrt(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p Integer.sqrt(0)`, `0`},
		{`p Integer.sqrt(1)`, `1`},
		{`p Integer.sqrt(24)`, `4`},
		{`p Integer.sqrt(25)`, `5`},
		{`p Integer.sqrt(10)`, `3`},
		{`p Integer.sqrt(10).is_a?(Integer)`, `true`},
		// Bignum argument and result.
		{`p Integer.sqrt(10**400) == 10**200`, `true`},
		{`p Integer.sqrt(2**128).class`, `Integer`},
		// #to_int coercion: a Float (truncated) and a user object.
		{`p Integer.sqrt(10.0)`, `3`},
		{`p Integer.sqrt(10.9)`, `3`},
		{`class K; def to_int; 25; end; end; p Integer.sqrt(K.new)`, `5`},
		// A negative argument raises Math::DomainError.
		{`begin; Integer.sqrt(-4); rescue Math::DomainError => e; p e.message; end`,
			`"Numerical argument is out of domain - \"isqrt\""`},
		// A non-coercible argument, and a #to_int returning a non-Integer, raise TypeError.
		{`begin; Integer.sqrt("test"); rescue TypeError => e; p e.class; end`, `TypeError`},
		{`class Bad; def to_int; "x"; end; end
begin; Integer.sqrt(Bad.new); rescue TypeError => e; p e.class; end`, `TypeError`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}

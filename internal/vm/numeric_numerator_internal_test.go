// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestNumericNumeratorDenominator covers #numerator and #denominator on Integer,
// Float, and the Numeric base (via Rational): an Integer is its own numerator
// over denominator 1 (Bignum included), a finite Float uses its exact rational
// value, and a non-finite Float (Infinity/NaN) returns itself as the numerator
// and 1 as the denominator. Verified against ruby 4.0.6.
func TestNumericNumeratorDenominator(t *testing.T) {
	cases := []struct{ src, want string }{
		// Integer.
		{`p 3.numerator`, `3`},
		{`p 3.denominator`, `1`},
		{`p(-5.numerator)`, `-5`},
		{`p(-5.denominator)`, `1`},
		{`p (2**70).numerator`, `1180591620717411303424`},
		{`p (2**70).denominator`, `1`},
		// Float, finite.
		{`p 1.5.numerator`, `3`},
		{`p 1.5.denominator`, `2`},
		{`p 0.75.numerator`, `3`},
		{`p 0.75.denominator`, `4`},
		{`p 2.0.numerator`, `2`},
		{`p 2.0.denominator`, `1`},
		// Float, non-finite: no rational form.
		{`p (1.0/0).numerator`, `Infinity`},
		{`p (1.0/0).denominator`, `1`},
		{`p (-1.0/0).numerator`, `-Infinity`},
		{`p (0.0/0).numerator.nan?`, `true`},
		{`p (0.0/0).denominator`, `1`},
		// Numeric base delegates to #to_r (Rational has its own, so this exercises
		// the inherited path through a Numeric subclass that defines only #to_r).
		{`p (3r/4).numerator`, `3`},
		{`p (3r/4).denominator`, `4`},
		{`class N < Numeric; def to_r; Rational(5, 6); end; end; p N.new.numerator`, `5`},
		{`class N < Numeric; def to_r; Rational(5, 6); end; end; p N.new.denominator`, `6`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}

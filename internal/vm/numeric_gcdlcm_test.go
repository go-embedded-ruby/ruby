package vm_test

import (
	"strings"
	"testing"
)

// TestNumericGcdlcmDivmod covers Integer#gcdlcm / #remainder and Float#divmod /
// #to_r, asserted against MRI Ruby 4.0.5.
func TestNumericGcdlcmDivmod(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p [17.gcdlcm(5), 0.gcdlcm(5), 12.gcdlcm(18)]`, "[[1, 85], [5, 0], [6, 36]]\n"},
		// remainder truncates toward zero (sign of the dividend), unlike %.
		{`p [(-7).remainder(3), 7.remainder(-3), 7.remainder(3), (-7).remainder(-3)]`, "[-1, 1, 1, -1]\n"},
		// Float#divmod: floored quotient (Integer) + Float modulo.
		{`p [7.0.divmod(2), (-7.0).divmod(2), 7.5.divmod(2)]`, "[[3, 1.0], [-4, 1.0], [3, 1.5]]\n"},
		{`p 7.0.divmod(3)`, "[2, 1.0]\n"},
		// Float#to_r: exact rational (floats are exact binary rationals).
		{`p [10.0.to_r, 0.5.to_r, 2.5.to_r]`, "[(10/1), (1/2), (5/2)]\n"},
		{`p 3.14.to_r.class`, "Rational\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	errs := []struct{ src, want string }{
		{`5.remainder(0)`, "divided by 0"},
		{`7.0.divmod(0)`, "divided by 0"},
		{`7.0.divmod("x")`, "String can't be coerced into Float"},
		{`(1.0 / 0).to_r`, "Infinity"},   // +Inf
		{`(-1.0 / 0).to_r`, "-Infinity"}, // -Inf
		{`(0.0 / 0).to_r`, "NaN"},        // NaN
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q err=%v, want substring %q", c.src, err, c.want)
		}
	}
}

// TestIntegerBignumDivision covers the Integer division family — divmod,
// remainder, gcd, lcm, gcdlcm — with Bignum operands (receiver and argument),
// Float arguments, the coerce protocol and the error paths, asserted against MRI
// Ruby 4.0.6.
func TestIntegerBignumDivision(t *testing.T) {
	cases := []struct{ src, want string }{
		// Bignum argument and Bignum receiver, floored like MRI.
		{`p 10.divmod(2**70)`, "[0, 10]\n"},
		{`p (-10).divmod(2**70)`, "[-1, 1180591620717411303414]\n"},
		{`p (2**70).divmod(7)`, "[168655945816773043346, 2]\n"},
		{`p [10.remainder(2**70), (2**70).remainder(7)]`, "[10, 2]\n"},
		{`p [10.gcd(2**70), (2**70).gcd(6), 0.gcd(0)]`, "[2, 2, 0]\n"},
		{`p [(2**70).lcm(6), 12.gcdlcm(2**70)]`, "[3541774862152233910272, [4, 3541774862152233910272]]\n"},
		// A Float argument gives an Integer quotient and a Float modulo/remainder.
		{`p [10.divmod(3.0), 10.divmod(3.5), (-7).divmod(3.0)]`, "[[3, 1.0], [2, 3.0], [-3, 2.0]]\n"},
		{`p [10.remainder(3.0), (-10).remainder(3.0)]`, "[1.0, -1.0]\n"},
		// The coerce protocol handles a non-numeric argument that offers #coerce.
		{`class Cx; def coerce(o); [o, 3]; end; end; p 10.divmod(Cx.new)`, "[3, 1]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	errs := []struct{ src, want string }{
		{`10.divmod(0)`, "divided by 0"},
		{`(2**70).divmod(0)`, "divided by 0"},
		{`10.divmod(0.0)`, "divided by 0"},
		{`(2**70).remainder(0)`, "divided by 0"},
		{`10.remainder(0.0)`, "divided by 0"},
		{`10.gcd(3.5)`, "not an integer"},
		{`10.lcm("x")`, "not an integer"},
		{`10.gcdlcm(nil)`, "not an integer"},
		{`10.divmod(nil)`, "nil can't be coerced into Integer"},
		{`10.divmod("x")`, "String can't be coerced into Integer"},
		{`10.remainder(true)`, "true can't be coerced into Integer"},
		{`class Bx; def coerce(o); 5; end; end; 10.divmod(Bx.new)`, "coerce must return [x, y]"},
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q err=%v, want substring %q", c.src, err, c.want)
		}
	}
}

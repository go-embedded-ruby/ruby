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

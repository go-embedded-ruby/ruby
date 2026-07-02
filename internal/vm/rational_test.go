package vm_test

import (
	"strings"
	"testing"
)

// TestRational covers Ruby Rational: construction/inspection, exact arithmetic,
// Float contamination, coercion both ways, equality and ordering (via
// Comparable), the conversions and helpers, exponentiation, and modulo — every
// value asserted against MRI 4.0.5.
func TestRational(t *testing.T) {
	cases := []struct{ src, want string }{
		// Construction + inspect (reduced, positive denominator, default den 1).
		{`p Rational(1, 2)`, "(1/2)\n"},
		{`p Rational(4, 2)`, "(2/1)\n"},
		{`p Rational(1, -2)`, "(-1/2)\n"},
		{`p Rational(3)`, "(3/1)\n"},
		{`puts Rational(1, 2)`, "1/2\n"}, // to_s drops the parentheses
		// Exact arithmetic.
		{`p Rational(1, 2) + Rational(1, 3)`, "(5/6)\n"},
		{`p Rational(3, 4) - Rational(1, 4)`, "(1/2)\n"},
		{`p Rational(1, 2) * Rational(2, 3)`, "(1/3)\n"},
		{`p Rational(1, 2) / Rational(3, 4)`, "(2/3)\n"},
		{`p Rational(7, 2) % Rational(2, 1)`, "(3/2)\n"},
		// Coercion: an Integer stays exact (either order).
		{`p Rational(1, 2) + 1`, "(3/2)\n"},
		{`p 1 + Rational(1, 2)`, "(3/2)\n"},
		{`p Rational(1, 2) + (10 ** 30)`, "(2000000000000000000000000000001/2)\n"}, // Bignum operand
		// A Float promotes the result to Float (either order).
		{`p Rational(1, 2) + 0.5`, "1.0\n"},
		{`p 0.5 + Rational(1, 2)`, "1.0\n"},
		// Equality and ordering (Comparable from <=>).
		{`p Rational(2, 1) == 2`, "true\n"},
		{`p 2 == Rational(2, 1)`, "true\n"}, // Rational on the right of ==
		{`p Rational(1, 2) == 0.5`, "true\n"},
		{`p Rational(1, 2) == "x"`, "false\n"},
		{`p(Rational(0, 1) ? "y" : "n")`, "\"y\"\n"}, // truthy
		{`p Rational(1, 2) < Rational(2, 3)`, "true\n"},
		{`p Rational(1, 2) > 0.4`, "true\n"},
		{`p [Rational(1, 3), Rational(1, 2), Rational(1, 4)].sort`, "[(1/4), (1/3), (1/2)]\n"},
		{`p(Rational(1, 2) <=> "x")`, "nil\n"}, // non-numeric → nil
		// Conversions and helpers.
		{`p Rational(7, 2).to_i`, "3\n"},
		{`p Rational(1, 2).to_f`, "0.5\n"},
		{`p Rational(2, 3).numerator`, "2\n"},
		{`p Rational(2, 3).denominator`, "3\n"},
		{`p Rational(1, 2).to_r`, "(1/2)\n"},
		{`p Rational(1, 2).to_s`, "\"1/2\"\n"},
		{`p Rational(1, 2).inspect`, "\"(1/2)\"\n"},
		{`p Rational(-3, 4).abs`, "(3/4)\n"},
		{`p(-Rational(1, 2))`, "(-1/2)\n"},
		// Exponentiation: integer exponent exact, negative inverts, fractional → Float.
		{`p Rational(2, 3) ** 2`, "(4/9)\n"},
		{`p Rational(2, 3) ** -1`, "(3/2)\n"},
		{`p Rational(1, 4) ** 0.5`, "0.5\n"},                           // Float exponent → Float
		{`p Rational(4, 9) ** Rational(1, 2)`, "0.6666666666666666\n"}, // fractional Rational exponent → Float
		{`p Rational(1, 2).class`, "Rational\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestRationalErrors covers the raising paths: a zero denominator (construction,
// division, modulo), non-integer construction args, and a non-coercible operand.
func TestRationalErrors(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`Rational(1, 0)`, "ZeroDivisionError"},
		{`Rational(1.5, 2)`, "TypeError"},
		{`Rational(1, 1.5)`, "TypeError"},
		{`Rational(1, 2) / Rational(0, 1)`, "ZeroDivisionError"},
		{`Rational(1, 2) % Rational(0, 1)`, "ZeroDivisionError"},
		{`Rational(1, 2) + "x"`, "TypeError"},
		{`true + Rational(1, 2)`, "TypeError"},
		{`Rational(1, 2) ** "x"`, "TypeError"},
		{`Rational(0, 1) ** -1`, "ZeroDivisionError"},
	} {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want %q", c.src, err, c.want)
		}
	}
}

// TestRationalCompareFloat exercises the three float-comparison branches of <=>.
func TestRationalCompareFloat(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`p(Rational(1, 2) <=> 0.6)`, "-1\n"},
		{`p(Rational(1, 2) <=> 0.4)`, "1\n"},
		{`p(Rational(1, 2) <=> 0.5)`, "0\n"},
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

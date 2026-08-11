// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestIntegerBitPredicates covers Integer#allbits?/#anybits?/#nobits?, asserted
// against MRI Ruby 4.0.5. The mask is coerced with to_int semantics (a Float
// truncates), and negative / Bignum receivers use two's-complement bits.
func TestIntegerBitPredicates(t *testing.T) {
	cases := []struct{ src, want string }{
		// allbits?: every mask bit set in the receiver.
		{`p 0b1010.allbits?(0b1000)`, "true\n"},
		{`p 0b1010.allbits?(0b1010)`, "true\n"},
		{`p 0b1010.allbits?(0b1100)`, "false\n"},
		{`p 0b1010.allbits?(0)`, "true\n"}, // empty mask is vacuously contained
		// Float mask truncates via to_int: 1.5 -> 1, and 5 & 1 == 1.
		{`p 5.allbits?(1.5)`, "true\n"},
		{`p 5.allbits?(2.9)`, "false\n"}, // 2.9 -> 2, 5 & 2 == 0 != 2
		// Bignum receiver and mask.
		{`p (2**70 | 1).allbits?(2**70)`, "true\n"},
		{`p (2**70).allbits?(2**70 | 1)`, "false\n"},
		// Two's-complement: -1 has every bit set.
		{`p (-1).allbits?(0b1111)`, "true\n"},

		// anybits?: at least one shared bit.
		{`p 0b1010.anybits?(0b0100)`, "false\n"},
		{`p 0b1010.anybits?(0b1100)`, "true\n"},
		{`p 0.anybits?(5)`, "false\n"},
		{`p (2**70 | 4).anybits?(4)`, "true\n"},

		// nobits?: no shared bit (the complement of anybits?).
		{`p 0b1010.nobits?(0b0101)`, "true\n"},
		{`p 0b1010.nobits?(0b1100)`, "false\n"},
		{`p 0.nobits?(5)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	// A non-integer mask that lacks #to_int raises, as does a non-finite Float.
	errs := []struct{ src, want string }{
		{`5.allbits?("x")`, "no implicit conversion of String into Integer"},
		{`5.anybits?("x")`, "no implicit conversion of String into Integer"},
		{`5.nobits?("x")`, "no implicit conversion of String into Integer"},
		{`5.allbits?(1.0 / 0)`, "Infinity"},
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q err=%v, want substring %q", c.src, err, c.want)
		}
	}
}

// TestIntegerCeildiv covers Integer#ceildiv (Ruby 3.2+): the quotient rounded
// toward +Infinity, for positive/negative operands, Float/Rational divisors, and
// Bignum receivers — matching MRI Ruby 4.0.5.
func TestIntegerCeildiv(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p 10.ceildiv(3)`, "4\n"},
		{`p 9.ceildiv(3)`, "3\n"}, // exact multiple: no rounding up
		{`p (-10).ceildiv(3)`, "-3\n"},
		{`p 10.ceildiv(-3)`, "-3\n"},
		{`p (-10).ceildiv(-3)`, "4\n"},
		{`p 0.ceildiv(3)`, "0\n"},
		{`p 10.ceildiv(3.5)`, "3\n"}, // Float divisor, result still an Integer
		{`p 10.ceildiv(2r)`, "5\n"},  // Rational divisor
		{`p (10**30).ceildiv(7)`, "142857142857142857142857142858\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	// Zero divisor raises ZeroDivisionError; a non-numeric divisor raises TypeError.
	if err := runErr(t, `10.ceildiv(0)`); err == nil || !strings.Contains(err.Error(), "divided by 0") {
		t.Errorf("ceildiv(0) err=%v, want divided by 0", err)
	}
	if err := runErr(t, `10.ceildiv("x")`); err == nil || !strings.Contains(err.Error(), "coerced") {
		t.Errorf("ceildiv(\"x\") err=%v, want a coercion TypeError", err)
	}
}

// TestNumericQuo covers Integer#quo / Float#quo, asserted against MRI Ruby 4.0.5.
// Integer#quo yields a Rational for Integer/Rational operands and a Float for a
// Float operand; Float#quo (an alias of #fdiv) always yields a Float.
func TestNumericQuo(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p 3.quo(2)`, "(3/2)\n"},
		{`p 6.quo(3)`, "(2/1)\n"}, // reduced, but the denominator is kept
		{`p (-3).quo(2)`, "(-3/2)\n"},
		{`p 3.quo(2.0)`, "1.5\n"}, // Float operand -> Float
		{`p 3.quo(2r)`, "(3/2)\n"},
		{`p 3.quo(10**40)`, "(3/10000000000000000000000000000000000000000)\n"}, // Bignum operand
		{`p 3.quo(2).class`, "Rational\n"},
		// Float#quo is fdiv: always a Float, and a zero divisor gives Infinity.
		{`p 3.0.quo(2)`, "1.5\n"},
		{`p 3.0.quo(2r)`, "1.5\n"},
		{`p 3.0.quo(0)`, "Infinity\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	errs := []struct{ src, want string }{
		{`3.quo(0)`, "divided by 0"},
		{`3.quo(0r)`, "divided by 0"},
		{`3.quo("x")`, "String can't be coerced into Rational"},
		{`3.quo(nil)`, "nil can't be coerced into Rational"},
		{`3.quo(true)`, "true can't be coerced into Rational"},
		{`3.0.quo("x")`, "String can't be coerced into Float"},
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q err=%v, want substring %q", c.src, err, c.want)
		}
	}
}

// TestFloatRemainder covers Float#remainder: self - other*(self/other).truncate,
// keeping the dividend's sign, with NaN/Infinity edges — vs MRI Ruby 4.0.5.
func TestFloatRemainder(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p 7.0.remainder(3)`, "1.0\n"},
		{`p (-7.0).remainder(3)`, "-1.0\n"},
		{`p 7.0.remainder(-3)`, "1.0\n"},
		{`p 7.0.remainder(3.5)`, "0.0\n"},
		{`p (0.0 / 0).remainder(3)`, "NaN\n"}, // NaN dividend
		{`p (1.0 / 0).remainder(3)`, "NaN\n"}, // Infinity dividend
		{`p (-7.5).remainder(3)`, "-1.5\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	if err := runErr(t, `5.0.remainder(0)`); err == nil || !strings.Contains(err.Error(), "divided by 0") {
		t.Errorf("remainder(0) err=%v, want divided by 0", err)
	}
	if err := runErr(t, `5.0.remainder("x")`); err == nil || !strings.Contains(err.Error(), "String can't be coerced into Float") {
		t.Errorf("remainder(\"x\") err=%v, want coercion TypeError", err)
	}
}

// TestFloatStep covers Float#step: the enumerator form (no block) and the
// block form walking [self, limit] by step (default 1), incl. a negative step —
// asserted against MRI Ruby 4.0.5.
func TestFloatStep(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p 1.0.step(3.0).to_a`, "[1.0, 2.0, 3.0]\n"}, // default step, enumerator
		{`p 1.0.step(3.0, 0.5).to_a`, "[1.0, 1.5, 2.0, 2.5, 3.0]\n"},
		{`p 5.0.step(1.0, -2.0).to_a`, "[5.0, 3.0, 1.0]\n"}, // negative step
		{`p 1.0.step(3.0) { |x| }`, "1.0\n"},                // block form returns self
		{`a = []; 1.0.step(2.0, 0.25) { |x| a << x }; p a`, "[1.0, 1.25, 1.5, 1.75, 2.0]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	// No limit argument raises ArgumentError; a zero step raises ArgumentError.
	if err := runErr(t, `1.0.step { |x| }`); err == nil || !strings.Contains(err.Error(), "given 0") {
		t.Errorf("step (no args) err=%v, want ArgumentError", err)
	}
	if err := runErr(t, `1.0.step(3.0, 0) { |x| }`); err == nil || !strings.Contains(err.Error(), "step can't be 0") {
		t.Errorf("step(3.0,0) err=%v, want step can't be 0", err)
	}
}

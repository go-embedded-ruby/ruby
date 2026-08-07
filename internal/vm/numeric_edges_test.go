// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestIntegerBitReference covers Integer#[] in its single-bit, (start, len) and
// Range forms, including negative indices, negative lengths, two's-complement
// negatives, Bignum receivers and beginless/endless ranges. Values match MRI 4.0.5.
func TestIntegerBitReference(t *testing.T) {
	cases := []struct{ src, want string }{
		// single-bit
		{`p 0b1011[0]`, "1\n"},
		{`p 0b1011[2]`, "0\n"},
		{`p 0b1011[-1]`, "0\n"},  // negative index -> 0
		{`p 0b1011[100]`, "0\n"}, // past MSB of a positive -> 0
		{`p (-2)[0]`, "0\n"},     // two's complement
		{`p (-2)[100]`, "1\n"},   // sign extension of a negative
		{`p (-1)[3]`, "1\n"},     // negative, bit set in two's complement
		{`p 13[1.3]`, "0\n"},     // Float index truncates to 1
		{`p 13[2.1]`, "1\n"},     // Float index truncates to 2
		{`p (2**70)[70]`, "1\n"}, // Bignum receiver
		{`p (2**70)[69]`, "0\n"},
		{`p 2[2**70]`, "0\n"}, // Bignum index
		// (start, len)
		{`p 255[0,3]`, "7\n"},
		{`p 0b101001101[2,4]`, "3\n"},
		{`p 0b000001[-1,4]`, "2\n"},      // negative start shifts to MSB
		{`p 0b101001101[3,-15]`, "41\n"}, // negative length ignored -> full shift
		// Range
		{`p 255[1..4]`, "15\n"},
		{`p 255[1...4]`, "7\n"},             // exclusive
		{`p 255[2..]`, "63\n"},              // endless
		{`p 0b1011[-2..3]`, "44\n"},         // negative begin
		{`p 0b101001101[4..1]`, "20\n"},     // upper < lower -> endless from lower
		{`p 0b101001101[-4..-5]`, "5328\n"}, // upper < lower, negatives
		{`p eval("0b10000[..3]")`, "0\n"},   // beginless, all covered bits zero
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestIntegerBitReferenceErrors covers Integer#[]'s error branches.
func TestIntegerBitReferenceErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		{`3["3"]`, "TypeError"}, // String -> no implicit conversion
		{`o = Object.new; def o.to_int; "x"; end; 3[o]`, "TypeError"}, // to_int gives non-Integer
		{`3[Float::NAN]`, "FloatDomainError"},                         // NaN index
		{`p 0x0001[3..Float::INFINITY]`, "FloatDomainError"},          // +Infinity boundary
		{`p 0x0001[-Float::INFINITY..3]`, "FloatDomainError"},         // -Infinity boundary
		{`eval("0b111110[..3]")`, "ArgumentError"},                    // beginless, covered bit set
		{`0b1011[nil..nil]`, "ArgumentError"},                         // beginless + endless
	}
	for _, c := range cases {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q err=%v want substring %q", c.src, err, c.want)
		}
	}
}

// TestIntegerBitReferenceToInt confirms a custom #to_int index is honoured.
func TestIntegerBitReferenceToInt(t *testing.T) {
	src := `o = Object.new
def o.to_int; 1; end
p 2[o]`
	if got := eval(t, src); got != "1\n" {
		t.Errorf("to_int index got=%q", got)
	}
	// a #to_int returning a Bignum is honoured too.
	bigSrc := `o = Object.new
def o.to_int; 2**70; end
p 2[o]`
	if got := eval(t, bigSrc); got != "0\n" {
		t.Errorf("to_int Bignum index got=%q", got)
	}
}

// TestIntegerSizeOrdInteger covers Integer#size / #ord / #integer?.
func TestIntegerSizeOrdInteger(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p 42.size`, "8\n"},
		{`p 0.size`, "8\n"},
		{`p (-1).size`, "8\n"},
		{`p (2**63).size`, "8\n"},
		{`p (2**64).size`, "9\n"},
		{`p (2**64 - 1).size`, "8\n"},
		{`p (2**128).size`, "17\n"},
		{`p 65.ord`, "65\n"},
		{`p (-5).ord`, "-5\n"},
		{`p 5.integer?`, "true\n"},
		{`p (2**70).integer?`, "true\n"},
		{`p 5.5.integer?`, "false\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestFloatNextPrevFloat covers Float#next_float / #prev_float, including the
// MAX/INFINITY transitions and non-finite pass-through.
func TestFloatNextPrevFloat(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p 1.0.next_float == 1.0 + Float::EPSILON`, "true\n"},
		{`p 3.0.prev_float < 3.0`, "true\n"},
		{`p Float::MAX.next_float == Float::INFINITY`, "true\n"},
		{`p (-Float::INFINITY).next_float == -Float::MAX`, "true\n"},
		{`p Float::INFINITY.next_float == Float::INFINITY`, "true\n"},
		{`p (-Float::INFINITY).prev_float == -Float::INFINITY`, "true\n"},
		{`p 0.0.next_float > 0.0`, "true\n"},
		{`p Float::NAN.next_float.nan?`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestFloatRoundHalf covers Float#round with the `half:` keyword across ndigits.
func TestFloatRoundHalf(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p 2.5.round(half: :even)`, "2\n"},
		{`p 3.5.round(half: :even)`, "4\n"},
		{`p 2.5.round(half: :up)`, "3\n"},
		{`p 2.5.round(half: :down)`, "2\n"},
		{`p 2.5.round(half: nil)`, "3\n"},
		{`p (-2.5).round(half: :even)`, "-2\n"},
		{`p 5.55.round(1, half: :up)`, "5.6\n"},
		{`p 5.55.round(1, half: :down)`, "5.5\n"},
		{`p 5.55.round(1, half: :even)`, "5.6\n"},
		{`p (-5.55).round(1, half: :even)`, "-5.6\n"},
		{`p 4.8100000000000005.round(5, half: :even)`, "4.81\n"},
		{`p 2.675.round(2)`, "2.68\n"}, // default (up) with representation correction
		{`p 0.0.round(2)`, "0.0\n"},
		{`p 0.0.round`, "0\n"},
		{`p 25.0.round(-1, half: :even)`, "20\n"}, // ndigits<0 with half:
		{`p 15.0.round(-1, half: :even)`, "20\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	// invalid rounding mode raises ArgumentError (even for ndigits >= 0).
	for _, src := range []string{`2.5.round(half: :foo)`, `2.5.round(2, half: :foo)`} {
		if err := runErr(t, src); err == nil || !strings.Contains(err.Error(), "invalid rounding mode: foo") {
			t.Errorf("src=%q err=%v", src, err)
		}
	}
}

// TestIntegerRoundHalf covers Integer#round with negative ndigits and `half:`,
// including Bignum receivers.
func TestIntegerRoundHalf(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p 35.round`, "35\n"},                 // ndigits>=0 unchanged
		{`p 35.round(1, half: :even)`, "35\n"}, // ndigits>=0 keyword still validated
		{`p 12345.round(-2)`, "12300\n"},
		{`p 250.round(-2)`, "300\n"}, // default half up
		{`p 25.round(-1, half: :up)`, "30\n"},
		{`p 25.round(-1, half: :down)`, "20\n"},
		{`p 25.round(-1, half: :even)`, "20\n"},
		{`p 35.round(-1, half: :even)`, "40\n"},
		{`p (-25).round(-1, half: :down)`, "-20\n"},
		{`p (-25).round(-1, half: :up)`, "-30\n"},
		{`p 5.round(-100)`, "0\n"}, // int_round_zero_p
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	// Bignum round: (25*10**70).round(-71) == 30*10**70 (default half up);
	// with half: :even it is 20*10**70.
	if got := eval(t, `p (25 * 10**70).round(-71) == 30 * 10**70`); got != "true\n" {
		t.Errorf("bignum round default got=%q", got)
	}
	if got := eval(t, `p (25 * 10**70).round(-71, half: :even) == 20 * 10**70`); got != "true\n" {
		t.Errorf("bignum round even got=%q", got)
	}
}

// TestNumericNonzeroInteger covers Numeric#nonzero? and #integer? (dispatching
// through #zero? and answered false at the abstract Numeric level).
func TestNumericNonzeroInteger(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p 5.nonzero?`, "5\n"},
		{`p 0.nonzero?`, "nil\n"},
		{`p 0.0.nonzero?`, "nil\n"},
		{`p (-3).nonzero?`, "-3\n"},
		{`p 5.5.nonzero?`, "5.5\n"},
		{`p 1r.integer?`, "false\n"},     // Rational inherits Numeric#integer?
		{`p (1/2r).nonzero?`, "(1/2)\n"}, // Rational nonzero? via zero?
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

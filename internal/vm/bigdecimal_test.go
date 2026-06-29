// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestBigDecimal covers Ruby BigDecimal, now backed by
// github.com/go-ruby-bigdecimal/bigdecimal (an MRI-4.0.5-byte-exact
// arbitrary-precision decimal, a sibling of go-ruby-regexp / go-ruby-erb /
// go-ruby-yaml / go-ruby-json). The expectations are the REAL MRI ones: to_s is
// the scientific "0.15e1" form (the former go-composites/bigfloat shim rendered
// the plain "1.5"), and floor/ceil/round/truncate with no argument return an
// Integer. It exercises the Kernel#BigDecimal constructor over each operand type,
// the arbitrary-precision arithmetic (+/-/*/), the precision-taking div / % /
// divmod / modulo / remainder, the round family, the conversions / predicates,
// and the ordering / equality operators — including the exact-decimal property
// (0.1 + 0.2 == 0.3) a binary Float cannot represent.
func TestBigDecimal(t *testing.T) {
	cases := []struct{ src, want string }{
		// Construction from a String, parsed at full precision; to_s/inspect are
		// MRI's scientific 0.<digits>e<exp>.
		{`p BigDecimal("1.5").class`, "BigDecimal\n"},
		{`puts BigDecimal("1.5")`, "0.15e1\n"},
		{`p BigDecimal("1.5").to_s`, "\"0.15e1\"\n"},
		{`p BigDecimal("1.5").inspect`, "\"0.15e1\"\n"},
		{`p [BigDecimal("1.5")]`, "[0.15e1]\n"},           // Go-level Inspect, via Array#inspect
		{`puts BigDecimal("1.5").to_s("F")`, "1.5\n"},     // plain floating form
		{`puts BigDecimal("1.5").to_s("+")`, "+0.15e1\n"}, // sign prefix
		// Construction from an Integer / Bignum / Float(+ndigits) / BigDecimal.
		{`puts BigDecimal(5)`, "0.5e1\n"},
		{`puts BigDecimal(10 ** 30)`, "0.1e31\n"},          // Bignum, exact through its big.Int
		{`puts BigDecimal(2.5, 5)`, "0.25e1\n"},            // Float needs the ndigits argument
		{`puts BigDecimal(BigDecimal("3.5"))`, "0.35e1\n"}, // pass-through
		// The exact-decimal headline: 0.1 + 0.2 is exactly 0.3 (Float is not).
		{`puts (BigDecimal("0.1") + BigDecimal("0.2")).to_s`, "0.3e0\n"},
		{`p(BigDecimal("0.1") + BigDecimal("0.2") == BigDecimal("0.3"))`, "true\n"},
		{`p(0.1 + 0.2 == 0.3)`, "false\n"}, // contrast: binary Float
		// Arithmetic.
		{`puts (BigDecimal("1.5") - BigDecimal("0.25")).to_s`, "0.125e1\n"},
		{`puts (BigDecimal("2") * BigDecimal("3.5")).to_s`, "0.7e1\n"},
		{`puts (BigDecimal("10") / BigDecimal("4")).to_s`, "0.25e1\n"},
		// Mixed operands: an Integer / Float / Rational coerces to BigDecimal.
		{`puts (BigDecimal("1.5") + 2).to_s`, "0.35e1\n"},
		{`puts (BigDecimal("1.5") + 0.5).to_s`, "0.2e1\n"},
		{`puts (BigDecimal("1.5") + Rational(1, 2)).to_s`, "0.2e1\n"},
		{`puts (BigDecimal("1.5") + BigDecimal("1000000000000000000000000")).to_s`, "0.10000000000000000000000015e25\n"},
		// abs / unary -@.
		{`puts BigDecimal("-3.5").abs.to_s`, "0.35e1\n"},
		{`puts BigDecimal("3.5").abs.to_s`, "0.35e1\n"},
		{`puts (-BigDecimal("2.5")).to_s`, "-0.25e1\n"},
		{`puts BigDecimal("2.5").send(:-@).to_s`, "-0.25e1\n"}, // explicit -@ method
		// to_f / to_i / to_int / to_r.
		{`p BigDecimal("2.5").to_f`, "2.5\n"},
		{`p BigDecimal("1.5").to_i`, "1\n"},
		{`p BigDecimal("-1.9").to_i`, "-1\n"}, // truncates toward zero, not floor
		{`p BigDecimal("42").to_int`, "42\n"},
		{`p BigDecimal("2.5").to_i.class`, "Integer\n"},
		{`p BigDecimal("10000000000000000000").to_i.class`, "Integer\n"}, // Bignum-range stays Integer
		{`p BigDecimal("2.5").to_r`, "(5/2)\n"},
		// zero? / nan? / infinite? / finite?.
		{`p BigDecimal("0").zero?`, "true\n"},
		{`p BigDecimal("-0.1").zero?`, "false\n"},
		{`p BigDecimal("NaN").nan?`, "true\n"},
		{`p BigDecimal("1").nan?`, "false\n"},
		{`p BigDecimal("Infinity").infinite?`, "1\n"},
		{`p BigDecimal("-Infinity").infinite?`, "-1\n"},
		{`p BigDecimal("1").infinite?`, "nil\n"}, // a finite value: nil, not false
		{`p BigDecimal("1").finite?`, "true\n"},
		{`p BigDecimal("Infinity").finite?`, "false\n"},
		// sign / exponent / precision / frac / fix / split.
		{`p BigDecimal("2.5").sign`, "2\n"},   // positive finite
		{`p BigDecimal("-2.5").sign`, "-2\n"}, // negative finite
		{`p BigDecimal("0").sign`, "1\n"},     // +0
		{`p BigDecimal("1.23").exponent`, "1\n"},
		{`p BigDecimal("1.23").precision`, "3\n"},
		{`puts BigDecimal("1.5").frac`, "0.5e0\n"},
		{`puts BigDecimal("1.5").fix`, "0.1e1\n"},
		{`p BigDecimal("1.5").split`, "[1, \"15\", 10, 1]\n"},
		{`p BigDecimal("-1.5").split`, "[-1, \"15\", 10, 1]\n"},
		// div: with precision -> BigDecimal; with none -> the integer quotient.
		{`puts BigDecimal("1").div(3, 10)`, "0.3333333333e0\n"},
		{`p BigDecimal("10").div(BigDecimal("4")).class`, "BigDecimal\n"},
		{`puts BigDecimal("10").div(BigDecimal("4"))`, "0.2e1\n"}, // floor(10/4)=2
		{`p BigDecimal("10").div(BigDecimal("4"), 5).class`, "BigDecimal\n"},
		// % / modulo / remainder / divmod.
		{`puts BigDecimal("7") % BigDecimal("3")`, "0.1e1\n"},
		{`puts BigDecimal("-7").modulo(BigDecimal("3"))`, "0.2e1\n"},     // sign of divisor
		{`puts BigDecimal("-7").remainder(BigDecimal("3"))`, "-0.1e1\n"}, // sign of receiver
		{`p BigDecimal("7").divmod(BigDecimal("3"))`, "[0.2e1, 0.1e1]\n"},
		{`p BigDecimal("7").divmod(BigDecimal("3")).map(&:class)`, "[BigDecimal, BigDecimal]\n"},
		// floor / ceil / truncate: no-arg or n==0 -> Integer; other n -> BigDecimal.
		{`p BigDecimal("2.4").floor`, "2\n"},
		{`p BigDecimal("2.4").floor.class`, "Integer\n"},
		{`p BigDecimal("-2.4").floor`, "-3\n"},
		{`p BigDecimal("2.4").ceil`, "3\n"},
		{`p BigDecimal("-2.4").ceil`, "-2\n"},
		{`puts BigDecimal("2.78").truncate(1)`, "0.27e1\n"},
		{`p BigDecimal("2.78").truncate(1).class`, "BigDecimal\n"},
		{`puts BigDecimal("25").floor(-1)`, "0.2e2\n"}, // negative digit -> BigDecimal
		{`p BigDecimal("25").floor(-1).class`, "BigDecimal\n"},
		{`p BigDecimal("2.7").floor(0).class`, "BigDecimal\n"}, // floor: any explicit arg -> BigDecimal
		{`p BigDecimal("2.7").ceil(0).class`, "BigDecimal\n"},
		{`p BigDecimal("2.7").truncate(0).class`, "BigDecimal\n"},
		// round: no-arg / 0 / negative-without-mode -> Integer; positive or
		// any-with-mode -> BigDecimal; halves away from zero by default.
		{`p BigDecimal("2.5").round`, "3\n"},
		{`p BigDecimal("2.4").round`, "2\n"},
		{`p BigDecimal("-2.5").round`, "-3\n"},
		{`p BigDecimal("2.5").round.class`, "Integer\n"},
		{`p BigDecimal("2.5").round(0).class`, "Integer\n"},
		{`puts BigDecimal("1.567").round(2)`, "0.157e1\n"},
		{`p BigDecimal("1.567").round(2).class`, "BigDecimal\n"},
		{`p BigDecimal("25.67").round(-1)`, "30\n"}, // negative digit, no mode -> Integer
		{`p BigDecimal("25.67").round(-1).class`, "Integer\n"},
		{`puts BigDecimal("9.9").round(-2, BigDecimal::ROUND_UP)`, "0.1e3\n"}, // mode -> BigDecimal
		{`p BigDecimal("9.9").round(-2, BigDecimal::ROUND_UP).class`, "BigDecimal\n"},
		{`puts BigDecimal("2.5").round(0, BigDecimal::ROUND_HALF_EVEN)`, "0.2e1\n"}, // banker's
		{`puts BigDecimal("2.5").round(0, BigDecimal::ROUND_DOWN)`, "0.2e1\n"},
		{`puts BigDecimal("2.5").round(0, BigDecimal::ROUND_CEILING)`, "0.3e1\n"},
		{`puts BigDecimal("-2.5").round(0, BigDecimal::ROUND_FLOOR)`, "-0.3e1\n"},
		{`puts BigDecimal("2.5").round(0, BigDecimal::ROUND_HALF_DOWN)`, "0.2e1\n"},
		// ** / power / pow (Integer exponent; negative -> reciprocal; 0**0 == 1).
		{`puts (BigDecimal("2") ** 10).to_s`, "0.1024e4\n"},
		{`puts BigDecimal("2").power(10).to_s`, "0.1024e4\n"},
		{`puts BigDecimal("2").pow(10).to_s`, "0.1024e4\n"},
		{`puts (BigDecimal("3") ** 0).to_s`, "0.1e1\n"},
		{`puts (BigDecimal("2") ** -1).to_s`, "0.5e0\n"}, // reciprocal
		{`puts BigDecimal("2").send(:**, 3).to_s`, "0.8e1\n"},
		// Comparison: <=> and the boolean operators (numeric operand coerces).
		{`p(BigDecimal("1") <=> BigDecimal("2"))`, "-1\n"},
		{`p(BigDecimal("2") <=> BigDecimal("1"))`, "1\n"},
		{`p(BigDecimal("1") <=> BigDecimal("1"))`, "0\n"},
		{`p(BigDecimal("1.5") <=> 2)`, "-1\n"},                // Integer operand coerces
		{`p(BigDecimal("1") <=> 10 ** 30)`, "-1\n"},           // Bignum operand coerces
		{`p(BigDecimal("0.5") <=> Rational(1, 2))`, "0\n"},    // Rational operand coerces
		{`p(BigDecimal("1.5") <=> 1.5)`, "0\n"},               // Float operand coerces
		{`p(BigDecimal("1") <=> "x")`, "nil\n"},               // non-numeric operand
		{`p(BigDecimal("NaN") <=> BigDecimal("1"))`, "nil\n"}, // NaN is incomparable
		{`p(BigDecimal("1") < BigDecimal("2"))`, "true\n"},
		{`p(BigDecimal("2") < BigDecimal("1"))`, "false\n"},
		{`p(BigDecimal("2") > BigDecimal("1"))`, "true\n"},
		{`p(BigDecimal("1") > BigDecimal("2"))`, "false\n"},
		{`p(BigDecimal("1") <= BigDecimal("1"))`, "true\n"},
		{`p(BigDecimal("2") <= BigDecimal("1"))`, "false\n"},
		{`p(BigDecimal("1") >= BigDecimal("1"))`, "true\n"},
		{`p(BigDecimal("1") >= BigDecimal("2"))`, "false\n"},
		{`p(BigDecimal("NaN") < BigDecimal("1"))`, "nil\n"}, // a NaN operand -> nil
		// Equality (operator routes through valueEqual; the explicit == method too).
		{`p(BigDecimal("1.5") == BigDecimal("1.5"))`, "true\n"},
		{`p(BigDecimal("1.5") == BigDecimal("2"))`, "false\n"},
		{`p(BigDecimal("2") == 2)`, "true\n"},                    // numeric operand coerces
		{`p(BigDecimal("2") == 10 ** 30)`, "false\n"},            // Bignum operand coerces (unequal)
		{`p(BigDecimal("1.5") == Rational(3, 2))`, "true\n"},     // Rational operand coerces
		{`p(BigDecimal("1.5") == "x")`, "false\n"},               // non-numeric operand
		{`p(BigDecimal("NaN") == BigDecimal("NaN"))`, "false\n"}, // NaN != NaN
		{`p(2 == BigDecimal("2"))`, "true\n"},                    // valueEqual, BigDecimal on the right
		{`p(Rational(3, 2) == BigDecimal("1.5"))`, "true\n"},     // valueEqual, Rational on the left
		{`p(BigDecimal("2") == BigDecimal("2"))`, "true\n"},      // valueEqual, BigDecimal both sides
		{`p BigDecimal("1.5").send(:==, BigDecimal("1.5"))`, "true\n"},
		{`p BigDecimal("1.5").send(:==, 42)`, "false\n"},
		// A BigDecimal on the RIGHT of an operator coerces the left operand and
		// keeps the BigDecimal result (the right-operand fast path).
		{`puts (2 + BigDecimal("1.5")).to_s`, "0.35e1\n"},
		{`puts (10 - BigDecimal("1.5")).to_s`, "0.85e1\n"},
		{`puts (3 * BigDecimal("2.5")).to_s`, "0.75e1\n"},
		{`puts (6 / BigDecimal("4")).to_s`, "0.15e1\n"},
		{`puts (7 % BigDecimal("3")).to_s`, "0.1e1\n"},
		{`puts (0.5 + BigDecimal("1.5")).to_s`, "0.2e1\n"},            // Float left operand
		{`puts (Rational(1, 2) + BigDecimal("1.5")).to_s`, "0.2e1\n"}, // Rational left operand
		{`p(((10 ** 30) + BigDecimal("0")).class)`, "BigDecimal\n"},   // Bignum left operand
		// send agrees with the operator syntax (operator fast path via dispatch).
		{`puts BigDecimal("1.5").send(:+, BigDecimal("0.5")).to_s`, "0.2e1\n"},
		{`puts BigDecimal("1.5").send(:-, BigDecimal("0.5")).to_s`, "0.1e1\n"},
		{`puts BigDecimal("1.5").send(:*, BigDecimal("2")).to_s`, "0.3e1\n"},
		{`puts BigDecimal("3").send(:/, BigDecimal("2")).to_s`, "0.15e1\n"},
		// Constants.
		{`p BigDecimal::ROUND_UP`, "1\n"},
		{`p BigDecimal::ROUND_HALF_EVEN`, "5\n"},
		{`p BigDecimal::ROUND_FLOOR`, "7\n"},
		{`p BigDecimal::SIGN_NEGATIVE_FINITE`, "-2\n"},
		{`p BigDecimal::SIGN_NaN`, "0\n"},
		{`p BigDecimal::INFINITY.infinite?`, "1\n"},
		{`p BigDecimal::NAN.nan?`, "true\n"},
		// truthiness + class.
		{`p(BigDecimal("0") ? "y" : "n")`, "\"y\"\n"}, // every BigDecimal is truthy
		{`p BigDecimal("1").class`, "BigDecimal\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestBigDecimalErrors covers the raising paths: a malformed string literal
// (ArgumentError), a Float constructor without the required ndigits
// (ArgumentError), a division / modulo by zero (ZeroDivisionError), a
// non-numeric operand to an arithmetic / ordering operator (TypeError via the
// coercion), the bare % operator (no opcode -> NoMethodError through the
// fast path), a non-convertible constructor argument (TypeError), a non-Integer
// exponent (TypeError), and an unknown rounding mode (ArgumentError).
func TestBigDecimalErrors(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`BigDecimal("xyz")`, "ArgumentError"},   // bad string literal
		{`BigDecimal(2.5)`, "ArgumentError"},     // Float without ndigits
		{`BigDecimal(2.5, "x")`, "TypeError"},    // non-Integer ndigits
		{`BigDecimal(2.5, -1)`, "ArgumentError"}, // negative ndigits (FromFloat fails)
		{`BigDecimal("1") / BigDecimal("0")`, "ZeroDivisionError"},
		{`BigDecimal("1") / 0`, "ZeroDivisionError"},                     // coerced zero divisor
		{`BigDecimal("1").div(BigDecimal("0"))`, "ZeroDivisionError"},    // integer div by zero
		{`BigDecimal("1").div(BigDecimal("0"), 5)`, "ZeroDivisionError"}, // precision div by zero
		{`BigDecimal("1") % BigDecimal("0")`, "ZeroDivisionError"},
		{`BigDecimal("1").modulo(BigDecimal("0"))`, "ZeroDivisionError"},
		{`BigDecimal("1").remainder(BigDecimal("0"))`, "ZeroDivisionError"},
		{`BigDecimal("1").divmod(BigDecimal("0"))`, "ZeroDivisionError"},
		{`BigDecimal("1") + "x"`, "TypeError"},               // + non-numeric
		{`BigDecimal("1") - "x"`, "TypeError"},               // - non-numeric
		{`BigDecimal("1") * "x"`, "TypeError"},               // * non-numeric
		{`BigDecimal("1") / "x"`, "TypeError"},               // / non-numeric
		{`BigDecimal("1") < "x"`, "TypeError"},               // < non-numeric
		{`BigDecimal("1") > "x"`, "TypeError"},               // > non-numeric
		{`BigDecimal("1") <= "x"`, "TypeError"},              // <= non-numeric
		{`BigDecimal("1") >= "x"`, "TypeError"},              // >= non-numeric
		{`BigDecimal([])`, "TypeError"},                      // non-convertible constructor arg
		{`BigDecimal("2") ** "x"`, "TypeError"},              // non-Integer exponent
		{`BigDecimal("9.9").round(-2, 99)`, "ArgumentError"}, // unknown rounding mode
		{`nil + BigDecimal("1")`, "TypeError"},               // non-numeric LEFT operand (right-op path)
	} {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want %q", c.src, err, c.want)
		}
	}
}

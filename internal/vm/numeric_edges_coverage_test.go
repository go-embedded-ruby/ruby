package vm_test

import (
	"strings"
	"testing"
)

// TestNumericEdgesCoverage exercises every method registered in
// registerNumericEdges across each of its branches — bit reference forms, the
// exact-quotient (#quo) Rational/Float/error paths, Float#remainder sign and
// error handling, the bit predicates, #ceildiv, #size, and Float#step — plus
// the numeric-operator branches (floored Bignum div/mod, Float % zero-divisor)
// that the shared arith dispatch grew. Each result is printed with `p` and
// checked against MRI Ruby 4.0.6's output.
func TestNumericEdgesCoverage(t *testing.T) {
	ok := []struct{ expr, want string }{
		// Integer#[] — single bit, two-arg (start,len) slice, and Range forms.
		{`5[0]`, "1"}, {`5[1]`, "0"}, {`5[2]`, "1"},
		{`0b1011001[2,3]`, "6"},
		{`0b1011001[2..4]`, "6"},
		{`0b1011001[2...5]`, "6"},
		{`255[4..]`, "15"},
		// Integer#size — fixnum is 8, a Bignum reports its byte length.
		{`1.size`, "8"},
		{`(256**20).size`, "21"},
		// Integer#ord / #integer?.
		{`65.ord`, "65"}, {`5.integer?`, "true"},
		// Integer#round with negative ndigits, half: modes, and ndigits>=0 (self).
		{`12345.round(-2)`, "12300"},
		{`15.round(-1)`, "20"},
		{`15.round(-1,half: :even)`, "20"},
		{`25.round(-1,half: :down)`, "20"},
		{`123.round(2)`, "123"},
		// Float#next_float / #prev_float bracket the value.
		{`1.0.next_float > 1.0`, "true"},
		{`1.0.prev_float < 1.0`, "true"},
		// Float#round honouring half: and ndigits.
		{`2.5.round`, "3"}, {`2.5.round(half: :even)`, "2"},
		{`3.14159.round(2)`, "3.14"},
		// Bit predicates + their #to_int mask coercion (Float mask truncates).
		{`0b1100.allbits?(0b0100)`, "true"},
		{`0b1100.allbits?(0b0110)`, "false"},
		{`0b1100.anybits?(0b0110)`, "true"},
		{`0b1100.anybits?(0b0011)`, "false"},
		{`0b1100.nobits?(0b0011)`, "true"},
		{`0b1100.nobits?(0b0100)`, "false"},
		{`0b1100.allbits?(4.9)`, "true"}, // Float mask truncates to 4
		// Integer#ceildiv rounds toward +Infinity, both signs.
		{`7.ceildiv(3)`, "3"}, {`-7.ceildiv(3)`, "-2"}, {`6.ceildiv(3)`, "2"},
		// Integer#quo: Float operand -> Float; Integer/Rational -> Rational.
		{`7.quo(2)`, "(7/2)"},
		{`7.quo(2.0)`, "3.5"},
		{`7.quo(Rational(1,2))`, "(14/1)"},
		{`(2**70).quo(2)`, "(590295810358705651712/1)"},
		{`7.quo(2**70)`, "(7/1180591620717411303424)"},
		// Float#quo is an exact fdiv; a zero divisor gives Infinity.
		{`10.0.quo(4)`, "2.5"},
		{`(1.0.quo(0)).infinite?`, "1"},
		// Float#remainder keeps the dividend's sign; the zero result is signed.
		{`7.0.remainder(3.5)`, "0.0"},
		{`7.5.remainder(2.0)`, "1.5"},
		{`-7.5.remainder(2.0)`, "-1.5"},
		{`(-7.0).remainder(3.5)`, "-0.0"},
		// Float#step without a block yields an Enumerator (materialised here).
		{`1.0.step(2.0,0.5).to_a`, "[1.0, 1.5, 2.0]"},
		// Shared numeric operators the arith dispatch grew: floored Bignum div/mod
		// with a negative divisor, and Float % with special divisors.
		{`(2**70) % -3`, "-2"},
		{`(-(2**70)) % 3`, "2"},
		{`(2**70) / -3`, "-393530540239137101142"},
		{`(7.0 % 3.0)`, "1.0"},
		{`(-7.0 % 3.0)`, "2.0"},
		// Numeric#<=> across every exact/float/Bignum path, NaN (nil), and a
		// non-numeric operand (nil) — dispatched through the method (via #send) so
		// it exercises spaceshipNumeric rather than the inlined comparison opcode.
		{`1.send(:"<=>", 2)`, "-1"},
		{`5.send(:"<=>", 5)`, "0"},
		{`(2**70).send(:"<=>", 2**71)`, "-1"},
		{`(2**70).send(:"<=>", 1.0e30)`, "-1"},
		{`(2**70).send(:"<=>", 0.0/0.0)`, "nil"},
		{`1.0.send(:"<=>", 2**70)`, "-1"},
		{`(0.0/0.0).send(:"<=>", 2**70)`, "nil"},
		{`1.5.send(:"<=>", 2)`, "-1"},
		{`1.send(:"<=>", "x")`, "nil"},
	}
	for _, c := range ok {
		if got := eval(t, "p ("+c.expr+")"); got != c.want+"\n" {
			t.Errorf("p (%s) = %q, want %q", c.expr, got, c.want+"\n")
		}
	}

	// Float#step with a block walks [self, limit] by step.
	if got := eval(t, `r = []; 1.0.step(2.0, 0.5) { |x| r << x }; p r`); got != "[1.0, 1.5, 2.0]\n" {
		t.Errorf("Float#step block form = %q", got)
	}

	// Numeric <=> with a non-numeric operand that defines #coerce: the value runs
	// the coerce protocol and re-dispatches <=> on the returned pair.
	if got := eval(t, `class Cz; def coerce(o); [o, 0]; end; end; p (5.send(:"<=>", Cz.new))`); got != "1\n" {
		t.Errorf("spaceshipNumeric coerce form = %q", got)
	}

	errs := []struct{ src, want string }{
		// Integer#quo error paths: non-numeric coercion and division by zero.
		{`7.quo("x")`, "can't be coerced into Rational"},
		{`7.quo(0)`, "divided by 0"},
		// Float#remainder error paths.
		{`7.0.remainder("x")`, "can't be coerced into Float"},
		{`7.0.remainder(0)`, "divided by 0"},
		// A String bit mask raises rather than coercing.
		{`12.allbits?("x")`, "no implicit conversion"},
		// Float % by zero raises (the grown floatOp branch).
		{`7.0 % 0`, "divided by 0"},
	}
	for _, c := range errs {
		err := runErr(t, c.src)
		if err == nil {
			t.Errorf("runErr(%q) = nil, want error containing %q", c.src, c.want)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("runErr(%q) error = %q, want containing %q", c.src, err.Error(), c.want)
		}
	}
}

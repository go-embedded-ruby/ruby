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

// TestRationalRounding covers floor/ceil/round/truncate at zero, positive and
// negative precision, every `half:` tie-breaking mode, the terminating-value
// short-circuit at large precision, and the TypeError on a non-integer precision
// — each value checked against MRI 4.0.5.
func TestRationalRounding(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`p Rational(5, 2).floor`, "2\n"},
		{`p Rational(-5, 2).floor`, "-3\n"},
		{`p Rational(5, 2).ceil`, "3\n"},
		{`p Rational(-5, 2).ceil`, "-2\n"},
		{`p Rational(5, 2).truncate`, "2\n"},
		{`p Rational(-5, 2).truncate`, "-2\n"},
		{`p Rational(5, 2).round`, "3\n"},
		{`p Rational(-5, 2).round`, "-3\n"},
		{`p Rational(4, 2).floor`, "2\n"}, // exact integer value: remainder 0
		{`p Rational(1, 3).round(2)`, "(33/100)\n"},
		{`p Rational(2, 3).ceil(2)`, "(67/100)\n"},
		{`p Rational(2, 3).floor(2)`, "(33/50)\n"},
		{`p Rational(5, 2).floor(2)`, "(5/2)\n"},  // terminates within precision
		{`p Rational(3, 2).round(20)`, "(3/2)\n"}, // large precision short-circuit
		{`p Rational(3, 2).round(2097171)`, "(3/2)\n"},
		{`p Rational(35, 1).round(-1)`, "40\n"},
		{`p Rational(7, 2).truncate(-1)`, "0\n"},
		{`p Rational(35, 1).floor(-1)`, "30\n"},
		{`p Rational(35, 1).ceil(-1)`, "40\n"},
		// half: modes on an exact tie (5/2 = 2.5).
		{`p Rational(5, 2).round(half: :up)`, "3\n"},
		{`p Rational(5, 2).round(half: :down)`, "2\n"},
		{`p Rational(5, 2).round(half: :even)`, "2\n"},
		{`p Rational(7, 2).round(half: :even)`, "4\n"}, // odd quotient rounds away
		{`p Rational(-5, 2).round(half: :down)`, "-2\n"},
		{`p Rational(5, 2).round(half: nil)`, "3\n"},
		{`p Rational(25, 100).round(1, half: :even)`, "(1/5)\n"},
		{`p Rational(1, 20).round(3)`, "(1/20)\n"},               // denominator 2^2*5: terminates
		{`p Rational(3, 40).floor(3)`, "(3/40)\n"},               // denominator 2^3*5
		{`p Rational(7, 2).send(:%, Rational(2, 1))`, "(3/2)\n"}, // the % method (not operator)
		{`p Rational(7, 2).send(:modulo, 1.5)`, "0.5\n"},
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	for _, c := range []struct{ src, want string }{
		{`Rational(1, 2).truncate(1.0)`, "TypeError"},
		{`Rational(1, 2).truncate(nil)`, "TypeError"},
		{`Rational(1, 2).round(half: :bogus)`, "ArgumentError"},
		{`Rational(1, 2).round(half: 5)`, "ArgumentError"},
	} {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want %q", c.src, err, c.want)
		}
	}
}

// TestRationalDivmod covers div / divmod / modulo / % against Integer, Rational
// and Float divisors (a Float promoting the quotient to a whole Float and the
// remainder to Float), the zero-divisor errors for each operand kind, and the
// div arity check.
func TestRationalDivmod(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`p Rational(7, 2).div(2)`, "1\n"},
		{`p Rational(7, 2).div(Rational(1, 3))`, "10\n"},
		{`p Rational(7, 2).div(1.5)`, "2\n"},
		{`p Rational(7, 2).divmod(2)`, "[1, (3/2)]\n"},
		{`p Rational(1, 2).divmod(1.5)`, "[0, 0.5]\n"},
		{`p Rational(-7, 2) % 3`, "(5/2)\n"},
		{`p Rational(7, 2).modulo(Rational(2, 1))`, "(3/2)\n"},
		{`p Rational(7, 2) % 1.5`, "0.5\n"},
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	for _, c := range []struct{ src, want string }{
		{`Rational(1, 2).div(Rational(0, 1))`, "ZeroDivisionError"},
		{`Rational(1, 2).div(0.0)`, "ZeroDivisionError"},
		{`Rational(1, 2).divmod(0.0)`, "ZeroDivisionError"},
		{`Rational(1, 2).divmod("x")`, "TypeError"},
		{`Rational(1, 2) % 0.0`, "ZeroDivisionError"},
		{`Rational(1, 2).div(2, 3)`, "ArgumentError"},
	} {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want %q", c.src, err, c.want)
		}
	}
}

// TestRationalMisc covers zero?/integer?/magnitude/quo/coerce/rationalize/== and
// the shadowed Rational.new, plus the huge-exponent guard and 0**negative.
func TestRationalMisc(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`p Rational(0, 5).zero?`, "true\n"},
		{`p Rational(1, 2).zero?`, "false\n"},
		{`p Rational(2, 1).integer?`, "false\n"},
		{`p Rational(3, 2).magnitude`, "(3/2)\n"},
		{`p Rational(-3, 2).magnitude`, "(3/2)\n"},
		{`p Rational(3, 4).quo(2)`, "(3/8)\n"},
		{`p Rational(3, 2).coerce(2)`, "[(2/1), (3/2)]\n"},
		{`p Rational(3, 2).coerce(Rational(1, 4))`, "[(1/4), (3/2)]\n"},
		{`p Rational(3, 2).coerce(1.5)`, "[1.5, 1.5]\n"},
		{`p Rational(12, 3).rationalize`, "(4/1)\n"},
		{`p Rational(3, 4).rationalize(0)`, "(3/4)\n"},                   // zero eps: lo == hi
		{`p Rational(1, 10).rationalize(Rational(2, 10))`, "(0/1)\n"},    // interval straddles 0
		{`p Rational(-45, 7).rationalize(Rational(1, 10))`, "(-13/2)\n"}, // negative interval
		{`p Rational(45, 7).rationalize(0.1)`, "(13/2)\n"},               // Float eps
		{`p(Rational(1, 2) == Complex(0.5, 0))`, "true\n"},
		{`p(Rational(1, 2) == Object.new)`, "false\n"},
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	for _, c := range []struct{ src, want string }{
		{`Rational.new(1)`, "NoMethodError"},
		{`Rational(3, 2).coerce("x")`, "TypeError"},
		{`Rational(1, 2).rationalize(:sym)`, "TypeError"},
		{`Rational(1, 2).rationalize(0.1, 0.1)`, "ArgumentError"},
		{`Rational(2) ** (2 ** 64)`, "ArgumentError"}, // exponent too large
		{`Rational(0, 1) ** Rational(-1, 2)`, "ZeroDivisionError"},
	} {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want %q", c.src, err, c.want)
		}
	}
}

// TestRationalKernel covers Kernel#Rational: a single Float / to_r object arg,
// two-arg Float division, Bignum and Rational operands, and the coercion errors
// (nil / true / a plain object / an infinite Float / a zero denominator).
func TestRationalKernel(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`p Rational(3, 4)`, "(3/4)\n"},
		{`p Rational(1.5, 2)`, "(3/4)\n"},
		{`p Rational(1, 1.5)`, "(2/3)\n"},
		{`p Rational(2.5, 1.25)`, "(2/1)\n"},
		{`p Rational(Rational(1, 2))`, "(1/2)\n"},
		{`p Rational(10 ** 20)`, "(100000000000000000000/1)\n"},
		{`o = Object.new; def o.to_r; Rational(3, 5); end; p Rational(o)`, "(3/5)\n"},
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	for _, c := range []struct{ src, want string }{
		{`Rational(nil)`, "TypeError"},
		{`Rational(true)`, "TypeError"},
		{`Rational(Object.new)`, "TypeError"},
		{`Rational(Float::INFINITY)`, "FloatDomainError"}, // an infinite Float has no exact Rational (MRI's FloatDomainError)
		{`Rational(Object.new, 2)`, "TypeError"},          // first of two args not convertible
		{`Rational(1, Object.new)`, "TypeError"},
		{`Rational(1, 0.0)`, "ZeroDivisionError"},
	} {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want %q", c.src, err, c.want)
		}
	}
}

// TestNumericCoerce covers the numeric coercion protocol reached from binaryOp:
// a built-in number combined with a user object that answers #coerce re-runs the
// operator on the returned pair (for +, -, *, / and **), a coerce result that is
// not a two-element Array raises TypeError, and a numeric operand paired with a
// non-coercible object still raises the plain coercion TypeError.
func TestNumericCoerce(t *testing.T) {
	pre := "class C; def coerce(o); [o, Rational(1, 2)]; end; end; "
	for _, c := range []struct{ src, want string }{
		{pre + `p(Rational(1, 4) + C.new)`, "(3/4)\n"},
		{pre + `p(2 + C.new)`, "(5/2)\n"}, // Integer left operand
		{pre + `p(2.0 * C.new)`, "1.0\n"}, // Float left operand
		{pre + `p(Rational(3, 4) <=> C.new)`, "1\n"},
		{`class P; def coerce(o); [o, 2]; end; end; p(Rational(2, 3) ** P.new)`, "(4/9)\n"}, // ** via coerce
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	for _, c := range []struct{ src, want string }{
		{`class B; def coerce(o); 5; end; end; Rational(1, 2) + B.new`, "TypeError"},
		{`Rational(1, 2) + Object.new`, "TypeError"},
	} {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want %q", c.src, err, c.want)
		}
	}
}

// TestNumericHashAndUnbound covers the value-based Rational/Complex hashes and
// UnboundMethod equality (an alias equals its original; a distinct method and a
// non-UnboundMethod operand do not).
func TestNumericHashAndUnbound(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`p(Rational(2, 4).hash == Rational(1, 2).hash)`, "true\n"},
		{`p(Rational(2, 3).hash == Rational(2, 4).hash)`, "false\n"},
		{`p(Complex(1, 0).hash == Complex(1).hash)`, "true\n"},
		{`p(Complex(1, 2).hash == Complex(2, 1).hash)`, "false\n"},
		{`p(Rational.instance_method(:magnitude) == Rational.instance_method(:abs))`, "true\n"},
		{`p(Rational.instance_method(:magnitude).eql?(Rational.instance_method(:abs)))`, "true\n"},
		{`p(Rational.instance_method(:floor) == Rational.instance_method(:ceil))`, "false\n"},
		{`p(Rational.instance_method(:abs) == 5)`, "false\n"},
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestPureUserOperatorSend covers the send fast-path guard: an operator sent to a
// pure user object with no matching method resolves through method_missing rather
// than re-entering the operator fast path (which would loop).
func TestPureUserOperatorSend(t *testing.T) {
	src := `class M; def method_missing(n, *a); "#{n}:#{a.first}"; end; end; p(M.new.send(:+, 3))`
	if got := eval(t, src); got != "\"+:3\"\n" {
		t.Errorf("got=%q", got)
	}
}

package vm_test

import (
	"strings"
	"testing"
)

// TestComplex covers Ruby Complex: construction, inspection/to_s, the readers and
// trig helpers, arithmetic (with coercion both ways), equality, and negation —
// every value asserted against MRI 4.0.5.
func TestComplex(t *testing.T) {
	cases := []struct{ src, want string }{
		// Construction + inspect (Integer parts kept; sign of the imaginary term).
		{`p Complex(1, 2)`, "(1+2i)\n"},
		{`p Complex(1, -2)`, "(1-2i)\n"},
		{`p Complex(3)`, "(3+0i)\n"}, // imaginary defaults to 0
		{`p Complex(1.5, 2)`, "(1.5+2i)\n"},
		// to_s drops the parentheses.
		{`puts Complex(1, 2)`, "1+2i\n"},
		{`puts Complex(1, -2)`, "1-2i\n"},
		// Readers.
		{`p Complex(1, 2).real`, "1\n"},
		{`p Complex(1, 2).imaginary`, "2\n"},
		{`p Complex(1, 2).imag`, "2\n"},
		{`p Complex(1, 2).abs2`, "5\n"},
		{`p Complex(3, 4).abs`, "5.0\n"},
		{`p Complex(3, 4).magnitude`, "5.0\n"},
		// arg/angle/phase are atan2-based; round to keep the assertion stable across
		// architectures (qemu libm differs in the last ULP).
		{`p Complex(0, 1).arg.round(9)`, "1.570796327\n"},
		{`p Complex(0, 1).angle.round(9)`, "1.570796327\n"},
		{`p Complex(0, 1).phase.round(9)`, "1.570796327\n"},
		{`p Complex(1, 2).conjugate`, "(1-2i)\n"},
		{`p Complex(1, 2).conj`, "(1-2i)\n"},
		{`p Complex(1, 2).rectangular`, "[1, 2]\n"},
		{`p Complex(1, 2).rect`, "[1, 2]\n"},
		{`p Complex(3, 4).polar.map { |x| x.round(9) }`, "[5.0, 0.927295218]\n"},
		{`p Complex(1, 2).to_s`, "\"1+2i\"\n"},
		{`p Complex(1, 2).inspect`, "\"(1+2i)\"\n"},
		// Arithmetic — exact on integer components for +/-/*, float for /.
		{`p Complex(1, 2) + Complex(3, 4)`, "(4+6i)\n"},
		{`p Complex(1, 2) - Complex(3, 4)`, "(-2-2i)\n"},
		{`p Complex(1, 2) * Complex(3, 4)`, "(-5+10i)\n"},
		{`p Complex(0, 1) * Complex(0, 1)`, "(-1+0i)\n"},
		{`p Complex(1.0, 2) / Complex(1, 1)`, "(1.5+0.5i)\n"},
		// Coercion of a real number, either operand order.
		{`p 2 + Complex(1, 1)`, "(3+1i)\n"},
		{`p Complex(1, 1) + 2`, "(3+1i)\n"},
		// Equality, including Complex(x, 0) == x in both orders.
		{`p Complex(2, 0) == 2`, "true\n"},
		{`p 2 == Complex(2, 0)`, "true\n"},
		{`p Complex(1, 2) == Complex(1, 2)`, "true\n"},
		{`p Complex(1, 2) == Complex(1, 3)`, "false\n"},
		{`p Complex(1, 0) == "x"`, "false\n"},
		// Negation, class, truthiness.
		{`p(-Complex(1, 2))`, "(-1-2i)\n"},
		{`p Complex(1, 2).class`, "Complex\n"},
		{`p(Complex(0, 0) ? "y" : "n")`, "\"y\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestComplexErrors covers the raising paths: non-numeric construction args, a
// non-coercible operand, and an operator Complex does not define.
func TestComplexErrors(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`Complex("a")`, "ArgumentError"}, // an unparseable String is ArgumentError (MRI's "invalid value for convert()")
		{`Complex(1, "b")`, "TypeError"},  // a non-real second argument is still a TypeError
		{`Complex(1, 2) + "x"`, "TypeError"},
		{`true + Complex(1, 1)`, "TypeError"},
		{`Complex(1, 2) % 1`, "NoMethodError"},
	} {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want %q", c.src, err, c.want)
		}
	}
}

// TestComplexConversions covers Complex#to_f/#to_i/#to_r/#to_c/#rationalize —
// including the exact-zero vs numeric-zero imaginary rule and the RangeError /
// ArgumentError paths — plus real?/integer? and the numerator/denominator pair.
func TestComplexConversions(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`p Complex(3, 0).to_f`, "3.0\n"},
		{`p Complex(3, 0).to_i`, "3\n"},
		{`p Complex(3, 0).to_r`, "(3/1)\n"},
		{`p Complex(3, 0.0).to_r`, "(3/1)\n"}, // to_r accepts a Float 0.0 imaginary
		{`p Complex(3, 4).to_c.equal?(Complex(3, 4).to_c)`, "false\n"},
		{`p Complex(3, 4).to_c == Complex(3, 4)`, "true\n"},
		{`p Complex(Rational(3, 4), 0).rationalize`, "(3/4)\n"},
		{`p Complex(3, 0).real?`, "false\n"},
		{`p Complex(3, 4).integer?`, "false\n"},
		{`p Complex(Rational(3, 4), Rational(1, 2)).numerator`, "(3+2i)\n"},
		{`p Complex(Rational(3, 4), Rational(1, 2)).denominator`, "4\n"},
		{`p Complex(3, 4).fdiv(2)`, "(1.5+2.0i)\n"},
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	for _, c := range []struct{ src, want string }{
		{`Complex(3, 4).to_f`, "RangeError"},
		{`Complex(3, 4).to_i`, "RangeError"},
		{`Complex(3, 4).to_r`, "RangeError"},
		{`Complex(3, 0.0).to_f`, "RangeError"}, // to_f rejects a Float 0.0 imaginary
		{`Complex(3, 4).rationalize`, "RangeError"},
		{`Complex(3, 0).rationalize(0.1, 0.2)`, "ArgumentError"},
	} {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want %q", c.src, err, c.want)
		}
	}
}

// TestComplexArithmeticExtra covers exact division (quo semantics), the
// zero-divisor rules, exponentiation (whole/negative/Complex exponents), finite?/
// infinite?, coerce, quo, eql?, <=>, and the tombstoned negative?/positive?.
func TestComplexArithmeticExtra(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`p Complex(3, 4) / 2`, "((3/2)+2i)\n"}, // exact, not float
		{`p Complex(3, 4).quo(2)`, "((3/2)+2i)\n"},
		{`p Complex(1.0, 2) / Complex(1, 1)`, "(1.5+0.5i)\n"}, // float component
		{`p Complex(2, 3) ** 2`, "(-5+12i)\n"},                // whole exponent, exact
		{`p Complex(2, 3) ** -1`, "((2/13)-(3/13)*i)\n"},      // negative exponent, exact
		{`p Complex(2, 3) ** 0`, "(1+0i)\n"},
		{`p Complex(3, 4).finite?`, "true\n"},
		{`p Complex(3, Float::INFINITY).finite?`, "false\n"},
		{`p Complex(3, 4).infinite?`, "nil\n"},
		{`p Complex(Float::INFINITY, 0).infinite?`, "1\n"},
		{`p Complex(3, 0).coerce(2)`, "[(2+0i), (3+0i)]\n"},
		{`p Complex(3, 0).coerce(Complex(1, 1))`, "[(1+1i), (3+0i)]\n"},
		{`p Complex(1, 2).eql?(Complex(1, 2))`, "true\n"},
		{`p Complex(1, 2).eql?(Complex(1.0, 2.0))`, "false\n"}, // different part classes
		{`p Complex(1, 2).eql?(5)`, "false\n"},
		{`p(Complex(3, 0) <=> Complex(5, 0))`, "-1\n"},
		{`p(Complex(3, 0) <=> 5.5)`, "-1\n"},
		{`p(Complex(3, 2) <=> 5)`, "nil\n"},             // non-real self
		{`p(Complex(3, 0) <=> Complex(5, 1))`, "nil\n"}, // non-real other
		{`p(Complex(3, 0) <=> "x")`, "nil\n"},           // non-numeric other
		{`p Complex(3, 4).respond_to?(:negative?)`, "false\n"},
		{`p Complex(3, 4).respond_to?(:positive?)`, "false\n"},
		{`p Complex(1, Float::INFINITY).to_s`, "\"1+Infinity*i\"\n"},
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	for _, c := range []struct{ src, want string }{
		{`Complex(3, 4) / 0`, "ZeroDivisionError"},
		{`Complex(3, 0).coerce("x")`, "TypeError"},
	} {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want %q", c.src, err, c.want)
		}
	}
}

// TestComplexClassAndAliases covers Complex::I, the class constructors
// rectangular/rect/polar, the true-alias identities, and the ** coercion path,
// plus Complex ** Complex (exp(w·ln z)).
func TestComplexClassAndAliases(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`p Complex::I`, "(0+1i)\n"},
		{`p Complex.rectangular(1, 2)`, "(1+2i)\n"},
		{`p Complex.rect(3)`, "(3+0i)\n"},
		{`p Complex.polar(2, 0).real.round(6)`, "2.0\n"},
		{`p Complex(1, 2).rectangular`, "[1, 2]\n"},
		{`p Complex.instance_method(:angle) == Complex.instance_method(:arg)`, "true\n"},
		{`p Complex.instance_method(:conj) == Complex.instance_method(:conjugate)`, "true\n"},
		{`p Complex.instance_method(:imag) == Complex.instance_method(:imaginary)`, "true\n"},
		{`p Complex.instance_method(:magnitude) == Complex.instance_method(:abs)`, "true\n"},
		{`r = (Complex(3, 4) ** Complex(1, 1)); p [r.real.round(6), r.imaginary.round(6)]`, "[-1.627159, 1.124846]\n"},
		{`r = (Complex(2, 3) ** 0.5); p [r.real.round(6), r.imaginary.round(6)]`, "[1.674149, 0.895977]\n"}, // real non-integer exponent
		{`class Q; def coerce(o); [o, 2]; end; end; p(Complex(2, 0) ** Q.new)`, "(4+0i)\n"},
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	for _, c := range []struct{ src, want string }{
		{`Complex.rectangular(Complex(1, 1))`, "TypeError"},
		{`Complex.rectangular(1, Complex(1, 1))`, "TypeError"}, // non-real imaginary arg
		{`Complex(1, 2) ** Object.new`, "TypeError"},           // non-numeric, non-coercible exponent
		{`Complex.polar("x")`, "TypeError"},
		{`Complex.polar(1, "x")`, "TypeError"},
		{`Complex(1, 2).rectangular(1)`, "ArgumentError"},
	} {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want %q", c.src, err, c.want)
		}
	}
}

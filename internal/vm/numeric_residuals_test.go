package vm_test

import (
	"strings"
	"testing"
)

// TestComplexRationalResiduals covers the MRI 4.0.6 conformance work on
// core/complex and core/rational: construction accepting any real Numeric,
// #/ + #quo aliasing, real-divisor division, #to_s/#inspect over Numeric
// subclasses, negative-base fractional exponentiation, marshal_dump, and the
// reflexive #== fallback.
func TestComplexRationalResiduals(t *testing.T) {
	// numeric subclass fixtures reused across cases: a formattable numeric whose
	// parts drive #to_s / #inspect / sign / negation, a non-real numeric, and a
	// numeric whose #to_s returns a non-String (to exercise the strVal fallback).
	const prelude = `
class MyNum < Numeric
  def initialize(v); @v = v; end
  def to_s; @v.to_s; end
  def inspect; "<#{@v}>"; end
  def <(o); @v < 0; end
  def -@; MyNum.new(-@v); end
end
class NotReal < Numeric
  def real?; false; end
end
class Weird < Numeric
  def to_s; 5; end
  def <(o); false; end
end
`
	cases := []struct{ name, src, want string }{
		// --- constructors accept real Numerics / zero-imaginary Complex ---
		{"ctor_string", `puts Complex("1+2i")`, "1+2i\n"},
		{"ctor_lone_complex", `puts Complex(Complex(1, 2))`, "1+2i\n"},
		{"ctor_real_parts", `puts Complex(1, 2)`, "1+2i\n"},
		{"ctor_subclass_parts", prelude + `c = Complex(MyNum.new(1), MyNum.new(2)); puts c.real.to_s; puts c.imag.to_s`, "1\n2\n"},
		{"rect_zero_imag_complex", `c = Complex.rectangular(Complex(1, 0), Complex(2, 0)); puts c.real; puts c.imag`, "1\n2\n"},
		{"rect_real", `c = Complex.rectangular(3, 4); puts c`, "3+4i\n"},

		// --- #to_s / #inspect over built-ins and Numeric subclasses ---
		{"to_s_builtin", `puts Complex(1, 2).to_s`, "1+2i\n"},
		{"inspect_builtin", `puts Complex(1, 2).inspect`, "(1+2i)\n"},
		{"to_s_neg_builtin", `puts Complex(1, -2).to_s`, "1-2i\n"},
		{"to_s_subclass", prelude + `puts Complex(MyNum.new(1), MyNum.new(2)).to_s`, "1+2i\n"},
		{"inspect_subclass_star", prelude + `puts Complex(MyNum.new(1), MyNum.new(2)).inspect`, "(<1>+<2>*i)\n"},
		{"to_s_subclass_neg", prelude + `puts Complex(MyNum.new(1), MyNum.new(-2)).to_s`, "1-2i\n"},
		{"to_s_strval_fallback", prelude + `puts Complex(Weird.new, Weird.new).to_s`, "5+5i\n"},

		// --- division: real divisor divides each part; /0.0 → signed Infinity ---
		{"div_real_exact", `puts Complex(4, 8) / 2`, "2+4i\n"},
		{"div_zero_infinity", `c = Complex(20, 40) / 0.0; puts c.real.infinite?; puts c.imag.infinite?`, "1\n1\n"},
		{"div_zero_neg", `puts((Complex(-20, 40) / 0.0).real.infinite?)`, "-1\n"},
		{"div_complex_divisor", `puts Complex(1, 1) / Complex(1, 1)`, "1+0i\n"},

		// --- #/ is a real method with #quo as its true alias ---
		{"complex_quo_is_div", `puts(Complex.instance_method(:quo) == Complex.instance_method(:/))`, "true\n"},
		{"complex_quo_call", `puts Complex(6, 0).quo(Complex(2, 0))`, "3+0i\n"},
		{"complex_div_send", `puts Complex(6, 0).send(:/, Complex(2, 0))`, "3+0i\n"},
		{"rational_quo_is_div", `puts(Rational.instance_method(:quo) == Rational.instance_method(:/))`, "true\n"},
		{"rational_quo_call", `puts Rational(1, 2).quo(Rational(1, 3))`, "3/2\n"},

		// --- negative-base fractional exponent → principal Complex root ---
		{"rat_pow_rat_complex", `x = Rational(-2, 1) ** Rational(1, 3); puts x.class`, "Complex\n"},
		{"rat_pow_float_complex", `x = Rational(-3, 2) ** 1.5; puts x.class; puts((x - Complex(0.0, -1.8371173070873836)).abs < 1e-6)`, "Complex\ntrue\n"},
		{"rat_pow_pos_float_real", `puts((Rational(3, 1) ** 1.5).class)`, "Float\n"},
		{"rat_pow_neg_int_float_real", `puts Rational(-2, 1) ** 2.0`, "4.0\n"},
		{"rat_pow_pos_rat_real", `puts((Rational(3, 4) ** Rational(4, 3)).class)`, "Float\n"},

		// --- reflexive #== delegates to a non-numeric right operand ---
		{"complex_eq_reflexive", `o = Object.new; def o.==(x); :yes; end; puts(Complex(3, 0) == o)`, "true\n"},
		{"complex_neq_reflexive", `o = Object.new; def o.==(x); true; end; puts(Complex(3, 0) != o)`, "false\n"},
		{"complex_eq_numeric", `puts(Complex(3, 0) == 3)`, "true\n"},
		{"rational_eq_reflexive", `o = Object.new; def o.==(x); :yes; end; puts(Rational(3, 4) == o)`, "true\n"},
		{"rational_eq_numeric", `puts(Rational(4, 2) == 2)`, "true\n"},

		// --- marshal_dump (private) returns the component array ---
		{"complex_marshal_dump", `p Complex(1, 2).send(:marshal_dump)`, "[1, 2]\n"},
		{"rational_marshal_dump", `p Rational(3, 4).send(:marshal_dump)`, "[3, 4]\n"},
		{"complex_marshal_private", `puts Complex.private_instance_methods(false).include?(:marshal_dump)`, "true\n"},
		{"complex_marshal_roundtrip", `puts Marshal.load(Marshal.dump(Complex(1, 2)))`, "1+2i\n"},
		{"rational_marshal_roundtrip", `puts Marshal.load(Marshal.dump(Rational(3, 4)))`, "3/4\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestComplexRationalResidualErrors covers the raising branches of the
// constructors and rectangular: a non-real component is a TypeError, an invalid
// Complex string is an ArgumentError.
func TestComplexRationalResidualErrors(t *testing.T) {
	const prelude = `
class NotReal < Numeric
  def real?; false; end
end
`
	cases := []struct{ name, src, want string }{
		{"ctor_symbol", `Complex(:sym)`, "TypeError"},
		{"ctor_imag_symbol", `Complex(1, :sym)`, "TypeError"},
		{"ctor_not_real_numeric", prelude + `Complex(NotReal.new)`, "TypeError"},
		{"ctor_invalid_string", `Complex("not a number!")`, "ArgumentError"},
		{"rect_nonzero_imag_complex", `Complex.rectangular(Complex(1, 1), 2)`, "TypeError"},
		{"rect_imag_nonzero_complex", `Complex.rectangular(1, Complex(2, 3))`, "TypeError"},
		{"rect_symbol", `Complex.rectangular(:sym)`, "TypeError"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := runErr(t, tc.src)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("src=%q: got err=%v, want containing %q", tc.src, err, tc.want)
			}
		})
	}
}

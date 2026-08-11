package vm_test

import "testing"

// mathRescue wraps expr so a raised exception prints "Class: message" and a
// normal result prints its inspect form, letting one eval harness assert both
// return values and exact MRI error messages.
func mathRescue(expr string) string {
	return "begin\np(" + expr + ")\nrescue => e\nputs \"#{e.class}: #{e.message}\"\nend"
}

// TestMathConstantsAndValues covers the constants and every function's normal
// (in-domain) result. Transcendentals are rounded to stay architecture-stable.
func TestMathConstantsAndValues(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p Math::PI`, "3.141592653589793\n"},
		{`p Math::E`, "2.718281828459045\n"},
		// Unary, no domain restriction.
		{`p Math.cbrt(-8)`, "-2.0\n"},
		{`p Math.exp(0)`, "1.0\n"},
		{`p Math.sin(0)`, "0.0\n"},
		{`p Math.cos(0)`, "1.0\n"},
		{`p Math.tan(0)`, "0.0\n"},
		{`p Math.atan(1).round(9)`, "0.785398163\n"},
		{`p Math.sinh(0)`, "0.0\n"},
		{`p Math.cosh(0)`, "1.0\n"},
		{`p Math.tanh(0)`, "0.0\n"},
		{`p Math.asinh(1).round(9)`, "0.881373587\n"},
		{`p Math.erf(1).round(9)`, "0.842700793\n"},
		{`p Math.erfc(1).round(9)`, "0.157299207\n"},
		// Unary with a domain restriction, in-domain results.
		{`p Math.sqrt(2).round(9)`, "1.414213562\n"},
		{`p Math.sqrt(0)`, "0.0\n"},
		{`p Math.sqrt(-0.0)`, "0.0\n"}, // MRI normalises to +0.0 (not -0.0)
		{`p Math.log2(8)`, "3.0\n"},
		{`p Math.log2(0)`, "-Infinity\n"},
		{`p Math.log10(1000)`, "3.0\n"},
		{`p Math.asin(0.5).round(9)`, "0.523598776\n"},
		{`p Math.acos(0.5).round(9)`, "1.047197551\n"},
		{`p Math.atanh(0.5).round(9)`, "0.549306144\n"},
		{`p Math.atanh(1)`, "Infinity\n"},
		{`p Math.acosh(1)`, "0.0\n"},
		// log with and without a base; log(0) is -Infinity.
		{`p Math.log(Math::E).round(9)`, "1.0\n"},
		{`p Math.log(0)`, "-Infinity\n"},
		{`p Math.log(8, 2).round(9)`, "3.0\n"},
		{`p Math.log(0, 10)`, "-Infinity\n"},
		// A Bignum too large for float64 keeps full precision (bit-length reduction)
		// instead of overflowing to Infinity.
		{`p Math.log2(2**10001 + 45677544234809571)`, "10001.0\n"},
		{`p Math.log(2**5000).round(6)`, "3465.735903\n"},
		{`p Math.log10(10**400).round(6)`, "400.0\n"},
		{`p Math.log(2**10000, 2**5000).round(6)`, "2.0\n"},             // huge Bignum base too
		{`p Math.log2(2**101 + 45677544234809571).round(5)`, "101.0\n"}, // small Bignum: direct path
		// Binary.
		{`p Math.hypot(3, 4)`, "5.0\n"},
		{`p Math.atan2(1, 1).round(9)`, "0.785398163\n"},
		// gamma / lgamma / frexp / ldexp.
		{`p Math.gamma(5)`, "24.0\n"},
		{`p Math.gamma(0)`, "Infinity\n"},
		{`p Math.gamma(-0.0)`, "-Infinity\n"},
		{`p Math.gamma(0.5).round(9)`, "1.772453851\n"},
		{`p Math.gamma(-2.15).round(6)`, "-2.999619\n"},
		{`p Math.gamma(Float::INFINITY)`, "Infinity\n"},
		{`p Math.lgamma(0)`, "[Infinity, 1]\n"},
		{`p Math.lgamma(-0.0)`, "[Infinity, -1]\n"}, // Go sign is +1; MRI is -1
		{`p Math.lgamma(-1)[0]`, "Infinity\n"},
		{`f, s = Math.lgamma(0.5); p [f.round(9), s]`, "[0.572364943, 1]\n"},
		{`f, s = Math.lgamma(-0.5); p [f.round(7), s]`, "[1.2655121, -1]\n"},
		{`p Math.lgamma(Float::INFINITY)`, "[Infinity, 1]\n"},
		{`p Math.lgamma(Float::NAN)[0].nan?`, "true\n"},
		{`frac, exp = Math.frexp(1234.5); p [frac, exp]`, "[0.602783203125, 11]\n"},
		{`p Math.frexp(0.0)`, "[0.0, 0]\n"},
		{`p Math.frexp(Float::NAN)[0].nan?`, "true\n"},
		{`p Math.ldexp(0.5, 11)`, "1024.0\n"},
		{`p Math.ldexp(0.5, 11.9)`, "1024.0\n"}, // Float exponent truncates toward zero
		{`p Math.ldexp(-1.25, 2)`, "-5.0\n"},
		{`p Math.ldexp(0.5, -2147483648)`, "0.0\n"}, // int32 lower bound is valid
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestMathDomainErrors covers every DomainError branch and its exact MRI message
// (which names the offending function).
func TestMathDomainErrors(t *testing.T) {
	dom := func(fn string) string { return "Math::DomainError: Numerical argument is out of domain - " + fn + "\n" }
	cases := []struct{ src, want string }{
		{`Math.sqrt(-1)`, dom("sqrt")},
		{`Math.log(-1)`, dom("log")},
		{`Math.log(8, -2)`, dom("log")}, // negative base
		{`Math.log2(-1)`, dom("log2")},
		{`Math.log10(-1)`, dom("log10")},
		{`Math.asin(2)`, dom("asin")},
		{`Math.acos(2)`, dom("acos")},
		{`Math.atanh(2)`, dom("atanh")},
		{`Math.acosh(0.5)`, dom("acosh")},
		{`Math.log2(-(2**5000))`, dom("log2")},           // negative Bignum via mathLogArg
		{`Math.gamma(-1)`, dom("gamma")},                 // negative integer
		{`Math.gamma(-Float::INFINITY)`, dom("gamma")},   // -Infinity
		{`Math.lgamma(-Float::INFINITY)`, dom("lgamma")}, // -Infinity
	}
	for _, c := range cases {
		if got := eval(t, mathRescue(c.src)); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestMathFloatCoercion covers mathFloat: direct numerics, a Numeric subclass
// coerced through #to_f, and the TypeError naming of every non-coercible kind.
func TestMathFloatCoercion(t *testing.T) {
	cases := []struct{ src, want string }{
		{`class MyNum < Numeric; def to_f; 4.0; end; end` + "\n" + `p Math.sqrt(MyNum.new)`, "2.0\n"},
		{mathRescue(`Math.sqrt(:x)`), "TypeError: can't convert Symbol into Float\n"},
		{mathRescue(`Math.sqrt("2")`), "TypeError: can't convert String into Float\n"},
		{mathRescue(`Math.sqrt(nil)`), "TypeError: can't convert nil into Float\n"},
		{mathRescue(`Math.sqrt(true)`), "TypeError: can't convert true into Float\n"},
		{mathRescue(`Math.sqrt(false)`), "TypeError: can't convert false into Float\n"},
		{mathRescue(`Math.atan2("x", 1)`), "TypeError: can't convert String into Float\n"},
		{mathRescue(`Math.atan2(1, "x")`), "TypeError: can't convert String into Float\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestMathLdexpExpCoercion covers mathLdexpExp: Integer/Float truncation, the
// to_int protocol, and every RangeError/TypeError message MRI emits for an
// out-of-range or non-integer exponent.
func TestMathLdexpExpCoercion(t *testing.T) {
	cases := []struct{ src, want string }{
		// to_int protocol (non-Numeric object with #to_int).
		{`class MyInt; def to_int; 2; end; end` + "\n" + `p Math.ldexp(3.23, MyInt.new).round(2)`, "12.92\n"},
		// Integer out of the signed 32-bit range.
		{mathRescue(`Math.ldexp(0.5, 2147483648)`), "RangeError: integer 2147483648 too big to convert to 'int'\n"},
		{mathRescue(`Math.ldexp(0.5, -2147483649)`), "RangeError: integer -2147483649 too small to convert to 'int'\n"},
		// Bignum exponent (exceeds a machine long).
		{mathRescue(`Math.ldexp(0.5, 2**70)`), "RangeError: bignum too big to convert into 'long'\n"},
		{mathRescue(`Math.ldexp(0.5, -(2**70))`), "RangeError: bignum too big to convert into 'long'\n"},
		{`class BigTo; def to_int; 2**70; end; end` + "\n" + mathRescue(`Math.ldexp(0.5, BigTo.new)`), "RangeError: bignum too big to convert into 'long'\n"},
		// Float exponent that fits a long but not a C int.
		{mathRescue(`Math.ldexp(0.5, 3e9)`), "RangeError: integer 3000000000 too big to convert to 'int'\n"},
		// Float exponent beyond a machine long.
		{mathRescue(`Math.ldexp(0.5, 1e30)`), "RangeError: float 1e+30 out of range of integer\n"},
		{mathRescue(`Math.ldexp(0.5, -1e30)`), "RangeError: float -1e+30 out of range of integer\n"},
		// NaN / +Infinity / -Infinity exponents.
		{mathRescue(`Math.ldexp(0.5, Float::NAN)`), "RangeError: float NaN out of range of integer\n"},
		{mathRescue(`Math.ldexp(0.5, Float::INFINITY)`), "RangeError: float Inf out of range of integer\n"},
		{mathRescue(`Math.ldexp(0.5, -Float::INFINITY)`), "RangeError: float -Inf out of range of integer\n"},
		// nil / Symbol / bad #to_int result.
		{mathRescue(`Math.ldexp(0.5, nil)`), "TypeError: no implicit conversion from nil to integer\n"},
		{mathRescue(`Math.ldexp(0.5, :x)`), "TypeError: no implicit conversion of Symbol into Integer\n"},
		{`class BadInt; def to_int; "x"; end; end` + "\n" + mathRescue(`Math.ldexp(0.5, BadInt.new)`),
			"TypeError: can't convert BadInt to Integer (BadInt#to_int gives String)\n"},
		// First argument still coerces with Float() semantics.
		{mathRescue(`Math.ldexp(:x, 3)`), "TypeError: can't convert Symbol into Float\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestMathPrivateInstanceMethods checks that, like MRI's module_function, the
// Math functions are reachable as (private) instance methods on an includer.
func TestMathPrivateInstanceMethods(t *testing.T) {
	cases := []struct{ src, want string }{
		{"class IM\n  include Math\nend\np IM.new.send(:sqrt, 4)", "2.0\n"},
		{"class IM2\n  include Math\nend\np IM2.new.send(:ldexp, 3.1415, 2).round(3)", "12.566\n"},
		{"class IM3\n  include Math\nend\nfrac, exp = IM3.new.send(:frexp, 2.1415)\np exp", "2\n"},
		// module_function makes them private, not public, instance methods.
		{`p Math.private_instance_methods(false).include?(:sqrt)`, "true\n"},
		{`p Math.public_instance_methods(false).include?(:sqrt)`, "false\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

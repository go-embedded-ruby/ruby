// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"math"
	"strconv"
	"strings"
	"testing"
)

// CMath (github.com/go-ruby-cmath/cmath) is the complex-aware Math: every value
// below is asserted against MRI 4.0.5's `ruby -rcmath` — both the real-vs-Complex
// result SHAPE (exactly where MRI chooses each branch) and the component value(s)
// within floating-point tolerance, so last-ULP and Integer-0-vs-Float-0.0
// inspect differences do not make the test brittle.

const cmathEps = 1e-9

// cmathVal is the parsed shape of a CMath result: a real Float, or a Complex
// with real and imaginary parts. The cmplx flag is the load-bearing assertion —
// it must match MRI's branch choice exactly.
type cmathVal struct {
	re, im float64
	cmplx  bool
}

// parseCMath parses rbgo's printed inspect of a CMath result (either "2.0" or
// "(0.0+2.0i)") into a cmathVal. It fatals on anything it cannot parse so a
// regression in the output shape surfaces immediately.
func parseCMath(t *testing.T, s string) cmathVal {
	t.Helper()
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "(") {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			t.Fatalf("not a real Float: %q (%v)", s, err)
		}
		return cmathVal{re: f}
	}
	// "(re±imi)" — strip the parens and trailing "i", then split on the sign
	// that joins the two parts (skipping a leading sign on the real part and
	// any exponent sign).
	body := strings.TrimSuffix(strings.TrimPrefix(s, "("), ")")
	body = strings.TrimSuffix(body, "i")
	split := -1
	for i := 1; i < len(body); i++ {
		c := body[i]
		if (c == '+' || c == '-') && body[i-1] != 'e' && body[i-1] != 'E' {
			split = i
		}
	}
	if split < 0 {
		t.Fatalf("not a Complex inspect: %q", s)
	}
	re, err1 := strconv.ParseFloat(body[:split], 64)
	im, err2 := strconv.ParseFloat(body[split:], 64)
	if err1 != nil || err2 != nil {
		t.Fatalf("bad Complex parts in %q (%v / %v)", s, err1, err2)
	}
	return cmathVal{re: re, im: im, cmplx: true}
}

// closeEnough reports whether got matches want within tolerance (relative for
// large magnitudes), treating two NaNs as equal.
func closeEnough(got, want float64) bool {
	if math.IsNaN(got) && math.IsNaN(want) {
		return true
	}
	d := math.Abs(got - want)
	return d <= cmathEps || d <= cmathEps*math.Abs(want)
}

// assertCMath evaluates `p <expr>` and checks the result's shape and components
// against the MRI 4.0.5 reference (want).
func assertCMath(t *testing.T, expr string, want cmathVal) {
	t.Helper()
	got := parseCMath(t, eval(t, "p "+expr))
	if got.cmplx != want.cmplx {
		t.Errorf("%s: shape mismatch got cmplx=%v want cmplx=%v (got %+v)", expr, got.cmplx, want.cmplx, got)
		return
	}
	if !closeEnough(got.re, want.re) || (want.cmplx && !closeEnough(got.im, want.im)) {
		t.Errorf("%s: value mismatch got %+v want %+v", expr, got, want)
	}
}

// TestCMath covers every CMath module-function on both its real and complex
// branches, the two-argument log(x, base) base form, Complex-argument input,
// and the inverse-trig/hyperbolic branch-cut cases — each asserted against MRI
// 4.0.5 (`ruby -rcmath -e ...`).
func TestCMath(t *testing.T) {
	r := func(x float64) cmathVal { return cmathVal{re: x} }
	c := func(re, im float64) cmathVal { return cmathVal{re: re, im: im, cmplx: true} }

	for _, tc := range []struct {
		expr string
		want cmathVal
	}{
		// sqrt: real for x>=0, complex for x<0; Complex argument stays complex.
		{`CMath.sqrt(4)`, r(2.0)},
		{`CMath.sqrt(0)`, r(0.0)},
		{`CMath.sqrt(-4)`, c(0.0, 2.0)},
		{`CMath.sqrt(Complex(3, 4))`, c(2.0, 1.0)},

		// cbrt: real for any real input; Complex argument is complex.
		{`CMath.cbrt(8)`, r(2.0)},
		{`CMath.cbrt(-8)`, c(1.0, 1.7320508075688772)},
		{`CMath.cbrt(Complex(0, 1))`, c(0.8660254037844387, 0.5)},

		// exp: real for real input; Complex argument is complex.
		{`CMath.exp(1)`, r(2.718281828459045)},
		{`CMath.exp(Complex(0, Math::PI))`, c(-1.0, 1.2246467991473532e-16)},

		// log: natural log; real for x>0, complex for x<=0; 2-arg base form.
		{`CMath.log(Math::E)`, r(1.0)},
		{`CMath.log(-1)`, c(0.0, math.Pi)},
		{`CMath.log(8, 2)`, r(3.0)},
		{`CMath.log(-8, 2)`, c(3.0, 4.532360141827194)},
		{`CMath.log(Complex(0, 1))`, c(0.0, 1.5707963267948966)},

		// log2 / log10: real for x>0, complex for x<0.
		{`CMath.log2(8)`, r(3.0)},
		{`CMath.log2(-8)`, c(3.0, 4.532360141827194)},
		{`CMath.log10(1000)`, r(3.0)},
		{`CMath.log10(-1)`, c(0.0, 1.3643763538418412)},

		// sin / cos / tan: real on the real line; Complex argument is complex.
		{`CMath.sin(1)`, r(0.8414709848078965)},
		{`CMath.sin(Complex(1, 1))`, c(1.2984575814159773, 0.6349639147847361)},
		{`CMath.cos(1)`, r(0.5403023058681398)},
		{`CMath.cos(Complex(1, 1))`, c(0.8337300251311491, -0.9888977057628651)},
		{`CMath.tan(1)`, r(1.557407724654902)},
		{`CMath.tan(Complex(1, 1))`, c(0.2717525853195117, 1.0839233273386946)},

		// sinh / cosh / tanh.
		{`CMath.sinh(1)`, r(1.1752011936438014)},
		{`CMath.cosh(1)`, r(1.5430806348152437)},
		{`CMath.tanh(1)`, r(0.7615941559557649)},
		{`CMath.sinh(Complex(1, 1))`, c(0.6349639147847361, 1.2984575814159773)},

		// asin / acos: real inside [-1,1], complex outside.
		{`CMath.asin(0.5)`, r(0.5235987755982989)},
		{`CMath.asin(2)`, c(1.5707963267948966, -1.3169578969248166)},
		{`CMath.acos(0.5)`, r(1.0471975511965979)},
		{`CMath.acos(2)`, c(0.0, 1.3169578969248164)},

		// atan: real for real input; complex on the imaginary-axis branch cut.
		{`CMath.atan(1)`, r(0.7853981633974483)},
		{`CMath.atan(Complex(0, 2))`, c(-1.5707963267948966, 0.5493061443340549)},

		// atan2: real for two reals; complex when an argument is complex.
		{`CMath.atan2(1, 1)`, r(0.7853981633974483)},
		{`CMath.atan2(Complex(1, 1), 1)`, c(1.0172219678978514, 0.4023594781085251)},

		// asinh: real everywhere on the real line.
		{`CMath.asinh(1)`, r(0.881373587019543)},

		// acosh: real for x>=1, complex below.
		{`CMath.acosh(2)`, r(1.3169578969248166)},
		{`CMath.acosh(0.5)`, c(0.0, 1.0471975511965976)},

		// atanh: real inside (-1,1), complex outside.
		{`CMath.atanh(0.5)`, r(0.5493061443340549)},
		{`CMath.atanh(2)`, c(0.5493061443340549, 1.5707963267948966)},
	} {
		assertCMath(t, tc.expr, tc.want)
	}
}

// TestCMathFloatInput exercises the Integer-vs-Float coercion path (both map to
// cmath.Real) and confirms require "cmath" is satisfied as a provided feature.
func TestCMathFloatInput(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`require "cmath"; p CMath.sqrt(4.0)`, "2.0\n"},   // Float argument
		{`require "cmath"; p CMath.sqrt(9)`, "3.0\n"},     // Integer argument
		{`p require "cmath"`, "true\n"},                   // provided feature → true
		{`require "cmath"; p require "cmath"`, "false\n"}, // already loaded → false
		{`p CMath.class`, "Module\n"},                     // it is a module
		{`p defined?(CMath)`, "\"constant\"\n"},           // constant is defined
		{`p CMath.respond_to?(:sqrt)`, "true\n"},          // module-function present
		{`p CMath.log(10 ** 30) > 0`, "true\n"},           // Bignum coerces through toFloat
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestCMathModuleFunction confirms the functions are module_function-style:
// usable as CMath.fn and, after include CMath, as (private) instance methods.
func TestCMathModuleFunction(t *testing.T) {
	src := `class Foo; include CMath; def s(x); sqrt(x); end; end; p Foo.new.s(-4).imaginary`
	if got := eval(t, src); got != "2.0\n" {
		t.Errorf("included module-function: got %q want %q", got, "2.0\n")
	}
}

// TestCMathErrors covers the TypeError path: a non-numeric argument (for the
// unary, the binary atan2, and the log functions), and a non-numeric Complex
// component.
func TestCMathErrors(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`CMath.sqrt("x")`, "TypeError"},     // unary, non-numeric
		{`CMath.atan2("x", 1)`, "TypeError"}, // binary, non-numeric first arg
		{`CMath.atan2(1, "x")`, "TypeError"}, // binary, non-numeric second arg
		{`CMath.log("x")`, "TypeError"},      // log, non-numeric arg
		{`CMath.log(8, "x")`, "TypeError"},   // log base, non-numeric
	} {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want %q", c.src, err, c.want)
		}
	}
}

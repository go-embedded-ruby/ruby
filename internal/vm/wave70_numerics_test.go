// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"math"
	"strings"
	"testing"
)

// TestIntegrateClosedForms checks the quadrature against integrals whose value
// is known exactly, not against the Go routines being bound. Each row is a
// definite integral a reader can verify by hand, including the two cases a
// quadrature binding most easily gets wrong: an infinite limit and an
// integrable singularity.
func TestIntegrateClosedForms(t *testing.T) {
	for _, tc := range []struct {
		expr string
		want float64
		tol  float64
		why  string
	}{
		{"Integrate.quad(0.0, 1.0) { |x| x * x }[0]", 1.0 / 3, 1e-12, "x^3/3"},
		{"Integrate.quad(0.0, Math::PI) { |x| Math.sin(x) }[0]", 2, 1e-12, "-cos over [0,pi]"},
		{"Integrate.quad(1.0, Math::E) { |x| 1.0 / x }[0]", 1, 1e-12, "log e - log 1"},
		{"Integrate.quad(0.0, Float::INFINITY) { |x| Math.exp(-x) }[0]", 1, 1e-10, "an infinite limit"},
		{"Integrate.quad(-Float::INFINITY, Float::INFINITY) { |x| Math.exp(-x * x) }[0]", math.Sqrt(math.Pi), 1e-10, "the Gaussian integral, both limits infinite"},
		{"Integrate.quad(0.0, 1.0) { |x| 1.0 / Math.sqrt(x) }[0]", 2, 1e-8, "an integrable singularity at 0"},
		// The abserr must be an error estimate, not a constant.
		{"Integrate.quad(0.0, 1.0) { |x| 1.0 }[1]", 0, 1e-12, "a constant integrand is exact"},

		// A Gauss-Legendre rule of n points is exact for degree 2n-1.
		{"Integrate.fixed_quad(0.0, 1.0, 3) { |x| x ** 4 }", 0.2, 1e-14, "3 points, degree 4 <= 5"},
		{"Integrate.fixed_quad(-1.0, 1.0, 2) { |x| x ** 3 + 1 }", 2, 1e-14, "2 points, degree 3"},

		{"Integrate.romberg(0.0, Math::PI) { |x| Math.sin(x) }", 2, 1e-10, "Richardson extrapolation"},

		// Sampled rules on data where the rule is exact.
		{"Integrate.trapezoid([0.0, 1.0, 4.0], dx: 1.0)", 3, 1e-14, "(0+1)/2 + (1+4)/2"},
		{"Integrate.trapezoid([0.0, 1.0], x: [0.0, 2.0])", 1, 1e-14, "x: overrides dx:"},
		{"Integrate.simpson([0.0, 0.25, 1.0, 2.25, 4.0], dx: 0.5)", 8.0 / 3, 1e-14, "Simpson is exact for x^2"},
		{"Integrate.romb([0.0, 0.25, 1.0, 2.25, 4.0], dx: 0.5)", 8.0 / 3, 1e-14, "Romberg on 2^2+1 samples"},

		{"Integrate.dblquad(0.0, 1.0, 0.0, 1.0) { |y, x| x * y }[0]", 0.25, 1e-12, "the unit square"},
		{"Integrate.nquad([[0.0, 1.0], [0.0, 2.0]]) { |v| 1.0 }[0]", 2, 1e-12, "the area of a 1x2 rectangle"},
	} {
		t.Run(tc.expr, func(t *testing.T) {
			got := evalFloat(t, tc.expr)
			if math.Abs(got-tc.want) > tc.tol {
				t.Errorf("%s = %v, want %v +/- %v (%s)", tc.expr, got, tc.want, tc.tol, tc.why)
			}
		})
	}
}

// TestIntegrateGaussLegendreNodes pins the two-point rule, whose nodes and
// weights are a closed form: +/-1/sqrt(3) with weight 1.
func TestIntegrateGaussLegendreNodes(t *testing.T) {
	src := `x, w = Integrate.gauss_legendre(2)
p [(x[0] + 1.0 / Math.sqrt(3)).abs < 1e-15,
   (x[1] - 1.0 / Math.sqrt(3)).abs < 1e-15,
   (w[0] - 1.0).abs < 1e-15,
   (w[1] - 1.0).abs < 1e-15,
   x.size, w.size]`
	const want = "[true, true, true, true, 2, 2]\n"
	if got := eval(t, src); got != want {
		t.Errorf("got %s want %s", got, want)
	}
}

// TestIntegrateFullOutput: the default answer is scipy's (value, abserr) pair,
// and full_output: true is the whole record, as scipy's full_output is.
func TestIntegrateFullOutput(t *testing.T) {
	src := `a = Integrate.quad(0.0, 1.0) { |x| x }
p a.class, a.size
h = Integrate.quad(0.0, 1.0, full_output: true) { |x| x }
p h.class, h.keys.sort
p (h[:value] - 0.5).abs < 1e-14, h[:neval] > 0, h[:nintervals] > 0`
	const want = "Array\n2\nHash\n[:abserr, :neval, :nintervals, :value]\ntrue\ntrue\ntrue\n"
	if got := eval(t, src); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// TestIntegrateCumulativeTrapezoid: initial: controls the leading 0, which is
// what makes the result the same length as the input or one shorter -- the one
// place scipy's cumulative_trapezoid surprises people.
func TestIntegrateCumulativeTrapezoid(t *testing.T) {
	src := `p Integrate.cumulative_trapezoid([0.0, 1.0, 2.0], dx: 1.0)
p Integrate.cumulative_trapezoid([0.0, 1.0, 2.0], dx: 1.0, initial: true)`
	const want = "[0.5, 2.0]\n[0.0, 0.5, 2.0]\n"
	if got := eval(t, src); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// TestOptimizeFindsKnownRoots: five root finders, two functions whose roots are
// known to full precision -- sqrt(2) and the Dottie number, the unique
// solution of cos x = x. Each is checked BOTH against the known value and by
// evaluating the function there, so a finder answering a plausible wrong number
// fails twice.
func TestOptimizeFindsKnownRoots(t *testing.T) {
	for _, finder := range []string{"brentq", "brenth", "bisect", "ridder"} {
		t.Run(finder+"/sqrt2", func(t *testing.T) {
			got := evalFloat(t, "Optimize."+finder+"(0.0, 2.0) { |x| x * x - 2 }")
			if math.Abs(got-math.Sqrt2) > 1e-11 {
				t.Errorf("%s = %v, want sqrt(2) = %v", finder, got, math.Sqrt2)
			}
			if r := evalFloat(t, "(Optimize."+finder+"(0.0, 2.0) { |x| x * x - 2 }) ** 2 - 2"); math.Abs(r) > 1e-10 {
				t.Errorf("%s: f at the root is %v, not 0", finder, r)
			}
		})
		t.Run(finder+"/dottie", func(t *testing.T) {
			got := evalFloat(t, "Optimize."+finder+"(0.0, 1.0) { |x| Math.cos(x) - x }")
			if math.Abs(got-0.7390851332151607) > 1e-11 {
				t.Errorf("%s = %v, want the Dottie number 0.7390851332151607", finder, got)
			}
		})
	}
	// newton with and without a derivative: the secant method and Newton-Raphson.
	t.Run("newton/secant", func(t *testing.T) {
		if got := evalFloat(t, "Optimize.newton(1.0) { |x| x * x - 2 }"); math.Abs(got-math.Sqrt2) > 1e-12 {
			t.Errorf("secant = %v, want sqrt(2)", got)
		}
	})
	t.Run("newton/fprime", func(t *testing.T) {
		if got := evalFloat(t, "Optimize.newton(1.0, fprime: ->(x) { 2 * x }) { |x| x * x - 2 }"); got != math.Sqrt2 {
			t.Errorf("Newton-Raphson = %v, want sqrt(2) = %v exactly", got, math.Sqrt2)
		}
	})
}

// TestOptimizeMinimises: minimisers on objectives whose minimiser is known.
// Rosenbrock is there because it is the standard hard case and its minimum is
// exactly (1, 1), so a method that merely descends somewhere fails.
func TestOptimizeMinimises(t *testing.T) {
	src := `ok = []
r = Optimize.minimize_scalar { |x| (x - 3.0) ** 2 }
ok << ((r[:x] - 3.0).abs < 1e-8) << (r[:fun].abs < 1e-14) << r[:success]
r = Optimize.minimize_scalar(method: "golden") { |x| (x - 3.0) ** 2 }
ok << ((r[:x] - 3.0).abs < 1e-6)
r = Optimize.minimize_scalar(bounds: [0.0, 1.0]) { |x| (x - 3.0) ** 2 }
ok << ((r[:x] - 1.0).abs < 1e-4)
ok << ((Optimize.fminbound(0.0, 10.0) { |x| (x - 3.0) ** 2 } - 3.0).abs < 1e-4)
%w[BFGS CG Nelder-Mead Powell L-BFGS-B].each do |m|
  r = Optimize.minimize([-1.2, 1.0], method: m, tol: 1e-10) { |v|
    100.0 * (v[1] - v[0] ** 2) ** 2 + (1.0 - v[0]) ** 2
  }
  ok << ((r[:x][0] - 1.0).abs < 1e-3) << ((r[:x][1] - 1.0).abs < 1e-3)
end
puts ok.count(false)
puts ok.size`
	out := strings.Fields(strings.TrimSpace(eval(t, src)))
	if len(out) != 2 || out[0] != "0" {
		t.Errorf("minimisation: %v of %v checks failed", out[0], out[1])
	}
	if out[1] != "16" {
		t.Errorf("made %s checks, expected 16 -- the loop stopped early", out[1])
	}
}

// TestOptimizeGradientAndFit: approx_fprime on a function whose gradient is
// constant, and curve_fit on data that lies exactly on the model, where the
// right answer is the model's own parameters to machine precision.
func TestOptimizeGradientAndFit(t *testing.T) {
	src := `g = Optimize.approx_fprime([1.0, 2.0]) { |v| 2.0 * v[0] + 3.0 * v[1] }
p [(g[0] - 2.0).abs < 1e-6, (g[1] - 3.0).abs < 1e-6, g.size]
xs = (0..4).map(&:to_f)
ys = xs.map { |x| 2.0 * x + 1.0 }
popt, pcov = Optimize.curve_fit(xs, ys, [0.0, 0.0]) { |x, p| p[0] * x + p[1] }
p [(popt[0] - 2.0).abs < 1e-9, (popt[1] - 1.0).abs < 1e-9, popt.size, pcov.size]`
	const want = "[true, true, 2]\n[true, true, 2, 4]\n"
	if got := eval(t, src); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// TestOptimizeBracket: the downhill search must return three points that
// bracket a minimum, which is a property of the answer, not a fixed triple.
func TestOptimizeBracket(t *testing.T) {
	src := `x, fx, nfev = Optimize.bracket(0.0, 1.0) { |z| (z - 3.0) ** 2 }
p [x.size, fx.size, x[0] < x[1], x[1] < x[2], fx[1] < fx[0], fx[1] < fx[2], nfev > 0]`
	const want = "[3, 3, true, true, true, true, true]\n"
	if got := eval(t, src); got != want {
		t.Errorf("got %s want %s", got, want)
	}
}

// TestNumericsBlockErrors: the block is the one argument these modules cannot
// type-check in advance, so a missing block and a block answering the wrong
// kind must each be a clear Ruby error rather than a NaN surfacing later or a
// Go fault.
func TestNumericsBlockErrors(t *testing.T) {
	for _, tc := range []struct{ expr, want string }{
		{"Integrate.quad(0.0, 1.0)", "no block given (yield)"},
		{"Optimize.brentq(0.0, 2.0)", "no block given (yield)"},
		{"Optimize.minimize([0.0])", "no block given (yield)"},
		{`Integrate.quad(0.0, 1.0) { |x| "no" }`, "the integrand must answer a Float, got String"},
		{`Optimize.brentq(0.0, 2.0) { |x| nil }`, "the function must answer a Float, got nil"},
		{`Optimize.minimize([0.0]) { |v| :sym }`, "the objective must answer a Float, got Symbol"},
		{`Optimize.newton(1.0, fprime: 3) { |x| x }`, "fprime: must be a Proc or lambda, got 3"},
		{`Optimize.minimize([0.0], method: 7) { |v| v[0] }`, "method: must be a String or Symbol, got 7"},
	} {
		t.Run(tc.expr, func(t *testing.T) {
			src := "begin\n  " + tc.expr + "\n  puts \"NO ERROR\"\nrescue => e\n  puts e.message\nend"
			if got := strings.TrimSpace(eval(t, src)); got != tc.want {
				t.Errorf("%s raised %q, want %q", tc.expr, got, tc.want)
			}
		})
	}
}

// TestNumericsDomainErrors: the module's own refusals reach Ruby as the error
// SciPy maps to, with the module's message -- not as a silent NaN.
func TestNumericsDomainErrors(t *testing.T) {
	for _, tc := range []struct{ expr, want string }{
		{"Optimize.brentq(2.0, 3.0) { |x| x * x - 2 }", "f(a) and f(b) must have different signs"},
		{`Optimize.minimize([0.0], method: "Nope") { |v| v[0] }`, "unknown method"},
		{"Integrate.gauss_legendre(0)", "n must be positive"},
		{"Integrate.nquad([[0.0]]) { |v| 1.0 }", "range 0 must be a pair [lo, hi], got 1 element(s)"},
		{`Optimize.minimize_scalar(bounds: [1.0]) { |x| x }`, "bounds must be a pair [lo, hi], got 1 element(s)"},
	} {
		t.Run(tc.expr, func(t *testing.T) {
			src := "begin\n  " + tc.expr + "\n  puts \"NO ERROR\"\nrescue => e\n  puts e.message\nend"
			got := strings.TrimSpace(eval(t, src))
			if !strings.Contains(got, tc.want) {
				t.Errorf("%s raised %q, want something containing %q", tc.expr, got, tc.want)
			}
		})
	}
}

// TestNumericsArityIsAlwaysChecked is the same census Special and Stats carry:
// no method of either module may convert a Go index-out-of-range into a Ruby
// error. See #820 for the defect this guards against.
func TestNumericsArityIsAlwaysChecked(t *testing.T) {
	src := `bad = []
n = 0
[[Integrate, Integrate.methods(false)], [Optimize, Optimize.methods(false)]].each do |recv, ms|
  ms.each do |m|
    n += 1
    begin
      recv.send(m) { |*a| 1.0 }
    rescue => e
      bad << m if e.message.include?("runtime error")
    end
  end
end
puts n
puts bad.join(" ")`
	out := strings.SplitN(strings.TrimSpace(eval(t, src)), "\n", 2)
	// 10 Integrate (tplquad is not bound: see the pull request) + 11 Optimize.
	if out[0] != "21" {
		t.Fatalf("examined %s methods, expected 21 (10 + 11) -- the census did not walk", out[0])
	}
	if len(out) > 1 && strings.TrimSpace(out[1]) != "" {
		t.Errorf("these methods leak a Go runtime fault when called with no arguments: %s", out[1])
	}
}

// TestNumericsKeywords exercises every keyword both modules accept, in the
// "given" direction. Each option is read through one small helper, and a
// keyword nothing passes leaves that helper's present-arm unrun -- which the
// per-function coverage gate reports, and which also means nobody has checked
// that the keyword reaches the module at all.
//
// So each row is a keyword with an OBSERVABLE effect: it must change the
// answer, or refuse the call, not merely be accepted.
func TestNumericsKeywords(t *testing.T) {
	t.Run("quad/limit_and_points", func(t *testing.T) {
		// points: names a kink. 1 - |2x-1| over [0,1] has one at x = 1/2 and
		// integrates to 1/2 either way, so the check is that naming it does not
		// change the answer while exercising the keyword.
		src := `v, _ = Integrate.quad(0.0, 1.0, points: [0.5], limit: 60) { |x| 1.0 - (2.0 * x - 1.0).abs }
p (v - 0.5).abs < 1e-12`
		if got := eval(t, src); got != "true\n" {
			t.Errorf("quad with points:/limit: gave %s", got)
		}
	})

	t.Run("romberg/divmax_does_not_converge", func(t *testing.T) {
		// divmax: 1 stops Romberg after one extrapolation, which on a function it
		// cannot resolve that fast is ErrDivMax -- a RuntimeError here, as
		// SciPy's AccuracyWarning is under -W error. This is the one route to
		// the non-convergence arm, which is why the keyword is tested with it.
		src := `begin
  Integrate.romberg(0.0, 1.0, divmax: 1) { |x| Math.sin(1.0 / (x + 1e-3)) }
  puts "NO ERROR"
rescue RuntimeError => e
  puts e.message.include?("DivMax")
end`
		if got := eval(t, src); got != "true\n" {
			t.Errorf("romberg with divmax: 1 gave %s, want a RuntimeError naming DivMax", got)
		}
	})

	t.Run("dblquad/callable_limits", func(t *testing.T) {
		// The inner limits may be numbers or callables. Over the triangle
		// 0 <= y <= x <= 1 the integral of 1 is 1/2, which a constant limit
		// would give as 1 -- so this distinguishes the two arms.
		src := `v, _ = Integrate.dblquad(0.0, 1.0, 0.0, ->(x) { x }) { |y, x| 1.0 }
p (v - 0.5).abs < 1e-12
w, _ = Integrate.dblquad(0.0, 1.0, 0.0, 1.0) { |y, x| 1.0 }
p (w - 1.0).abs < 1e-12`
		if got := eval(t, src); got != "true\ntrue\n" {
			t.Errorf("dblquad with a callable limit gave %s", got)
		}
	})

	t.Run("optimize/full_output", func(t *testing.T) {
		src := `h = Optimize.brentq(0.0, 2.0, full_output: true, xtol: 1e-10, rtol: 1e-12, maxiter: 80) { |x| x * x - 2 }
p h.keys.sort
p h[:converged], h[:method], (h[:root] - Math.sqrt(2)).abs < 1e-9, h[:iterations] > 0
g = Optimize.fminbound(0.0, 10.0, full_output: true, xtol: 1e-8, maxfun: 200) { |x| (x - 3.0) ** 2 }
p g[:success], (g[:x] - 3.0).abs < 1e-6
c = Optimize.curve_fit([0.0, 1.0, 2.0], [1.0, 3.0, 5.0], [0.0, 0.0], full_output: true) { |x, p| p[0] * x + p[1] }
p c.keys.sort, c[:cov_ok], c[:nfev] > 0`
		const want = "[:converged, :flag, :function_calls, :iterations, :method, :root]\n" +
			"true\n\"brentq\"\ntrue\ntrue\n" +
			"true\ntrue\n" +
			"[:cov_ok, :fvec, :message, :nfev, :pcov, :popt, :status]\ntrue\ntrue\n"
		if got := eval(t, src); got != want {
			t.Errorf("got\n%s\nwant\n%s", got, want)
		}
	})

	t.Run("newton/fprime2_and_x1", func(t *testing.T) {
		// fprime with fprime2 is Halley's method and x1 is the secant's second
		// point; both must still answer sqrt(2).
		src := `a = Optimize.newton(1.0, fprime: ->(x) { 2 * x }, fprime2: ->(x) { 2.0 }, tol: 1e-14, rtol: 1e-15, maxiter: 60) { |x| x * x - 2 }
b = Optimize.newton(1.0, x1: 1.5) { |x| x * x - 2 }
p (a - Math.sqrt(2)).abs < 1e-12, (b - Math.sqrt(2)).abs < 1e-12`
		if got := eval(t, src); got != "true\ntrue\n" {
			t.Errorf("newton with fprime2:/x1: gave %s", got)
		}
	})

	t.Run("minimize_scalar/bracket_and_method_symbol", func(t *testing.T) {
		// bracket: three points, and the method given as a Symbol rather than a
		// String -- both are what a Rubyist writes.
		src := `r = Optimize.minimize_scalar(bracket: [0.0, 2.0, 5.0], method: :brent, tol: 1e-12, xtol: 1e-10, maxiter: 200) { |x| (x - 3.0) ** 2 }
p (r[:x] - 3.0).abs < 1e-6
s = Optimize.minimize_scalar(bounds: [0.0, 10.0], method: :bounded, xatol: 1e-9) { |x| (x - 3.0) ** 2 }
p (s[:x] - 3.0).abs < 1e-6`
		if got := eval(t, src); got != "true\ntrue\n" {
			t.Errorf("minimize_scalar with bracket:/Symbol method gave %s", got)
		}
	})

	t.Run("minimize/bounds_and_counters", func(t *testing.T) {
		// bounds: must actually bind: the free minimum is at 5, so a box ending
		// at 1 has to answer 1.
		src := `r = Optimize.minimize([0.0], method: "L-BFGS-B", bounds: [[0.0, 1.0]], tol: 1e-12, maxiter: 500, maxfev: 500, gtol: 1e-9) { |v| (v[0] - 5.0) ** 2 }
p (r[:x][0] - 1.0).abs < 1e-6, r[:nfev] > 0
u = Optimize.minimize([0.0]) { |v| (v[0] - 5.0) ** 2 }
p (u[:x][0] - 5.0).abs < 1e-6`
		if got := eval(t, src); got != "true\ntrue\ntrue\n" {
			t.Errorf("minimize with bounds: gave %s", got)
		}
	})

	t.Run("approx_fprime/epsilon", func(t *testing.T) {
		src := `g = Optimize.approx_fprime([1.0], epsilon: 1e-6) { |v| 3.0 * v[0] }
p (g[0] - 3.0).abs < 1e-5`
		if got := eval(t, src); got != "true\n" {
			t.Errorf("approx_fprime with epsilon: gave %s", got)
		}
	})

	t.Run("blocks_of_two_arguments_are_type_checked", func(t *testing.T) {
		// dblquad's integrand takes two arguments, so its own conversion arm is
		// separate from the one-argument one.
		src := `begin
  Integrate.dblquad(0.0, 1.0, 0.0, 1.0) { |y, x| "no" }
  puts "NO ERROR"
rescue TypeError => e
  puts e.message
end
begin
  Optimize.curve_fit([0.0, 1.0], [0.0, 1.0], [1.0]) { |x, p| nil }
  puts "NO ERROR"
rescue TypeError => e
  puts e.message
end`
		const want = "the integrand must answer a Float, got String\nthe model must answer a Float, got nil\n"
		if got := eval(t, src); got != want {
			t.Errorf("got\n%q\nwant\n%q", got, want)
		}
	})
}

// TestOptimizeNewtonFullOutputAndBadBound covers the two arms the keyword test
// above leaves: #newton's full_output, which is a separate call site from the
// bracketing finders', and a malformed entry in minimize's bounds: list, which
// is checked per bound rather than for the list as a whole.
func TestOptimizeNewtonFullOutputAndBadBound(t *testing.T) {
	src := `h = Optimize.newton(1.0, full_output: true) { |x| x * x - 2 }
p h[:converged], (h[:root] - Math.sqrt(2)).abs < 1e-12, h[:method].is_a?(String)
begin
  Optimize.minimize([0.0, 0.0], bounds: [[0.0, 1.0], [0.0]]) { |v| v[0] + v[1] }
  puts "NO ERROR"
rescue ArgumentError => e
  puts e.message
end`
	const want = "true\ntrue\ntrue\nbound 1 must be a pair [lo, hi], got 1 element(s)\n"
	if got := eval(t, src); got != want {
		t.Errorf("got\n%q\nwant\n%q", got, want)
	}
}

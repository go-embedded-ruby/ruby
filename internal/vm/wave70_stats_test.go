// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"math"
	"strings"
	"testing"
)

// TestStatsClosedForms gives every one of the 32 distributions at least one
// value that is a mathematical fact rather than whatever the Go module answers
// -- comparing a binding against the code it calls proves only that the call
// happened. Covering all 32 matters because each constructor is written out
// separately, in scipy's argument order, and a swapped or dropped parameter is
// exactly what a table of known moments catches.
func TestStatsClosedForms(t *testing.T) {
	for _, tc := range []struct {
		expr string
		want float64
		why  string
	}{
		// --- continuous, loc+scale
		{"Stats.norm.pdf(0.0)", 1 / math.Sqrt(2*math.Pi), "the normal density at its mode"},
		{"Stats.norm.cdf(0.0)", 0.5, "symmetric about 0"},
		{"Stats.norm.ppf(0.975)", 1.9599639845400538, "the tabulated 1.96"},
		{"Stats.norm.entropy", 0.5 * math.Log(2*math.Pi*math.E), "(1/2) log(2 pi e)"},
		{"Stats.norm.var", 1, "standard normal"},
		{"Stats.norm.kurtosis", 0, "Fisher's excess kurtosis is 0 for a normal"},
		{"Stats.cauchy.pdf(0.0)", 1 / math.Pi, "1/pi at the mode"},
		{"Stats.cauchy.median", 0, "the median exists where the mean does not"},
		{"Stats.expon.mean", 1, "Exp(1)"},
		{"Stats.expon.median", math.Ln2, "log 2 / rate"},
		{"Stats.expon.cdf(1.0)", 1 - 1/math.E, "1 - e^-x"},
		{"Stats.gumbel_r.mean", 0.5772156649015329, "euler_gamma"},
		{"Stats.halfnorm.mean", math.Sqrt(2 / math.Pi), "sqrt(2/pi)"},
		{"Stats.laplace.pdf(0.0)", 0.5, "1/(2b) at the mode"},
		{"Stats.laplace.var", 2, "2 b^2"},
		{"Stats.logistic.pdf(0.0)", 0.25, "1/4 at the mode"},
		{"Stats.logistic.var", math.Pi * math.Pi / 3, "pi^2 s^2 / 3"},
		{"Stats.rayleigh.mean", math.Sqrt(math.Pi / 2), "sigma sqrt(pi/2)"},
		{"Stats.uniform.mean", 0.5, "U(0,1)"},
		{"Stats.uniform.var", 1.0 / 12, "1/12"},
		{"Stats.uniform.entropy", 0, "log of a unit width"},

		// --- continuous, one shape parameter
		{"Stats.chi(1.0).mean", math.Sqrt(2 / math.Pi), "|N(0,1)| has mean sqrt(2/pi)"},
		{"Stats.chi2(2.0).mean", 2, "the mean of chi2 is its df"},
		{"Stats.chi2(2.0).var", 4, "2 df"},
		{"Stats.chi2(2.0).pdf(0.0)", 0.5, "chi2 with 2 df is Exp(1/2)"},
		{"Stats.gamma(1.0).mean", 1, "Gamma(1) is Exp(1)"},
		{"Stats.gamma(2.0).var", 2, "a scale^2"},
		{"Stats.invgamma(3.0).mean", 0.5, "1/(a-1)"},
		{"Stats.lognorm(1.0).median", 1, "e^mu with mu = 0"},
		{"Stats.lomax(3.0).mean", 0.5, "1/(c-1)"},
		{"Stats.pareto(3.0).mean", 1.5, "b/(b-1)"},
		{"Stats.t(3.0).var", 3, "df/(df-2)"},
		{"Stats.t(10.0).mean", 0, "symmetric"},
		{"Stats.triang(0.5).mean", 0.5, "symmetric on (0,1)"},
		{"Stats.vonmises(2.0).mean", 0, "centred at loc"},
		{"Stats.weibull_min(1.0).mean", 1, "c = 1 is Exp(1)"},

		// --- continuous, two shape parameters
		{"Stats.beta(1.0, 1.0).pdf(0.5)", 1, "Beta(1,1) is U(0,1)"},
		{"Stats.beta(2.0, 2.0).mean", 0.5, "a/(a+b)"},
		{"Stats.f(5.0, 10.0).mean", 1.25, "dfd/(dfd-2)"},
		{"Stats.ncx2(2.0, 3.0).mean", 5, "df + nc"},
		{"Stats.reciprocal(1.0, Math::E).pdf(1.0)", 1, "1/(x log(b/a)) at x=1, log e = 1"},

		// --- discrete
		{"Stats.bernoulli(0.3).mean", 0.3, "p"},
		{"Stats.bernoulli(0.3).var", 0.21, "p(1-p)"},
		{"Stats.binom(10, 0.5).pmf(5)", 252.0 / 1024, "C(10,5)/2^10"},
		{"Stats.binom(10, 0.5).mean", 5, "n p"},
		{"Stats.binom(10, 0.5).var", 2.5, "n p (1-p)"},
		{"Stats.geom(0.5).pmf(1)", 0.5, "the first trial succeeds"},
		{"Stats.geom(0.5).mean", 2, "1/p"},
		{"Stats.hypergeom(10, 5, 3).mean", 1.5, "N n / M"},
		{"Stats.nbinom(5.0, 0.5).mean", 5, "n(1-p)/p"},
		{"Stats.poisson(3.0).pmf(2)", 4.5 * math.Exp(-3), "mu^k e^-mu / k!"},
		{"Stats.poisson(3.0).mean", 3, "mu"},
		{"Stats.poisson(3.0).var", 3, "the Poisson variance is its mean"},
		{"Stats.randint(0, 10).mean", 4.5, "(low + high - 1)/2"},
		{"Stats.randint(0, 10).var", 8.25, "(n^2-1)/12 with n = 10"},
		{"Stats.zipf(2.0).pmf(1)", 6 / (math.Pi * math.Pi), "1/zeta(2)"},
	} {
		t.Run(tc.expr, func(t *testing.T) {
			got := evalFloat(t, tc.expr)
			if math.Abs(got-tc.want) > 1e-12*math.Max(1, math.Abs(tc.want)) {
				t.Errorf("%s = %v, want %v (%s)", tc.expr, got, tc.want, tc.why)
			}
		})
	}
}

// TestStatsCoversEveryDistribution makes the table above answerable: it asserts
// that Stats defines exactly the 32 univariate names the Go module has
// constructors for, under scipy's names -- so a distribution added to the module
// and forgotten here, or a name misspelt, fails rather than going unnoticed.
//
// The four remaining constructors are multivariate or non-parametric (dirichlet,
// multinomial, multivariate_normal, gaussian_kde); they do not implement
// Continuous or Discrete and are deliberately not bound here.
func TestStatsCoversEveryDistribution(t *testing.T) {
	want := []string{
		"bernoulli", "beta", "binom", "cauchy", "chi", "chi2", "expon", "f",
		"gamma", "geom", "gumbel_r", "halfnorm", "hypergeom", "invgamma",
		"laplace", "logistic", "lognorm", "lomax", "nbinom", "ncx2", "norm",
		"pareto", "poisson", "randint", "rayleigh", "reciprocal", "t",
		"triang", "uniform", "vonmises", "weibull_min", "zipf",
	}
	got := eval(t, `puts Stats.methods(false).map(&:to_s).sort.join(" ")`)
	gotNames := strings.Fields(strings.TrimSpace(got))
	if len(gotNames) != len(want) {
		t.Fatalf("Stats defines %d distributions, want %d:\ngot  %v\nwant %v",
			len(gotNames), len(want), gotNames, want)
	}
	for i, w := range want {
		if gotNames[i] != w {
			t.Errorf("distribution %d: got %q want %q", i, gotNames[i], w)
		}
	}
}

// TestStatsMethodSet pins the method set of each class: the Continuous and
// Discrete interfaces differ only in the density, so the two Ruby classes must
// differ only in #pdf/#logpdf against #pmf/#logpmf and share the other
// seventeen. Both are checked as a SET, so a method lost from the shared
// definition fails here.
func TestStatsMethodSet(t *testing.T) {
	// Sorted, as Array#sort leaves them; the density sits where its name falls.
	for _, tc := range []struct{ expr, want string }{
		{"Stats.norm", "cdf entropy interval isf kurtosis logcdf logpdf logsf mean median moment pdf ppf rvs sf skew std support var"},
		{"Stats.poisson(1.0)", "cdf entropy interval isf kurtosis logcdf logpmf logsf mean median moment pmf ppf rvs sf skew std support var"},
	} {
		t.Run(tc.expr, func(t *testing.T) {
			src := `p ` + tc.expr + `.class.instance_methods(false).map(&:to_s).sort.join(" ")`
			got := strings.Trim(strings.TrimSpace(eval(t, src)), `"`)
			if got != tc.want {
				t.Errorf("%s methods\ngot  %s\nwant %s", tc.expr, got, tc.want)
			}
		})
	}
}

// TestStatsLocScale checks that loc and scale are read in scipy's positions and
// do what scipy says: loc shifts, scale stretches and divides the density. A
// swapped pair, or a scale applied as a shift, breaks all three rows.
func TestStatsLocScale(t *testing.T) {
	for _, tc := range []struct {
		expr string
		want float64
		why  string
	}{
		{"Stats.norm(5.0, 2.0).mean", 5, "loc is the mean"},
		{"Stats.norm(5.0, 2.0).std", 2, "scale is the standard deviation"},
		{"Stats.norm(5.0, 2.0).pdf(5.0)", 1 / (2 * math.Sqrt(2*math.Pi)), "the density is divided by scale"},
		{"Stats.norm(5.0, 2.0).cdf(5.0)", 0.5, "the median moved with loc"},
		{"Stats.expon(3.0, 2.0).support[0]", 3, "loc moves the support"},
		{"Stats.uniform(2.0, 4.0).mean", 4, "U(loc, loc+scale)"},
		{"Stats.poisson(3.0, 10.0).mean", 13, "a discrete loc shifts the mean"},
	} {
		t.Run(tc.expr, func(t *testing.T) {
			got := evalFloat(t, tc.expr)
			if math.Abs(got-tc.want) > 1e-12*math.Max(1, math.Abs(tc.want)) {
				t.Errorf("%s = %v, want %v (%s)", tc.expr, got, tc.want, tc.why)
			}
		})
	}
}

// TestStatsIdentities checks the pairs of methods against each other rather
// than against a table: sf = 1-cdf, isf = ppf(1-q), the logs are the logs, and
// ppf inverts cdf. Each identity holds for any distribution, so a method wired
// to the wrong Go function breaks one without breaking the others.
func TestStatsIdentities(t *testing.T) {
	src := `ok = []
[Stats.norm, Stats.gamma(2.0), Stats.t(5.0), Stats.beta(2.0, 3.0)].each do |d|
  x = 0.7
  ok << ((d.sf(x) - (1 - d.cdf(x))).abs < 1e-12)
  ok << ((d.logcdf(x) - Math.log(d.cdf(x))).abs < 1e-12)
  ok << ((d.logsf(x) - Math.log(d.sf(x))).abs < 1e-12)
  ok << ((d.logpdf(x) - Math.log(d.pdf(x))).abs < 1e-12)
  ok << ((d.ppf(d.cdf(x)) - x).abs < 1e-9)
  ok << ((d.isf(0.3) - d.ppf(0.7)).abs < 1e-9)
  ok << ((d.std ** 2 - d.var).abs < 1e-12)
  ok << ((d.median - d.ppf(0.5)).abs < 1e-9)
  lo, hi = d.interval(0.95)
  ok << ((d.cdf(hi) - d.cdf(lo) - 0.95).abs < 1e-9)
end
[Stats.poisson(4.0), Stats.binom(10, 0.3)].each do |d|
  ok << ((d.sf(3.0) - (1 - d.cdf(3.0))).abs < 1e-12)
  ok << ((d.logpmf(3.0) - Math.log(d.pmf(3.0))).abs < 1e-12)
  ok << ((d.std ** 2 - d.var).abs < 1e-12)
end
puts ok.count(false)
puts ok.size`
	out := strings.Fields(strings.TrimSpace(eval(t, src)))
	if len(out) != 2 || out[0] != "0" {
		t.Errorf("identities: %v failed out of %v", out[0], out[1])
	}
	// 4 continuous x 9 identities + 2 discrete x 3.
	if out[1] != "42" {
		t.Errorf("checked %s identities, expected 42 -- the loop stopped early", out[1])
	}
}

// TestStatsIsAUfunc is the test for the dispatch this binding adds: every
// per-point method takes a Float or an NDArray and answers the same numbers,
// through the module's own stats.Map.
func TestStatsIsAUfunc(t *testing.T) {
	src := `d = Stats.norm
a = NDArray.from([-1.0, 0.0, 1.0], [3])
ok = []
%w[pdf logpdf cdf logcdf sf logsf].each do |m|
  r = d.send(m, a)
  ok << (r.is_a?(NDArray) && r.shape == [3])
  ok << ([-1.0, 0.0, 1.0].each_with_index.all? { |x, i| (r[i] - d.send(m, x)).abs < 1e-15 })
end
q = NDArray.from([0.25, 0.5, 0.75], [3])
%w[ppf isf].each do |m|
  r = d.send(m, q)
  ok << (r.is_a?(NDArray) && r.shape == [3])
  ok << ([0.25, 0.5, 0.75].each_with_index.all? { |x, i| (r[i] - d.send(m, x)).abs < 1e-15 })
end
k = NDArray.from([0.0, 1.0, 2.0], [3])
pm = Stats.poisson(2.0).pmf(k)
ok << (pm.is_a?(NDArray) && pm.shape == [3])
ok << ([0.0, 1.0, 2.0].each_with_index.all? { |x, i| (pm[i] - Stats.poisson(2.0).pmf(x)).abs < 1e-15 })
puts ok.count(false)
puts ok.size`
	out := strings.Fields(strings.TrimSpace(eval(t, src)))
	if len(out) != 2 || out[0] != "0" {
		t.Errorf("ufunc dispatch: %v of %v checks failed", out[0], out[1])
	}
	if out[1] != "18" {
		t.Errorf("made %s checks, expected 18", out[1])
	}
}

// TestStatsShapePreserved: the element-wise path answers the shape it was given,
// 2-D included, which is what makes a distribution usable with the rest of the
// NDArray surface.
func TestStatsShapePreserved(t *testing.T) {
	src := `a = NDArray.from([0.0, 1.0, 2.0, 3.0], [2, 2])
p Stats.norm.cdf(a).shape
p Stats.norm.cdf(a).to_a[0]
p Stats.norm.cdf(a)[0].to_a`
	// [0] of a 2-D array is its first row, as NumPy basic indexing gives.
	const want = "[2, 2]\n0.5\n[0.5, 0.8413447460685429]\n"
	if got := eval(t, src); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// TestStatsRvs: #rvs with no size answers a Float as scipy's does and with a
// size an NDArray of that shape; a seed: makes the draw reproducible, no seed:
// advances one stream so successive calls are independent.
func TestStatsRvs(t *testing.T) {
	src := `d = Stats.norm
p d.rvs.class
p d.rvs(3).class, d.rvs(3).shape
p d.rvs(2, 2).shape
p d.rvs(3, seed: 42).to_a == d.rvs(3, seed: 42).to_a
p d.rvs(3, seed: 42).to_a == d.rvs(3, seed: 43).to_a
p d.rvs(3).to_a == d.rvs(3).to_a`
	const want = "Float\nNDArray\n[3]\n[2, 2]\ntrue\nfalse\nfalse\n"
	if got := eval(t, src); got != want {
		t.Errorf("got\n%q\nwant\n%q", got, want)
	}
}

// TestStatsRvsIsDistributed is the control that #rvs draws from the
// distribution asked for and not from some default: 20000 seeded draws from
// norm(10, 2) must have a sample mean and standard deviation near 10 and 2.
// The tolerance is a 6-sigma standard error, so it is a check and not a flake:
// se(mean) = 2/sqrt(20000) = 0.014.
func TestStatsRvsIsDistributed(t *testing.T) {
	src := `x = Stats.norm(10.0, 2.0).rvs(20000, seed: 7).to_a
n = x.size
m = x.sum / n
sd = Math.sqrt(x.map { |v| (v - m) ** 2 }.sum / (n - 1))
puts n
puts ((m - 10.0).abs < 0.09)
puts ((sd - 2.0).abs < 0.07)`
	got := strings.Fields(strings.TrimSpace(eval(t, src)))
	if len(got) != 3 || got[0] != "20000" {
		t.Fatalf("drew %v, want 20000 observations", got)
	}
	if got[1] != "true" || got[2] != "true" {
		t.Errorf("sample mean within 6 se: %s, sample sd within tolerance: %s -- rvs is not drawing from norm(10, 2)", got[1], got[2])
	}
}

// TestStatsParameterErrors: a parameter outside its domain is SciPy's
// ValueError, which is an ArgumentError here, carrying the module's own
// message. The point is that a bad parameter is refused at CONSTRUCTION rather
// than answering NaN later.
func TestStatsParameterErrors(t *testing.T) {
	for _, tc := range []struct{ expr, want string }{
		{"Stats.norm(0.0, -1.0)", "parameter out of domain"},
		{"Stats.norm(0.0, 0.0)", "parameter out of domain"},
		{"Stats.gamma(-1.0)", "parameter out of domain"},
		{"Stats.bernoulli(1.5)", "parameter out of domain"},
		{"Stats.binom(-1, 0.5)", "parameter out of domain"},
		{"Stats.beta(0.0, 1.0)", "parameter out of domain"},
		{"Stats.t(0.0)", "parameter out of domain"},
	} {
		t.Run(tc.expr, func(t *testing.T) {
			src := "begin\n  " + tc.expr + "\n  puts \"NO ERROR\"\nrescue ArgumentError => e\n  puts e.message\nend"
			got := strings.TrimSpace(eval(t, src))
			if !strings.Contains(got, tc.want) {
				t.Errorf("%s: got %q, want an ArgumentError containing %q", tc.expr, got, tc.want)
			}
		})
	}
}

// TestStatsArgumentErrors: each constructor checks its own count, in scipy's
// shape-then-loc-then-scale order, and each method checks its own.
func TestStatsArgumentErrors(t *testing.T) {
	for _, tc := range []struct{ expr, want string }{
		{"Stats.norm(1.0, 2.0, 3.0)", "wrong number of arguments (given 3, expected 0..2)"},
		{"Stats.gamma", "wrong number of arguments (given 0, expected 1..3)"},
		{"Stats.beta(1.0)", "wrong number of arguments (given 1, expected 2..4)"},
		{"Stats.hypergeom(10, 5)", "wrong number of arguments (given 2, expected 3..4)"},
		{"Stats.norm.pdf", "wrong number of arguments (given 0, expected 1)"},
		{"Stats.norm.mean(1.0)", "wrong number of arguments (given 1, expected 0)"},
		{"Stats.norm.support(1.0)", "wrong number of arguments (given 1, expected 0)"},
		{"Stats.norm.rvs(3, bogus: 1)", "unknown keyword: :bogus"},
		{"Stats.norm.pdf(nil)", "can't convert nil into Float"},
		{"Stats.binom(1.5, 0.5)", "no implicit conversion of Float into Integer"},
		{"Stats.norm.rvs(-1)", "negative dimension -1"},
	} {
		t.Run(tc.expr, func(t *testing.T) {
			src := "begin\n  " + tc.expr + "\n  puts \"NO ERROR\"\nrescue => e\n  puts e.message\nend"
			if got := strings.TrimSpace(eval(t, src)); got != tc.want {
				t.Errorf("%s raised %q, want %q", tc.expr, got, tc.want)
			}
		})
	}
}

// TestStatsInspect names the distribution a value came from, so an object in a
// console or a backtrace says which one it is.
func TestStatsInspect(t *testing.T) {
	src := `p Stats.norm
p Stats.poisson(1.0)
p Stats.weibull_min(2.0)
p Stats.norm.class, Stats.poisson(1.0).class`
	const want = "#<Stats::Continuous norm>\n#<Stats::Discrete poisson>\n#<Stats::Continuous weibull_min>\nStats::Continuous\nStats::Discrete\n"
	if got := eval(t, src); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// TestStatsArityIsAlwaysChecked is the same census for Stats: no constructor and
// no method may convert a Go index-out-of-range into a Ruby error. See
// TestSpecialArityIsAlwaysChecked for why.
func TestStatsArityIsAlwaysChecked(t *testing.T) {
	src := `bad = []
n = 0
[[Stats, Stats.methods(false)],
 [Stats.norm, Stats::Continuous.instance_methods(false)],
 [Stats.poisson(1.0), Stats::Discrete.instance_methods(false)]].each do |recv, ms|
  ms.each do |m|
    n += 1
    begin
      recv.send(m)
    rescue => e
      bad << m if e.message.include?("runtime error")
    end
  end
end
puts n
puts bad.join(" ")`
	out := strings.SplitN(strings.TrimSpace(eval(t, src)), "\n", 2)
	if out[0] != "70" {
		t.Fatalf("examined %s methods, expected 70 (32 distributions + 19 + 19) -- the census did not walk", out[0])
	}
	if len(out) > 1 && strings.TrimSpace(out[1]) != "" {
		t.Errorf("these Stats methods leak a Go runtime fault when called with no arguments: %s", out[1])
	}
}

// TestStatsTruthyAndRvsLimit covers the two things nothing else reaches.
//
// A distribution is an object, so it is truthy -- object.Value requires
// Truthy() of both wrapper types and only a boolean context calls it. And #rvs
// checks its own sizes, but go-ndarray refuses a shape whose product is too
// large BEFORE allocating anything, which is the one error #rvs can answer
// that is not a negative dimension. Both were recorded as untested by the
// per-function coverage gate, which is what asked for this test.
func TestStatsTruthyAndRvsLimit(t *testing.T) {
	src := `p [(!Stats.norm), (Stats.norm ? 1 : 0), (!Stats.poisson(1.0)), (Stats.poisson(1.0) ? 1 : 0)]
begin
  Stats.norm.rvs(2 ** 40, 2 ** 40)
  puts "NO ERROR"
rescue ArgumentError => e
  puts e.message.include?("too big")
end`
	const want = "[false, 1, false, 1]\ntrue\n"
	if got := eval(t, src); got != want {
		t.Errorf("got\n%q\nwant\n%q", got, want)
	}
}

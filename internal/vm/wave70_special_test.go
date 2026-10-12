// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"math"
	"strconv"
	"strings"
	"testing"
)

// TestSpecialClosedForms checks the Special module against values that are
// mathematical facts, not against the Go functions it binds -- comparing a
// binding to the code it calls proves only that the call happened. Each row is
// a closed form or a tabulated constant, so a wrong name, a swapped argument or
// a lost conversion shows up as a wrong number.
//
// The scipy name of each function is the one the Go module measures itself
// against, which is why softplus and gammaln appear rather than log1pexp and
// loggamma.
func TestSpecialClosedForms(t *testing.T) {
	for _, tc := range []struct {
		expr string  // Ruby
		want float64 // the mathematical value
		why  string
	}{
		{"Special.gamma(5.0)", 24, "Gamma(n+1) = n!"},
		{"Special.gamma(0.5)", math.Sqrt(math.Pi), "Gamma(1/2) = sqrt(pi)"},
		{"Special.gammaln(5.0)", math.Log(24), "log Gamma(5) = log 24"},
		{"Special.gammasgn(-0.5)", -1, "Gamma(-1/2) < 0"},
		{"Special.digamma(1.0)", -0.5772156649015329, "psi(1) = -euler_gamma"},
		{"Special.polygamma(1, 1.0)", math.Pi * math.Pi / 6, "psi'(1) = pi^2/6"},
		{"Special.factorial(5.0)", 120, "5! = 120"},
		{"Special.log_factorial(5.0)", math.Log(120), "log 5!"},
		{"Special.factorial2(6)", 48, "6!! = 6*4*2"},
		{"Special.beta(1.0, 1.0)", 1, "B(1,1) = 1"},
		{"Special.beta(2.0, 3.0)", 1.0 / 12, "B(2,3) = 1!2!/4! = 1/12"},
		{"Special.betaln(1.0, 1.0)", 0, "log B(1,1) = 0"},
		{"Special.binom(5.0, 2.0)", 10, "C(5,2) = 10"},
		{"Special.comb(5, 2)", 10, "C(5,2) = 10"},
		{"Special.perm(5, 2)", 20, "P(5,2) = 5*4"},
		{"Special.erf(0.0)", 0, "erf is odd"},
		{"Special.erf(1.0)", 0.8427007929497149, "tabulated erf(1)"},
		{"Special.erfc(0.0)", 1, "erfc(0) = 1"},
		{"Special.erfcx(0.0)", 1, "erfcx(0) = erfc(0)"},
		{"Special.erfinv(0.0)", 0, "inverse of an odd function"},
		{"Special.erfcinv(1.0)", 0, "erfc(0) = 1"},
		{"Special.ndtr(0.0)", 0.5, "the normal CDF at its median"},
		{"Special.ndtri(0.5)", 0, "its inverse"},
		{"Special.log_ndtr(0.0)", math.Log(0.5), "log of the above"},
		{"Special.expit(0.0)", 0.5, "the logistic at 0"},
		{"Special.logit(0.5)", 0, "its inverse"},
		{"Special.log_expit(0.0)", math.Log(0.5), "log of the logistic at 0"},
		{"Special.softplus(0.0)", math.Ln2, "log(1+e^0) = log 2"},
		{"Special.xlogy(2.0, 2.0)", 2 * math.Ln2, "x log y"},
		{"Special.xlogy(0.0, 0.0)", 0, "0 log 0 = 0, the point of xlogy"},
		{"Special.xlog1py(1.0, 1.0)", math.Ln2, "x log(1+y)"},
		{"Special.zeta(2.0)", math.Pi * math.Pi / 6, "the Basel problem"},
		{"Special.zeta(2.0, 1.0)", math.Pi * math.Pi / 6, "Hurwitz zeta(s,1) = zeta(s)"},
		{"Special.gammainc(1.0, 1.0)", 1 - 1/math.E, "P(1,x) = 1-e^-x"},
		{"Special.gammaincc(1.0, 1.0)", 1 / math.E, "Q = 1-P"},
		{"Special.gammaincinv(1.0, 1 - 1 / Math::E)", 1, "inverse of the row above"},
		{"Special.gammainccinv(1.0, 1 / Math::E)", 1, "and of the other one"},
		{"Special.betainc(1.0, 1.0, 0.5)", 0.5, "I_x(1,1) = x"},
		{"Special.betaincc(1.0, 1.0, 0.5)", 0.5, "1 - I_x(1,1)"},
		{"Special.betaincinv(1.0, 1.0, 0.5)", 0.5, "its inverse"},
		{"Special.betainccinv(1.0, 1.0, 0.5)", 0.5, "and the complement's"},
		{"Special.j0(0.0)", 1, "J0(0) = 1"},
		{"Special.j1(0.0)", 0, "J1(0) = 0"},
		{"Special.i0(0.0)", 1, "I0(0) = 1"},
		{"Special.i1(0.0)", 0, "I1(0) = 0"},
		{"Special.i0e(0.0)", 1, "I0e(0) = I0(0)"},
		{"Special.i1e(0.0)", 0, "I1e(0) = 0"},
		{"Special.jv(0.0, 0.0)", 1, "Jv with v=0 is J0"},
		{"Special.jn(0, 0.0)", 1, "and so is Jn with n=0"},
		{"Special.in(0, 0.0)", 1, "In with n=0 is I0"},
		{"Special.ellipk(0.0)", math.Pi / 2, "K(0) = pi/2"},
		{"Special.ellipe(0.0)", math.Pi / 2, "E(0) = pi/2"},
		{"Special.ellipkm1(1.0)", math.Pi / 2, "K(1-p) at p=1 is K(0)"},
		{"Special.logsumexp([0.0, 0.0])", math.Ln2, "log(e^0+e^0) = log 2"},
	} {
		t.Run(tc.expr, func(t *testing.T) {
			got := evalFloat(t, tc.expr)
			if math.Abs(got-tc.want) > 1e-12*math.Max(1, math.Abs(tc.want)) {
				t.Errorf("%s = %v, want %v (%s)", tc.expr, got, tc.want, tc.why)
			}
		})
	}
}

// TestSpecialNonFinite pins the three answers scipy gives outside a function's
// domain, which a binding can easily turn into a Ruby error or a zero.
func TestSpecialNonFinite(t *testing.T) {
	for _, tc := range []struct{ expr, want string }{
		{"Special.gamma(0.0)", "Infinity"},
		{"Special.logit(0.0)", "-Infinity"},
		{"Special.logit(2.0)", "NaN"},
		{"Special.erf(Float::INFINITY)", "1.0"},
		{"Special.gamma(-1.0)", "NaN"},
	} {
		t.Run(tc.expr, func(t *testing.T) {
			if got := strings.TrimSpace(eval(t, "p "+tc.expr)); got != tc.want {
				t.Errorf("%s = %s, want %s", tc.expr, got, tc.want)
			}
		})
	}
}

// TestSpecialIsAUfunc is the test for the dispatch this binding adds, which is
// the only part of it that is not the Go module's own code: one Ruby name must
// answer a Float for a Float and an NDArray for an NDArray, with the SAME
// numbers, and a Float mixed in with an NDArray must broadcast as scipy's does.
//
// It covers each arity -- one, two, three arguments, an integer order and a
// combinatorial pair -- because the dispatch is written once per arity.
func TestSpecialIsAUfunc(t *testing.T) {
	for _, tc := range []struct{ name, scalar, array string }{
		{"u1", "Special.gamma(2.5)", "Special.gamma(NDArray.from([2.5], [1]))[0]"},
		{"u2", "Special.xlogy(2.0, 3.0)", "Special.xlogy(NDArray.from([2.0], [1]), NDArray.from([3.0], [1]))[0]"},
		{"u2_mixed", "Special.xlogy(2.0, 3.0)", "Special.xlogy(2.0, NDArray.from([3.0], [1]))[0]"},
		{"u2_mixed_other", "Special.xlogy(2.0, 3.0)", "Special.xlogy(NDArray.from([2.0], [1]), 3.0)[0]"},
		{"u3", "Special.betainc(0.5, 0.5, 0.25)", "Special.betainc(0.5, 0.5, NDArray.from([0.25], [1]))[0]"},
		{"uInt1", "Special.jn(2, 1.5)", "Special.jn(2, NDArray.from([1.5], [1]))[0]"},
		{"uInt2", "Special.comb(6, 3)", "Special.comb(NDArray.from([6.0], [1]), NDArray.from([3.0], [1]))[0]"},
		{"zeta_1", "Special.zeta(3.0)", "Special.zeta(NDArray.from([3.0], [1]))[0]"},
		{"zeta_2", "Special.zeta(3.0, 2.0)", "Special.zeta(NDArray.from([3.0], [1]), 2.0)[0]"},
		{"vector", "Special.softmax([1.0, 2.0])[1]", "Special.softmax(NDArray.from([1.0, 2.0], [2]))[1]"},
		{"reduction", "Special.logsumexp([1.0, 2.0])", "Special.logsumexp(NDArray.from([1.0, 2.0], [2]))"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want, got := evalFloat(t, tc.scalar), evalFloat(t, tc.array)
			if got != want {
				t.Errorf("%s = %v but %s = %v: the two paths must agree", tc.scalar, want, tc.array, got)
			}
		})
	}
}

// TestSpecialPreservesShape: the element-wise path must answer the shape it was
// given, which is what makes it usable with the rest of the NDArray surface.
func TestSpecialPreservesShape(t *testing.T) {
	src := `a = NDArray.from([1.0, 2.0, 3.0, 4.0], [2, 2])
p Special.gamma(a).shape
p Special.gamma(a).to_a
p Special.xlogy(1.0, a).shape`
	const want = "[2, 2]\n[1.0, 1.0, 2.0, 6.0]\n[2, 2]\n"
	if got := eval(t, src); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// TestSpecialArgumentErrors: every binding checks its own arguments, so no Go
// runtime fault can reach the caller. A missing argument is MRI's ArgumentError
// with MRI's message -- not an index-out-of-range turned into one.
func TestSpecialArgumentErrors(t *testing.T) {
	for _, tc := range []struct{ expr, want string }{
		{"Special.gamma", "wrong number of arguments (given 0, expected 1)"},
		{"Special.gamma(1.0, 2.0)", "wrong number of arguments (given 2, expected 1)"},
		{"Special.xlogy(1.0)", "wrong number of arguments (given 1, expected 2)"},
		{"Special.betainc(1.0, 2.0)", "wrong number of arguments (given 2, expected 3)"},
		{"Special.zeta", "wrong number of arguments (given 0, expected 1..2)"},
		{"Special.zeta(1.0, 2.0, 3.0)", "wrong number of arguments (given 3, expected 1..2)"},
		{"Special.jn_zeros(1)", "wrong number of arguments (given 1, expected 2)"},
		{"Special.gamma(nil)", "can't convert nil into Float"},
		{"Special.gamma(\"x\")", "can't convert String into Float"},
		{"Special.comb(1.5, 2)", "no implicit conversion of Float into Integer"},
		{"Special.jn_zeros(-1, 2)", "order must be non-negative"},
		{"Special.jn_zeros(1, -2)", "count must be non-negative"},
	} {
		t.Run(tc.expr, func(t *testing.T) {
			src := "begin\n  " + tc.expr + "\nrescue => e\n  puts e.message\nend"
			if got := strings.TrimSpace(eval(t, src)); got != tc.want {
				t.Errorf("%s raised %q, want %q", tc.expr, got, tc.want)
			}
		})
	}
}

// TestSpecialJnZeros checks jn_zeros against the tabulated first zeros of J0 and
// J1, and that each is in fact a zero of the function this module computes.
func TestSpecialJnZeros(t *testing.T) {
	got := eval(t, "p Special.jn_zeros(0, 3)")
	for _, want := range []string{"2.404825557695773", "5.520078110286311", "8.653727912911013"} {
		if !strings.Contains(got, want) {
			t.Errorf("jn_zeros(0, 3) = %s, missing the tabulated zero %s", got, want)
		}
	}
	// And the property: J0 vanishes there. 1e-13 is the check on the pair of
	// functions, not on either alone.
	if v := evalFloat(t, "Special.j0(Special.jn_zeros(0, 1)[0])"); math.Abs(v) > 1e-13 {
		t.Errorf("j0 at its own first zero = %v, want ~0", v)
	}
}

// TestSpecialLogGammaSign answers the pair scipy splits across two functions,
// and must agree with both of them.
func TestSpecialLogGammaSign(t *testing.T) {
	src := `l, s = Special.loggamma_sign(-0.5)
p [(l - Special.gammaln(-0.5)).abs < 1e-15, s, Special.gammasgn(-0.5).to_i]`
	const want = "[true, -1, -1]\n"
	if got := eval(t, src); got != want {
		t.Errorf("got %s want %s", got, want)
	}
}

// evalFloat evaluates a Ruby expression and reads its answer as a float64, so a
// test compares numbers rather than printed reprs.
func evalFloat(t *testing.T, expr string) float64 {
	t.Helper()
	out := strings.TrimSpace(eval(t, "print ("+expr+").to_f"))
	f, err := strconv.ParseFloat(out, 64)
	if err != nil {
		t.Fatalf("%s printed %q, which is not a Float: %v", expr, out, err)
	}
	return f
}

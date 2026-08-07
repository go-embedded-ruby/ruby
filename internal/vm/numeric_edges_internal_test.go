// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"math"
	"math/big"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// catchRaise (returning the raised RubyError class) is shared with the other
// internal tests in this package.

// TestRoundHalfFuncs exercises every branch of the three float half-rounding
// functions ported from CRuby numeric.c, asserting exact results.
func TestRoundHalfFuncs(t *testing.T) {
	cases := []struct {
		name string
		fn   func(float64, float64) float64
		x, s float64
		want float64
	}{
		{"up_s1", roundHalfUp, 2.5, 1.0, 3},              // s == 1 fast path (ties away)
		{"up_s1_neg", roundHalfUp, -2.5, 1.0, -3},        // s == 1, negative
		{"up_pos_corr", roundHalfUp, 0.575, 100, 58},     // x>0, f+=1 correction taken
		{"up_pos_nocorr", roundHalfUp, 2.44, 100, 244},   // x>0 no correction
		{"up_neg_corr", roundHalfUp, -0.575, 100, -58},   // x<0, f-=1 correction taken
		{"up_neg_nocorr", roundHalfUp, -2.44, 100, -244}, // x<0 no correction
		{"down_pos_corr", roundHalfDown, 2.5, 1, 2},      // 2.5*1=2.5 round=3? corr down x>0
		{"down_pos_nocorr", roundHalfDown, 2.6, 10, 26},  // x>0 no correction
		{"down_neg_corr", roundHalfDown, -2.5, 1, -2},    // x<0 correction toward zero
		{"down_neg_nocorr", roundHalfDown, -2.6, 10, -26},
		{"even_pos_even", roundHalfEven, 2.5, 1, 2}, // tie -> even (down)
		{"even_pos_odd", roundHalfEven, 3.5, 1, 4},  // tie -> even (up)
		{"even_pos_gt", roundHalfEven, 2.8, 1, 3},   // d>0.5
		{"even_pos_lt", roundHalfEven, 2.2, 1, 2},   // d<0.5 (else d=0)
		{"even_neg_even", roundHalfEven, -2.5, 1, -2},
		{"even_neg_odd", roundHalfEven, -3.5, 1, -4},
		{"even_neg_gt", roundHalfEven, -2.8, 1, -3},
		{"even_neg_lt", roundHalfEven, -2.2, 1, -2},
	}
	for _, c := range cases {
		if got := c.fn(c.x, c.s); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
	// roundHalfEven with x == 0 returns 0 (the x==0 fall-through).
	if got := roundHalfEven(0, 10); got != 0 {
		t.Errorf("roundHalfEven(0,10)=%v want 0", got)
	}
	// applyRoundMode dispatch, incl. the default (modeHalfUp) arm.
	if got := applyRoundMode(modeHalfDown, 2.5, 1); got != 2 {
		t.Errorf("applyRoundMode down=%v", got)
	}
	if got := applyRoundMode(modeHalfEven, 2.5, 1); got != 2 {
		t.Errorf("applyRoundMode even=%v", got)
	}
	if got := applyRoundMode(modeHalfUp, 2.5, 1); got != 3 {
		t.Errorf("applyRoundMode up=%v", got)
	}
}

// TestFloatRoundOverflowUnderflow covers both binexp sign branches and both
// truth values of the precision-limit predicates.
func TestFloatRoundOverflowUnderflow(t *testing.T) {
	if !floatRoundOverflow(20, 4) { // binexp>0, over the limit
		t.Error("overflow(20,4) should be true")
	}
	if floatRoundOverflow(2, 4) { // binexp>0, within
		t.Error("overflow(2,4) should be false")
	}
	if !floatRoundOverflow(30, -6) { // binexp<=0 branch, over
		t.Error("overflow(30,-6) should be true")
	}
	if floatRoundOverflow(2, -6) { // binexp<=0 branch, within
		t.Error("overflow(2,-6) should be false")
	}
	if !floatRoundUnderflow(-10, 6) { // binexp>0 branch, under
		t.Error("underflow(-10,6) should be true")
	}
	if floatRoundUnderflow(0, 6) { // binexp>0 branch, not under
		t.Error("underflow(0,6) should be false")
	}
	if !floatRoundUnderflow(-10, -6) { // binexp<=0 branch, under
		t.Error("underflow(-10,-6) should be true")
	}
	if floatRoundUnderflow(5, -6) { // binexp<=0 branch, not under
		t.Error("underflow(5,-6) should be false")
	}
}

// TestDbl2ival covers both the int64-range and the Bignum path.
func TestDbl2ival(t *testing.T) {
	if v := dbl2ival(30); v != object.IntValue(30) {
		t.Errorf("dbl2ival(30)=%v", v)
	}
	big1e19 := dbl2ival(1e19) // beyond int64
	b, ok := object.BigOf(big1e19)
	if !ok || b.Cmp(pow10Big(19)) != 0 {
		t.Errorf("dbl2ival(1e19)=%v", big1e19)
	}
}

// TestFloRoundPaths drives floRound through each structural branch.
func TestFloRoundPaths(t *testing.T) {
	cases := []struct {
		number  float64
		ndigits int
		mode    roundMode
		want    string // inspect-like
	}{
		{0.0, 2, modeHalfUp, "0.0"},       // zero, ndigits>0 -> Float
		{0.0, 0, modeHalfUp, "0"},         // zero, ndigits<=0 -> Integer 0
		{25.7, -1, modeHalfUp, "30"},      // ndigits<0 -> integer round
		{2.5, 0, modeHalfEven, "2"},       // ndigits==0 -> Integer, banker
		{3.14159, 3, modeHalfUp, "3.142"}, // finite normal
		{3.0, 16, modeHalfUp, "3.0"},      // ndigits>=DBL_DIG -> rational path (Float)
	}
	for _, c := range cases {
		got := floRound(c.number, c.ndigits, c.mode).Inspect()
		if got != c.want {
			t.Errorf("floRound(%v,%d)=%s want %s", c.number, c.ndigits, got, c.want)
		}
	}
	// overflow: ndigits huge for a small binexp -> value unchanged (Float).
	if got := floRound(4.81, 40, modeHalfUp); float64(got.(object.Float)) != 4.81 {
		t.Errorf("floRound overflow=%v", got)
	}
	// underflow: very negative ndigits on a small value -> 0.0.
	if got := floRound(3.14, -50, modeHalfUp); got.Inspect() != "0" {
		// note: ndigits<0 path routes through intRound, which returns 0 here.
		t.Errorf("floRound underflow-ish=%v", got.Inspect())
	}
	// underflow branch of the finite path proper (ndigits>0 but negative-exp value).
	tiny := math.Ldexp(1, -400) // ~1e-121, binexp = -399
	if got := floRound(tiny, 1, modeHalfUp); float64(got.(object.Float)) != 0 {
		t.Errorf("floRound tiny underflow=%v", got)
	}
	// NaN / Infinity pass through unchanged.
	if got := floRound(math.NaN(), 2, modeHalfUp); !math.IsNaN(float64(got.(object.Float))) {
		t.Errorf("floRound(NaN)=%v", got)
	}
	if got := floRound(math.Inf(1), 2, modeHalfUp); !math.IsInf(float64(got.(object.Float)), 1) {
		t.Errorf("floRound(Inf)=%v", got)
	}
}

// TestIntRoundHelpers covers intRoundZeroP, intRound and intHalfUp branches.
func TestIntRoundHelpers(t *testing.T) {
	// intRoundZeroP: fixnum-range (8 bytes) and Bignum branches, both truths.
	if intRoundZeroP(big.NewInt(5), -1) {
		t.Error("zeroP(5,-1) should be false")
	}
	if !intRoundZeroP(big.NewInt(5), -100) {
		t.Error("zeroP(5,-100) should be true")
	}
	bigNum := new(big.Int).Exp(big.NewInt(10), big.NewInt(80), nil) // large Bignum
	if intRoundZeroP(bigNum, -1) {
		t.Error("zeroP(big,-1) should be false")
	}

	i := func(s string) *big.Int { z, _ := new(big.Int).SetString(s, 10); return z }
	cases := []struct {
		num     string
		ndigits int
		mode    roundMode
		want    string
	}{
		{"12345", 0, modeHalfUp, "12345"},  // ndigits>=0 -> unchanged
		{"0", -2, modeHalfUp, "0"},         // num==0 short-circuit
		{"5", -100, modeHalfUp, "0"},       // zeroP short-circuit
		{"249", -2, modeHalfUp, "200"},     // cmp<0
		{"251", -2, modeHalfUp, "300"},     // cmp>0
		{"250", -2, modeHalfUp, "300"},     // cmp==0, up, positive
		{"-250", -2, modeHalfUp, "-300"},   // cmp==0, up, negative -> not added
		{"250", -2, modeHalfDown, "200"},   // cmp==0, down, positive
		{"-250", -2, modeHalfDown, "-200"}, // cmp==0, down, negative -> toward zero
		{"250", -2, modeHalfEven, "200"},   // cmp==0, even, lower even
		{"350", -2, modeHalfEven, "400"},   // cmp==0, even, lower odd
	}
	for _, c := range cases {
		got := intRound(i(c.num), c.ndigits, c.mode).String()
		if got != c.want {
			t.Errorf("intRound(%s,%d,%d)=%s want %s", c.num, c.ndigits, c.mode, got, c.want)
		}
	}
}

// TestBitHelpers covers rubyShiftRight, bitAt and bitSlice edge branches.
func TestBitHelpers(t *testing.T) {
	i := big.NewInt
	// rubyShiftRight: normal right, huge (sign-extend both signs), left, huge-left raise.
	if rubyShiftRight(i(0b1011), i(2)).Cmp(i(2)) != 0 {
		t.Error("shift right normal")
	}
	if rubyShiftRight(i(1), i(maxShift+1)).Sign() != 0 {
		t.Error("shift right huge positive -> 0")
	}
	if rubyShiftRight(i(-1), i(maxShift+1)).Cmp(i(-1)) != 0 {
		t.Error("shift right huge negative -> -1")
	}
	if rubyShiftRight(i(1), i(-2)).Cmp(i(4)) != 0 {
		t.Error("shift right negative index -> left shift")
	}
	if cls := catchRaise(func() { rubyShiftRight(i(1), i(-(maxShift + 1))) }); cls != "RangeError" {
		t.Errorf("huge left shift class=%q", cls)
	}
	// non-int64 index: build a >64-bit index.
	huge := new(big.Int).Lsh(i(1), 100)
	if rubyShiftRight(i(1), huge).Sign() != 0 {
		t.Error("shift right non-int64 index -> 0")
	}
	if cls := catchRaise(func() { rubyShiftRight(i(1), new(big.Int).Neg(huge)) }); cls != "RangeError" {
		t.Errorf("non-int64 left shift class=%q", cls)
	}

	// bitAt: negative index, huge index (both signs), normal.
	if bitAt(i(0b1011), i(-1)).Sign() != 0 {
		t.Error("bitAt negative -> 0")
	}
	if bitAt(i(5), huge).Sign() != 0 {
		t.Error("bitAt huge positive receiver -> 0")
	}
	if bitAt(i(-2), huge).Cmp(i(1)) != 0 {
		t.Error("bitAt huge negative receiver -> 1")
	}
	if bitAt(i(0b100), i(2)).Cmp(i(1)) != 0 {
		t.Error("bitAt normal")
	}

	// bitSlice: negative length, huge length, normal.
	if bitSlice(i(0b101101), i(2), i(-1)).Cmp(i(0b1011)) != 0 {
		t.Error("bitSlice negative length -> full shifted")
	}
	if bitSlice(i(0b101101), i(2), huge).Cmp(i(0b1011)) != 0 {
		t.Error("bitSlice huge length -> full shifted")
	}
	if bitSlice(i(255), i(0), i(3)).Cmp(i(7)) != 0 {
		t.Error("bitSlice normal")
	}
}

// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package object

import (
	"math"
	"testing"
)

// TestFloatToSMatchesMRI pins Float#to_s against MRI's flo_to_s (numeric.c):
// the shortest round-tripping digits are laid out in fixed notation while the
// decimal point falls within -3..DBL_DIG (15), and in exponential notation
// outside that, where the mantissa always carries a fractional digit.
//
// Every expected value is what `ruby` 4.0.5 prints for the same literal.
func TestFloatToSMatchesMRI(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		// Fixed notation, including the "1.0 not 1" rule.
		{0, "0.0"},
		{math.Copysign(0, -1), "-0.0"},
		{1, "1.0"},
		{3.14, "3.14"},
		{-2.5, "-2.5"},
		{0.1, "0.1"},
		{100, "100.0"},
		{1e6, "1000000.0"}, // Go's 'g' verb gives "1e+06" here
		// The upper boundary: decpt 15 stays fixed, 16 goes exponential.
		{1e14, "100000000000000.0"},
		{123456789012345, "123456789012345.0"},
		{1e15, "1.0e+15"},
		{1234567890123456, "1.234567890123456e+15"},
		// The lower boundary: decpt -3 stays fixed, -4 goes exponential.
		{1e-4, "0.0001"},
		{1e-5, "1.0e-05"},
		// Exponential, with and without a fraction of its own.
		{1e-6, "1.0e-06"},
		{1.5e-6, "1.5e-06"},
		{-1e-6, "-1.0e-06"},
		{1e20, "1.0e+20"},
		{1e100, "1.0e+100"},
		{5e-324, "5.0e-324"},
		{math.MaxFloat64, "1.7976931348623157e+308"},
		// The non-finite values.
		{math.Inf(1), "Infinity"},
		{math.Inf(-1), "-Infinity"},
		{math.NaN(), "NaN"},
	}
	for _, c := range cases {
		if got := Float(c.in).ToS(); got != c.want {
			t.Errorf("Float(%v).ToS() = %q, want %q", c.in, got, c.want)
		}
		if got := Float(c.in).Inspect(); got != c.want {
			t.Errorf("Float(%v).Inspect() = %q, want %q", c.in, got, c.want)
		}
	}
}

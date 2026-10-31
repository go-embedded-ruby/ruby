// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"math"
	"strconv"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

func TestSaturate(t *testing.T) {
	for _, c := range []struct{ x, lo, hi, want int64 }{
		{5, 0, 10, 5},
		{-1, 0, 10, 0},
		{11, 0, 10, 10},
		{math.MaxInt64, math.MinInt32, math.MaxInt32, math.MaxInt32},
		{math.MinInt64, math.MinInt32, math.MaxInt32, math.MinInt32},
		{math.MaxInt64, math.MinInt64, math.MaxInt64, math.MaxInt64},
	} {
		if got := saturate(c.x, c.lo, c.hi); got != c.want {
			t.Errorf("saturate(%d, %d, %d) = %d; want %d", c.x, c.lo, c.hi, got, c.want)
		}
	}
}

func TestClampInt(t *testing.T) {
	// In range: the value itself, on every target, for int64 and named types.
	for _, x := range []int64{0, 1, -1, math.MaxInt32, math.MinInt32} {
		if got := clampInt(x); int64(got) != x {
			t.Errorf("clampInt(%d) = %d", x, got)
		}
		if got := clampInt(object.Integer(x)); int64(got) != x {
			t.Errorf("clampInt(Integer(%d)) = %d", x, got)
		}
	}
	big, small := int64(1)<<40, -(int64(1) << 40)
	if strconv.IntSize == 64 {
		// 64-bit: exactly int(x).
		if clampInt(big) != int(big) || clampInt(small) != int(small) {
			t.Errorf("clampInt is not the identity on a 64-bit target")
		}
		return
	}
	// 32-bit: saturates where int(x) would wrap (int(1<<40) is 0 there).
	if got := clampInt(big); got != math.MaxInt {
		t.Errorf("clampInt(1<<40) = %d; want math.MaxInt", got)
	}
	if got := clampInt(small); got != math.MinInt {
		t.Errorf("clampInt(-1<<40) = %d; want math.MinInt", got)
	}
}

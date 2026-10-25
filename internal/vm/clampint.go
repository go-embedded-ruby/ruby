// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "math"

// clampInt converts a 64-bit integer (an Integer's Fixnum payload, a count, an
// offset) to a Go int.
//
// On 64-bit targets it is exactly int(x): the bounds are math.MinInt64 and
// math.MaxInt64, which no int64 crosses, so the compiler proves both
// comparisons false and keeps only the conversion.
//
// On 32-bit targets (386, arm) int(x) would wrap silently: [1, 2][2**32] would
// read index 0, and Integer("10", 2**32 + 2) would accept radix 2. clampInt
// saturates instead, to math.MaxInt or math.MinInt, so a value outside int's
// range stays outside every slice's bounds and every validity range, and the
// caller's own check answers what it answers on 64-bit (nil, IndexError,
// ArgumentError, ...).
func clampInt[T ~int64](x T) int {
	return int(saturate(int64(x), math.MinInt, math.MaxInt))
}

// saturate limits x to [lo, hi].
func saturate(x, lo, hi int64) int64 {
	if x > hi {
		return hi
	}
	if x < lo {
		return lo
	}
	return x
}

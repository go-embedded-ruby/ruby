package vm

import (
	"math"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// Range#max(n) used to build the whole range to take n elements off its end.
// The cost was O(size of the range) and had nothing to do with n: measured at
// ~24 bytes per element, `(0..10**9).max(2)` asked for about 24 GB to answer
// with two integers, on a CI runner with sixteen.
//
// ⛔ #min(n) and #first(n) were already lazy on the same ranges, which is the
// control that made this one method's defect rather than the range
// representation's.
func TestRangeMaxNIsArithmeticNotMaterialised(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"inclusive", `p((1..10).max(2))`, "[10, 9]"},
		{"exclusive", `p((1...10).max(2))`, "[9, 8]"},
		{"n larger than the range clamps", `p((1..3).max(10))`, "[3, 2, 1]"},
		{"n equal to the size", `p((1..3).max(3))`, "[3, 2, 1]"},
		{"n of zero", `p((1..10).max(0))`, "[]"},
		{"an empty range", `p((5..1).max(2))`, "[]"},
		{"an empty exclusive range", `p((5...5).max(2))`, "[]"},
		{"a single-element range", `p((7..7).max(2))`, "[7]"},
		{"negative endpoints", `p((-5..-1).max(3))`, "[-1, -2, -3]"},
		{"a range spanning zero", `p((-2..2).max(3))`, "[2, 1, 0]"},

		// ⛔ The reason this is cheap at all. Before, each of these built the
		// whole range first; the first two would not have finished.
		{"ten million", `p((0..10_000_000).max(2))`, "[10000000, 9999999]"},
		{"a billion", `p((0..1_000_000_000).max(3))`, "[1000000000, 999999999, 999999998]"},
		{"near the int64 ceiling", `p(((2**62)..(2**62 + 10)).max(2))`,
			"[4611686018427387914, 4611686018427387913]"},

		// ⛔ THE OVERFLOW CASE the helper exists for: `top - lo + 1` is not
		// representable, and a size computed that way comes out NEGATIVE, which
		// would clamp n to a negative count. The span is taken in uint64
		// instead.
		{"a range whose size overflows int64", `p(((-(2**62))..(2**62)).max(2))`,
			"[4611686018427387904, 4611686018427387903]"},

		// ⛔ AN EXCLUSIVE END AT THE int64 FLOOR has no predecessor to step down
		// to, so `top--` would underflow. The range is empty for every possible
		// begin, and the coverage gate is what showed this guard was never
		// driven -- reachable from Ruby, since -2**63 is an Integer and not a
		// Bignum.
		{"an exclusive end at the int64 floor", `p((0...(-2**63)).max(2))`, "[]"},

		// Controls: the paths this must NOT have taken over.
		{"a String range still works", `p(("a".."f").max(2))`, `["f", "e"]`},
		{"min(n) is unchanged", `p((1..10).min(2))`, "[1, 2]"},
		{"max with no argument is unchanged", `p((1..10).max)`, "10"},
		{"max with a block is unchanged", `p((1..10).max { |a, b| b <=> a })`, "1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := runSrc(t, tc.src); got != tc.want {
				t.Errorf("%s\n got %q\nwant %q", tc.src, got, tc.want)
			}
		})
	}
}

// ⛔ A negative count is an ArgumentError, not an empty array. The old path got
// this from clampCount; the arithmetic one has to raise it itself, and nothing
// else in the suite would have noticed if it did not.
func TestRangeMaxNRefusesANegativeCount(t *testing.T) {
	const src = `begin; (1..10).max(-1); rescue ArgumentError => e; p e.class; end`
	if got := runSrc(t, src); got != "ArgumentError" {
		t.Errorf("got %q, want ArgumentError", got)
	}
}

// integerRangeMaxN must agree with materialising the range, on every small
// range in a window that crosses zero, inclusive and exclusive, for every n
// from 0 past the end. An equivalence test rather than a table, because the
// bug being fixed was in the clamping arithmetic and a table only covers the
// cases its author thought of.
func TestIntegerRangeMaxNAgreesWithMaterialising(t *testing.T) {
	for lo := int64(-4); lo <= 4; lo++ {
		for hi := int64(-4); hi <= 4; hi++ {
			for _, excl := range []bool{false, true} {
				for n := int64(0); n <= 10; n++ {
					r := &object.Range{Lo: object.IntValue(lo), Hi: object.IntValue(hi), Exclusive: excl}
					got, ok := integerRangeMaxN(r, n)
					if !ok {
						t.Fatalf("(%d..%d excl=%v) was not recognised as an Integer range", lo, hi, excl)
					}
					want := materialisedMaxN(lo, hi, excl, n)
					gotArr := got.(*object.Array).Elems
					if len(gotArr) != len(want) {
						t.Fatalf("(%d..%d excl=%v).max(%d): got %d elems, want %d",
							lo, hi, excl, n, len(gotArr), len(want))
					}
					for i := range want {
						if int64(gotArr[i].(object.Integer)) != want[i] {
							t.Fatalf("(%d..%d excl=%v).max(%d)[%d] = %v, want %d",
								lo, hi, excl, n, i, gotArr[i], want[i])
						}
					}
				}
			}
		}
	}
}

// materialisedMaxN is the definition the arithmetic has to match: build the
// range, take n off the end, descending.
func materialisedMaxN(lo, hi int64, excl bool, n int64) []int64 {
	var elems []int64
	for v := lo; ; v++ {
		if excl && v >= hi {
			break
		}
		if !excl && v > hi {
			break
		}
		elems = append(elems, v)
		if v == math.MaxInt64 {
			break
		}
	}
	var out []int64
	for i := int64(0); i < n && i < int64(len(elems)); i++ {
		out = append(out, elems[int64(len(elems))-1-i])
	}
	return out
}

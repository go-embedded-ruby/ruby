// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// Ruby 4.0 teaches Range#max to answer for beginless Integer ranges. range.c's
// range_max reaches its arithmetic with `c = NIL_P(b) ? -1 : OPTIMIZED_CMP(b, e)`
// -- a nil begin is simply "below the end" -- and guards the Integer-begin
// demand with `!NIL_P(b) &&`, so a beginless range skips it entirely. The n
// argument goes through reverse_each, which walks
// `(NIL_P(beg) || RB_INTEGER_TYPE_P(beg)) && RB_INTEGER_TYPE_P(end)` downward
// from the end with nothing but n to stop it.
//
// Every expectation below was taken from MRI 4.0.7 on this host, through
// rubydiff, not from reading range.c. The five ruby/spec examples behind
// `ruby_version_is "4.0"` in core/range/max_spec.rb are the first five rows.
func TestRangeMaxOnBeginlessRanges(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		// The five spec examples.
		{"exclusive beginless Integer", `p((...1).max)`, "0"},
		{"n maxima of a beginless range", `p((..1).max(2))`, "[1, 0]"},
		{"n maxima of an exclusive beginless range", `p((...1).max(2))`, "[0, -1]"},
		{"n maxima of a very large Integer range", `p((0...2**64).max(2))`,
			"[18446744073709551615, 18446744073709551614]"},
		{"a beginless non-Integer range cannot be walked",
			`begin; (..1.0).max(2); rescue TypeError => e; p e.message; end`,
			`"can't iterate from NilClass"`},

		// The same refusal for a String end, which is the other half of that
		// example and reaches it by a different route: not Numeric, so not an
		// Integer either.
		{"a beginless String range cannot be walked",
			`begin; (.."f").max(2); rescue TypeError => e; p e.message; end`,
			`"can't iterate from NilClass"`},

		// ⛔ The predecessor of an exclusive end is taken in big.Int, so the
		// int64 floor is not a special case: MRI answers -9223372036854775809,
		// a Bignum, and so must this.
		{"an exclusive beginless end at the int64 floor", `p((...(-2**63)).max)`,
			"-9223372036854775809"},
		{"n maxima below the int64 floor", `p((..(-2**63)).max(2))`,
			"[-9223372036854775808, -9223372036854775809]"},
		{"n maxima below an exclusive int64 floor", `p((...(-2**63)).max(2))`,
			"[-9223372036854775809, -9223372036854775810]"},

		// Bignum endpoints on a bounded range, which the int64 helper used to
		// decline and hand to the materialising path -- where a 2**64-element
		// range is not an answer anyone receives.
		{"a Bignum end on a bounded range", `p((0..2**64).max(2))`,
			"[18446744073709551616, 18446744073709551615]"},
		{"a Bignum end on a beginless range", `p((..(2**64)).max(2))`,
			"[18446744073709551616, 18446744073709551615]"},

		// n of zero is an empty array, not a refusal: MRI builds an array of
		// capacity 0 and reverse_each stops immediately.
		{"zero maxima of a beginless range", `p((..1).max(0))`, "[]"},
		{"zero maxima of an exclusive beginless range", `p((...1).max(0))`, "[]"},

		// A Float n is converted, as NUM2LONG does.
		{"a Float count on a beginless range", `p((..1).max(2.7))`, "[1, 0]"},

		// ⛔ THE ORDER OF THE GUARDS IS OBSERVABLE. range_max asks for a block
		// BEFORE it looks at the n argument, so a beginless range with both
		// raises rather than answering arithmetically. Answering first would
		// return [1, 0] here and look perfectly reasonable.
		{"a block beats the n argument on a beginless range",
			`begin; (..1).max(2) { |a, b| a <=> b }; rescue RangeError => e; p e.message; end`,
			`"cannot get the maximum of beginless range with custom comparison method"`},
		{"a block alone on a beginless range",
			`begin; (..1).max { |a, b| a <=> b }; rescue RangeError => e; p e.message; end`,
			`"cannot get the maximum of beginless range with custom comparison method"`},

		// ⛔ An exclusive range whose end is not Numeric takes the same
		// walk-the-elements route as a block does, so a beginless one raises
		// the SAME RangeError -- not the TypeError about excluding a non
		// Integer end, which is what rbgo used to say here.
		{"an exclusive beginless non-Numeric end",
			`begin; (..."f").max; rescue RangeError => e; p e.message; end`,
			`"cannot get the maximum of beginless range with custom comparison method"`},

		// ⛔ A non-Integer BEGIN with an Integer end declines the arithmetic and
		// falls to the materialising path, which cannot start from a Float.
		// Found by the coverage gate: rangeBigEnds' second refusal was the only
		// uncovered line in this change, and it is reachable from Ruby.
		{"a Float begin with an Integer end",
			`begin; (1.0..3).max(2); rescue TypeError => e; p e.message; end`,
			`"can't iterate from Float"`},

		// Controls: what must NOT have moved.
		{"an endless range still refuses",
			`begin; (1..).max; rescue RangeError => e; p e.message; end`,
			`"cannot get the maximum of endless range"`},
		{"an endless range with n still refuses",
			`begin; (1..).max(2); rescue RangeError => e; p e.message; end`,
			`"cannot get the maximum of endless range"`},
		{"a beginless inclusive range is still its end", `p((..1).max)`, "1"},
		{"a beginless Float range is still its end", `p((..1.0).max)`, "1.0"},
		{"an exclusive Float end still refuses",
			`begin; (...1.0).max; rescue TypeError => e; p e.message; end`,
			`"cannot exclude non Integer end value"`},
		{"an exclusive Rational end still refuses",
			`begin; (...Rational(1)).max; rescue TypeError => e; p e.message; end`,
			`"cannot exclude non Integer end value"`},
		{"a non-Integer begin still refuses an excluded end",
			`begin; (1.0...2).max; rescue TypeError => e; p e.message; end`,
			`"cannot exclude end value with non Integer begin value"`},
		{"a beginless minimum is still unobtainable",
			`begin; (..1).min; rescue RangeError => e; p e.message; end`,
			`"cannot get the minimum of beginless range"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := runSrc(t, tc.src); got != tc.want {
				t.Errorf("%s\n got %q\nwant %q", tc.src, got, tc.want)
			}
		})
	}
}

// ⛔ A beginless range has no lower endpoint to clamp n against, so n alone
// decides the allocation: `(..1).max(2**40)` is a request for 8 TiB of slice,
// from one short line of untrusted Ruby. MRI bounds it inside rb_ary_new2,
// which refuses a length above ARY_MAX_SIZE with ArgumentError; rbgo refuses
// with the same class so the answer is an exception the program can rescue and
// not a process the kernel kills.
//
// The control matters as much as the refusal: a cap that also refused ordinary
// counts would pass this test while breaking every real use.
func TestRangeMaxNOnABeginlessRangeRefusesAnUnboundedAllocation(t *testing.T) {
	const bomb = `begin; (..1).max(2**40); rescue ArgumentError => e; p e.class; end`
	if got := runSrc(t, bomb); got != "ArgumentError" {
		t.Errorf("(..1).max(2**40) = %q, want ArgumentError", got)
	}
	const ok = `p((..1).max(1000).length)`
	if got := runSrc(t, ok); got != "1000" {
		t.Errorf("(..1).max(1000).length = %q, want 1000 -- the cap refuses an ordinary count", got)
	}
}

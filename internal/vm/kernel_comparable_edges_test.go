package vm_test

import (
	"strings"
	"testing"
)

// TestComparableClampAndOrdering exercises Comparable#clamp (two-argument and
// Range forms, beginless/endless, and every error branch) plus the ordering
// operators' MRI-faithful "comparison of X with Y failed" ArgumentError, all
// against the semantics of ruby 4.x.
func TestComparableClampAndOrdering(t *testing.T) {
	valueTests := []struct {
		name, src, want string
	}{
		// clamp, two-argument form.
		{"clamp_in_range", `p 5.clamp(1, 10)`, "5\n"},
		{"clamp_below_min", `p 5.clamp(8, 10)`, "8\n"},
		{"clamp_above_max", `p 5.clamp(1, 3)`, "3\n"},
		{"clamp_at_min", `p 5.clamp(5, 10)`, "5\n"},
		{"clamp_at_max", `p 5.clamp(1, 5)`, "5\n"},
		{"clamp_equal_bounds", `p 5.clamp(3, 3)`, "3\n"},
		{"clamp_nil_min", `p 5.clamp(nil, 3)`, "3\n"},
		{"clamp_nil_max", `p 5.clamp(8, nil)`, "8\n"},
		{"clamp_min_nilmax_pass", `p 5.clamp(1, nil)`, "5\n"},
		{"clamp_both_nil", `p 5.clamp(nil, nil)`, "5\n"},
		// clamp, Range form (inclusive, beginless, endless).
		{"clamp_range_in", `p 5.clamp(1..10)`, "5\n"},
		{"clamp_range_below", `p 5.clamp(8..10)`, "8\n"},
		{"clamp_range_above", `p 5.clamp(1..3)`, "3\n"},
		{"clamp_range_beginless", `p 5.clamp(..3)`, "3\n"},
		{"clamp_range_beginless_in", `p 5.clamp(..10)`, "5\n"},
		{"clamp_range_endless", `p 5.clamp(10..)`, "10\n"},
		{"clamp_range_endless_in", `p 5.clamp(1..)`, "5\n"},
		{"clamp_range_endless_neg", `p(-5.clamp(0..))`, "0\n"},
		// == is lenient: an incomparable pair is unequal, not an error.
		{"cmp_eq_nil_false", `class F; include Comparable; def <=>(o); nil; end; end
p(F.new == F.new)`, "false\n"},
		// between?
		{"between_true", `p 5.between?(1, 10)`, "true\n"},
		{"between_false_low", `p 5.between?(6, 10)`, "false\n"},
		{"between_false_high", `p 5.between?(1, 3)`, "false\n"},
		{"between_at_bound", `p 5.between?(5, 5)`, "true\n"},
	}
	for _, tc := range valueTests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}

	errTests := []struct {
		name, src, wantClass, wantMsg string
	}{
		{"clamp_min_gt_max", `5.clamp(10, 1)`, "ArgumentError", "min argument must be less than or equal to max argument"},
		{"clamp_range_inverted", `5.clamp(10..1)`, "ArgumentError", "min argument must be less than or equal to max argument"},
		{"clamp_range_exclusive", `5.clamp(1...10)`, "ArgumentError", "cannot clamp with an exclusive range"},
		{"clamp_range_exclusive_inverted", `5.clamp(10...1)`, "ArgumentError", "cannot clamp with an exclusive range"},
		{"clamp_one_nonrange", `5.clamp(1)`, "TypeError", "wrong argument type Integer (expected Range)"},
		{"clamp_three_args", `5.clamp(1, 2, 3)`, "ArgumentError", "wrong number of arguments (given 3, expected 1..2)"},
		{"clamp_zero_args", `5.clamp()`, "ArgumentError", "wrong number of arguments (given 0, expected 1..2)"},
		// A nil <=> makes clamp's bound comparison fail; the min (an immediate) is
		// rendered by #inspect, matching MRI's rb_cmperr.
		{"clamp_incomparable", `class F; include Comparable; def <=>(o); nil; end; end
F.new.clamp(1, 2)`, "ArgumentError", "comparison of F with 1 failed"},
		// Ordering operators raise when <=> returns nil; a non-immediate right
		// operand is rendered by class name.
		{"lt_incomparable", `class F; include Comparable; def <=>(o); nil; end; end
F.new < F.new`, "ArgumentError", "comparison of F with F failed"},
		{"le_incomparable", `class F; include Comparable; def <=>(o); nil; end; end
F.new <= F.new`, "ArgumentError", "comparison of F with F failed"},
		{"gt_incomparable", `class F; include Comparable; def <=>(o); nil; end; end
F.new > F.new`, "ArgumentError", "comparison of F with F failed"},
		{"ge_incomparable", `class F; include Comparable; def <=>(o); nil; end; end
F.new >= F.new`, "ArgumentError", "comparison of F with F failed"},
		// A Symbol right operand is an immediate too: rendered by #inspect.
		{"lt_incomparable_symbol", `class F; include Comparable; def <=>(o); nil; end; end
F.new < :sym`, "ArgumentError", "comparison of F with :sym failed"},
	}
	for _, tc := range errTests {
		t.Run(tc.name, func(t *testing.T) {
			assertRaise(t, tc.src, tc.wantClass, tc.wantMsg)
		})
	}
}

// TestKernelIntegerFloat covers Kernel#Integer and Kernel#Float: radix prefixes
// (including Ruby's 0d), underscores, surrounding whitespace, explicit bases,
// non-finite Float coercion, and the exception: keyword that turns any failure
// into a nil result instead of a raise — all matched to ruby 4.x.
func TestKernelIntegerFloat(t *testing.T) {
	valueTests := []struct {
		name, src, want string
	}{
		{"int_plain", `p Integer("42")`, "42\n"},
		{"int_signed", `p Integer("-42")`, "-42\n"},
		{"int_whitespace", `p Integer("  42  ")`, "42\n"},
		{"int_underscore", `p Integer("1_000")`, "1000\n"},
		{"int_hex", `p Integer("0x10")`, "16\n"},
		{"int_hex_base", `p Integer("0x10", 16)`, "16\n"},
		{"int_hex_underscore", `p Integer("0x1_0")`, "16\n"},
		{"int_bin", `p Integer("0b101")`, "5\n"},
		{"int_oct", `p Integer("0o17")`, "15\n"},
		{"int_dec_prefix", `p Integer("0d99")`, "99\n"},
		{"int_dec_prefix_base10", `p Integer("0d99", 10)`, "99\n"},
		{"int_base2", `p Integer("101", 2)`, "5\n"},
		{"int_from_float", `p Integer(3.9)`, "3\n"},
		{"int_from_int", `p Integer(7)`, "7\n"},
		{"int_ex_false_bad", `p Integer("bad", exception: false)`, "nil\n"},
		{"int_ex_false_nil", `p Integer(nil, exception: false)`, "nil\n"},
		{"int_ex_false_inf", `p Integer(1.0/0, exception: false)`, "nil\n"},
		{"int_ex_true_ok", `p Integer("42", exception: true)`, "42\n"},
		{"int_empty_kwargs", `p Integer("42", **{})`, "42\n"},
		{"int_kwargs_from_hash", `h = {exception: false}
p Integer("bad", **h)`, "nil\n"},
		{"float_plain", `p Float("1.5")`, "1.5\n"},
		{"float_whitespace", `p Float("  1.5e3 ")`, "1500.0\n"},
		{"float_from_int", `p Float(3)`, "3.0\n"},
		{"float_from_float", `p Float(2.5)`, "2.5\n"},
		{"float_ex_false_bad", `p Float("bad", exception: false)`, "nil\n"},
		{"float_ex_false_nil", `p Float(nil, exception: false)`, "nil\n"},
	}
	for _, tc := range valueTests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}

	errTests := []struct {
		name, src, wantClass, wantMsg string
	}{
		{"int_bad_value", `Integer("bad")`, "ArgumentError", `invalid value for Integer(): "bad"`},
		{"int_bad_radix", `Integer("10", 40)`, "ArgumentError", "invalid radix 40"},
		{"int_low_radix", `Integer("10", 1)`, "ArgumentError", "invalid radix 1"},
		{"int_nil_type", `Integer(nil)`, "TypeError", "can't convert nil into Integer"},
		{"int_inf", `Integer(1.0/0)`, "FloatDomainError", "Infinity"},
		{"int_neg_inf", `Integer(-1.0/0)`, "FloatDomainError", "-Infinity"},
		{"int_nan", `Integer(0.0/0)`, "FloatDomainError", "NaN"},
		{"float_bad_value", `Float("bad")`, "ArgumentError", `invalid value for Float(): "bad"`},
		{"float_nil_type", `Float(nil)`, "TypeError", "can't convert nil into Float"},
		{"int_unknown_kw", `Integer("42", foo: 1)`, "ArgumentError", "unknown keyword: :foo"},
		{"int_unknown_kws", `Integer("42", foo: 1, bar: 2)`, "ArgumentError", "unknown keywords: :foo, :bar"},
	}
	for _, tc := range errTests {
		t.Run(tc.name, func(t *testing.T) {
			assertRaise(t, tc.src, tc.wantClass, tc.wantMsg)
		})
	}
}

// assertRaise runs src and asserts it raised an error whose text names both the
// expected exception class and message fragment.
func assertRaise(t *testing.T, src, wantClass, wantMsg string) {
	t.Helper()
	err := runErr(t, src)
	if err == nil {
		t.Fatalf("src=%q: expected %s (%q), got no error", src, wantClass, wantMsg)
	}
	if !strings.Contains(err.Error(), wantClass) {
		t.Errorf("src=%q: got err=%v, want class %q", src, err, wantClass)
	}
	if !strings.Contains(err.Error(), wantMsg) {
		t.Errorf("src=%q: got err=%v, want message %q", src, err, wantMsg)
	}
}

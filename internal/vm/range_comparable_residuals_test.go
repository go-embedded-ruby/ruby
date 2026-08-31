// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// Every expected value below was verified byte-for-byte against MRI Ruby 4.0.6
// (ruby -e '...') for the ruby/spec core/range and core/comparable residuals:
// value-based Range#== / #eql? / #hash, the (nil..nil) #inspect rule, Range's
// private #initialize (arity / frozen / bad-value), Range#count over unbounded
// ranges, the MRI-4.0 non-Integer String#step (#+ walk), and Comparable#==
// under an rb_cmpint-style non-nil, non-zero #<=> result.

// TestRangeEqualEqlHash covers Range#==, #eql? and #hash, which now compare by
// endpoint value (dispatching #== / #eql?) rather than object identity.
func TestRangeEqualEqlHash(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"eq_same", `p((0..2) == (0..2))`, "true\n"},
		{"eq_int_float", `p((0..1) == (0..1.0))`, "true\n"},
		{"eq_excl_differs", `p((0..1) == (0...1))`, "false\n"},
		{"eq_not_range", `p((1..10) == 1)`, "false\n"},
		{"eq_endless", `p((1..) == (1..))`, "true\n"},
		{"eq_endless_excl", `p((1..) == (1...))`, "false\n"},
		{"eq_beginless", `p((...10) == (...10))`, "true\n"},
		{"eql_true", `p((0..2).eql?(0..2))`, "true\n"},
		{"eql_int_float", `p((0..1).eql?(0..1.0))`, "false\n"},
		{"eql_not_range", `p((1..10).eql?('a'))`, "false\n"},
		// A Range subclass with equal endpoints is == and eql? to a plain Range.
		{"eq_subclass", `
class MyR < Range; end
p(Range.new(1, 2) == MyR.new(1, 2))`, "true\n"},
		// Custom endpoints compare by their own #== (here: same @v). The ruby/spec
		// custom-endpoint case invokes it as a message send (a.send(:==, b)), which
		// dispatches Range#== rather than the primitive == fast path.
		{"eq_custom_endpoints", `
class Pt
  include Comparable
  def initialize(v); @v = v; end
  attr_reader :v
  def <=>(o); @v <=> o.v; end
  def ==(o); o.is_a?(Pt) && @v == o.v; end
end
a = Pt.new(1)..Pt.new(3)
b = Range.new(Pt.new(1), Pt.new(3))
p(a.send(:==, b))`, "true\n"},
		// Equal ranges hash alike; an inclusive range differs from its exclusive twin.
		{"hash_equal", `p((0..1).hash == (0..1).hash)`, "true\n"},
		{"hash_excl_differs", `p((0..10).hash == (0...10).hash)`, "false\n"},
		{"hash_is_integer", `p((0..1).hash.is_a?(Integer))`, "true\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestRangeInspectToS covers the (nil..nil) #inspect special case (both bounds
// render as "nil") while #to_s and the one-sided unbounded forms stay bare.
func TestRangeInspectToS(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"inspect_nilnil", `p((..nil).inspect)`, "\"nil..nil\"\n"},
		{"inspect_endless", `p((1..).inspect)`, "\"1..\"\n"},
		{"inspect_beginless", `p((..1).inspect)`, "\"..1\"\n"},
		{"inspect_beginless_excl", `p((...1).inspect)`, "\"...1\"\n"},
		{"inspect_bounded", `p((1..5).inspect)`, "\"1..5\"\n"},
		{"inspect_str", `p(('A'..'Z').inspect)`, "\"\\\"A\\\"..\\\"Z\\\"\"\n"},
		{"to_s_nilnil", `p((nil..nil).to_s)`, "\"..\"\n"},
		{"to_s_endless", `p((1..).to_s)`, "\"1..\"\n"},
		{"to_s_bounded", `p((1..5).to_s)`, "\"1..5\"\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestRangeInitialize covers the private Range#initialize: it fills in a freshly
// allocated Range, is listed as a private instance method, enforces arity 2..3
// and comparable endpoints, and refuses to reinitialise a frozen (constructed)
// Range.
func TestRangeInitialize(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"is_private", `p(Range.private_instance_methods(false).include?(:initialize))`, "true\n"},
		{"allocate_init_two", `
r = Range.allocate
r.send(:initialize, 0, 2)
p(r.to_a)`, "[0, 1, 2]\n"},
		{"allocate_init_three", `
r = Range.allocate
r.send(:initialize, 0, 2, true)
p(r.to_a)`, "[0, 1]\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
	errCases := []struct{ name, src, class string }{
		{"init_too_few", `Range.allocate.send(:initialize, 1)`, "ArgumentError"},
		{"init_too_many", `Range.allocate.send(:initialize, 1, 2, 3, 4)`, "ArgumentError"},
		{"init_bad_value", `Range.allocate.send(:initialize, Object.new, Object.new)`, "ArgumentError"},
		{"init_frozen", `(0..1).send(:initialize, 1, 3)`, "FrozenError"},
		{"init_frozen_three", `(0..1).send(:initialize, 1, 3, true)`, "FrozenError"},
	}
	for _, tc := range errCases {
		t.Run(tc.name, func(t *testing.T) {
			err := runErr(t, tc.src)
			if err == nil || !strings.Contains(err.Error(), tc.class) {
				t.Errorf("src=%q: got err=%v, want %s", tc.src, err, tc.class)
			}
		})
	}
}

// TestRangeCountAndEntries covers Range#count over unbounded ranges (Infinity)
// and the true-alias Range#entries.
func TestRangeCountAndEntries(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"count_endless_int", `p((7..).count)`, "Infinity\n"},
		{"count_endless_str", `p(('a'...).count)`, "Infinity\n"},
		{"count_beginless_str", `p((...'a').count)`, "Infinity\n"},
		{"count_beginless_float", `p((..10.0).count)`, "Infinity\n"},
		{"count_nilnil", `p((nil..nil).count)`, "Infinity\n"},
		{"count_bounded", `p((1..4).count)`, "4\n"},
		{"count_with_block", `p((1..4).count(&:even?))`, "2\n"},
		{"entries_alias", `p(Range.instance_method(:entries) == Range.instance_method(:to_a))`, "true\n"},
		{"entries_value", `p((1..3).entries)`, "[1, 2, 3]\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestRangeStepStringPlus covers MRI 4.0's non-Integer String#step, which
// advances by #+ (not #succ) and raises TypeError for an incompatible step.
func TestRangeStepStringPlus(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"step_plus_inclusive", `
a = []
("A".."AAA").step("A") { |x| a << x }
p(a)`, "[\"A\", \"AA\", \"AAA\"]\n"},
		{"step_plus_exclusive", `
a = []
("A"..."AAA").step("A") { |x| a << x }
p(a)`, "[\"A\", \"AA\"]\n"},
		{"step_plus_endless", `
a = []
("A"..).step("A") { |x| break if x > "AAA"; a << x }
p(a)`, "[\"A\", \"AA\", \"AAA\"]\n"},
		// The existing Integer-step (via #succ) path still works.
		{"step_int_succ", `
a = []
("A".."G").step(2) { |x| a << x }
p(a)`, "[\"A\", \"C\", \"E\", \"G\"]\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
	errCases := []struct{ name, src, class string }{
		{"step_float", `("A".."G").step(2.0) { }`, "TypeError"},
		{"step_array", `("A".."G").step([]) { }`, "TypeError"},
		// A beginless String range cannot be materialised: MRI names the class.
		{"to_a_beginless", `(..'a').to_a`, "NilClass"},
	}
	for _, tc := range errCases {
		t.Run(tc.name, func(t *testing.T) {
			err := runErr(t, tc.src)
			if err == nil || !strings.Contains(err.Error(), tc.class) {
				t.Errorf("src=%q: got err=%v, want %s", tc.src, err, tc.class)
			}
		})
	}
}

// TestComparableEqualCmpint covers Comparable#== under the rb_cmpint rule: nil is
// unequal, a zero (including 0.0) result is equal, and a non-nil, non-comparison
// result (a String) raises ArgumentError.
func TestComparableEqualCmpint(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"eq_zero", `
class C0
  include Comparable
  def <=>(o); 0; end
end
p(C0.new == C0.new)`, "true\n"},
		{"eq_zero_float", `
class Cf
  include Comparable
  def <=>(o); 0.0; end
end
p(Cf.new == Cf.new)`, "true\n"},
		{"eq_nonzero", `
class Cn
  include Comparable
  def <=>(o); 1; end
end
p(Cn.new == Cn.new)`, "false\n"},
		{"eq_nil", `
class Cnil
  include Comparable
  def <=>(o); nil; end
end
p(Cnil.new == Cnil.new)`, "false\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
	t.Run("eq_bad_cmp_raises", func(t *testing.T) {
		src := `
class Cbad
  include Comparable
  def <=>(o); "abc"; end
end
Cbad.new == Cbad.new`
		err := runErr(t, src)
		if err == nil || !strings.Contains(err.Error(), "ArgumentError") {
			t.Errorf("got err=%v, want ArgumentError", err)
		}
	})
}

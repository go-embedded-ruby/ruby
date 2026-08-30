// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// Every expected value below was checked byte-for-byte against MRI Ruby
// (ruby -e '...') for the ruby/spec core/range cluster: #cover?, #include?,
// #member?, #===, #step, #each, #first, #min, #max, #to_a.

// TestRangeCoverIncludeCase covers the comparison-based membership methods, which
// now dispatch #<=> through the VM so Numeric subclasses, Time and custom
// Comparable objects order by their own comparison.
func TestRangeCoverIncludeCase(t *testing.T) {
	cases := []struct{ name, src, want string }{
		// cover? uses pure #<=> on both endpoints.
		{"cover_int_in", `p((1..5).cover?(2))`, "true\n"},
		{"cover_int_out_low", `p((0..6).cover?(-5))`, "false\n"},
		{"cover_int_out_high", `p((0..6).cover?(10))`, "false\n"},
		{"cover_float", `p((-1...1).cover?(10.5))`, "false\n"},
		{"cover_excl_end", `p((0.5...2.4).cover?(2.4))`, "false\n"},
		{"cover_incomparable", `p((1..10).cover?(nil))`, "false\n"},
		{"cover_endless", `p((1..).cover?(2.4))`, "true\n"},
		{"cover_beginless", `p((..10).cover?(2.4))`, "true\n"},
		{"cover_beginless_out", `p((...10.5).cover?(12.4))`, "false\n"},
		// cover? with a Range argument (range-in-range containment).
		{"cover_range_in", `p((0..10).cover?(3..7))`, "true\n"},
		{"cover_range_out", `p((0..10).cover?(3..15))`, "false\n"},
		{"cover_range_low", `p((0..10).cover?(-2..7))`, "false\n"},
		{"cover_range_str", `p(('c'..'i').cover?('d'..'f'))`, "true\n"},
		{"cover_range_excl_ok", `p((0..10).cover?(0...11))`, "true\n"},
		{"cover_range_excl_no", `p((0...10).cover?(0..10))`, "false\n"},
		{"cover_range_endless_self", `p((0..).cover?(4..6))`, "true\n"},
		{"cover_range_nilnil", `p((nil..nil).cover?(4..6))`, "true\n"},
		// Generic Comparable+succ end, self inclusive vs other exclusive: covered
		// when self.end.succ reaches other.end.
		{"cover_range_succ_end", `
class E
  include Comparable
  def initialize(n); @n=n; end
  attr_reader :n
  def <=>(o); @n <=> o.n; end
  def succ; E.new(@n+1); end
end
p((E.new(0)..E.new(10)).cover?(E.new(4)...E.new(11)))`, "true\n"},

		// === uses cover? logic (pure #<=>), even for a custom Comparable.
		{"eqq_int", `p((1..10) === 5)`, "true\n"},
		{"eqq_nilnil", `p((nil..nil) === "x")`, "true\n"},
		{"eqq_custom", `
class W
  include Comparable
  def initialize(n); @n=n; end
  attr_reader :n
  def <=>(o); return nil unless o.is_a?(W); @n <=> o.n; end
end
p((W.new(0)..W.new(10)) === W.new(2))`, "true\n"},

		// include? on a Numeric subclass endpoint uses cover? logic.
		{"include_numeric_subclass", `
class Num < Numeric
  def initialize(v); @v=v; end
  attr_reader :v
  def <=>(o); return nil unless o.is_a?(Num); @v <=> o.v; end
end
p((Num.new(0)..Num.new(6)).include?(Num.new(5)))`, "true\n"},
		// include? on String endpoints is a discrete #succ membership test.
		{"include_str_succ", `p(('a'..'c').include?('b'))`, "true\n"},
		{"include_str_not_succ", `p(('a'..'c').include?('bc'))`, "false\n"},
		{"include_str_multi", `p(('a'..'ab').include?('aa'))`, "true\n"},
		{"include_str_multi_out", `p(('a'..'ab').include?('ac'))`, "false\n"},
		{"include_str_excl_end", `p(('a'...'aa').include?('aa'))`, "false\n"},
		// include? coerces its argument to String via #to_str.
		{"include_to_str", `
o = Object.new
def o.to_str; 'b'; end
p(('a'..'aa').include?(o))`, "true\n"},
		{"include_no_to_str", `p(('a'..'aa').include?(nil))`, "false\n"},
		// include? falls back to Enumerable#include? (succ + ==) for custom ranges.
		{"include_custom_succ", `
class S
  def initialize(v); @v=v; end
  attr_reader :v
  def <=>(o); return nil unless o.is_a?(S); @v <=> o.v; end
  def succ; S.new(@v+1); end
  def ==(o); o.is_a?(S) && @v == o.v; end
end
p((S.new(1)..S.new(4)).include?(S.new(2)))`, "true\n"},
		{"include_custom_out", `
class S2
  def initialize(v); @v=v; end
  attr_reader :v
  def <=>(o); return nil unless o.is_a?(S2); @v <=> o.v; end
  def succ; S2.new(@v+1); end
  def ==(o); o.is_a?(S2) && @v == o.v; end
end
p((S2.new(1)..S2.new(4)).include?(S2.new(5)))`, "false\n"},
		// (nil..nil).include? is true for a linear value, else a TypeError (below).
		{"include_nilnil_numeric", `p((nil..nil).include?(1))`, "true\n"},
		// member? is a true alias of include?.
		{"member_alias", `p(Range.instance_method(:member?) == Range.instance_method(:include?))`, "true\n"},
		{"member_works", `p(('a'..'c').member?('b'))`, "true\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestRangeFirstMinMax covers #first, #min and #max, including the n-argument
// forms (which count up rather than materialise, so a Bignum-bounded or endless
// integer range works) and the endpoint edge cases.
func TestRangeFirstMinMax(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"first_bare", `p((3..7).first)`, "3\n"},
		{"first_n", `p((3..7).first(2))`, "[3, 4]\n"},
		{"first_n_over", `p((0..2).first(4))`, "[0, 1, 2]\n"},
		{"first_n_zero", `p((0..2).first(0))`, "[]\n"},
		{"first_to_int", `
o = Object.new
def o.to_int; 2; end
p((3..7).first(o))`, "[3, 4]\n"},
		{"first_float_trunc", `p((2..9).first(2.8))`, "[2, 3]\n"},
		{"first_endless", `p((1..).first(3))`, "[1, 2, 3]\n"},
		{"first_str_endless", `p(('a'..).first(3))`, "[\"a\", \"b\", \"c\"]\n"},
		{"first_float_end", `p((1..3.5).first(10))`, "[1, 2, 3]\n"},

		{"min_bare", `p((1..10).min)`, "1\n"},
		{"min_empty", `p((7...7).min)`, "nil\n"},
		{"min_n", `p((1..10).min(2))`, "[1, 2]\n"},
		{"min_n_bignum", `p((0...2**64).min(2))`, "[0, 1]\n"},
		{"min_n_endless", `p((1..).min(2))`, "[1, 2]\n"},
		{"min_n_over", `p((1..3).min(10))`, "[1, 2, 3]\n"},
		{"min_n_str", `p(('f'..'l').min(2))`, "[\"f\", \"g\"]\n"},
		{"min_block", `p((1..3).min {|a,b| -3 })`, "3\n"},
		{"min_block_empty", `p((100..10).min {|a,b| a <=> b })`, "nil\n"},

		{"max_bare", `p((1..10).max)`, "10\n"},
		{"max_excl", `p((1...10).max)`, "9\n"},
		{"max_bignum", `p((0..2**64-1).max)`, "18446744073709551615\n"},
		{"max_n", `p((1..10).max(2))`, "[10, 9]\n"},
		{"max_n_over", `p((1..3).max(10))`, "[3, 2, 1]\n"},
		{"max_beginless", `p((..1).max)`, "1\n"},
		{"max_block", `p((1..3).max {|a,b| -(a <=> b) })`, "1\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestRangeEachToA covers #each (which stops asking for a successor once the
// inclusive end is reached) and #to_a (ASCII multi-character String ranges walk
// the full #succ sequence; an endless range is a RangeError).
func TestRangeEachToA(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"to_a_int", `p((-2..2).to_a)`, "[-2, -1, 0, 1, 2]\n"},
		{"to_a_str", `p(('A'..'E').to_a)`, "[\"A\", \"B\", \"C\", \"D\", \"E\"]\n"},
		{"to_a_str_multi", `p(('a'..'ab').to_a.length)`, "28\n"},
		{"to_a_str_excl", `p(('a'...'c').to_a)`, "[\"a\", \"b\"]\n"},
		{"to_a_backward", `p(('D'..'A').to_a)`, "[]\n"},
		{"each_int", `(1..3).each {|i| print i}; puts`, "123\n"},
		{"each_returns_self", `r = (1..3); p(r.each {}.equal?(r))`, "true\n"},
		// #each stops at the inclusive end without calling #succ on it (a Time-like
		// object whose successor is a fresh object of the same value).
		{"each_succ_stop", `
class C
  include Comparable
  def initialize(n); @n=n; end
  attr_reader :n
  def <=>(o); @n <=> o.n; end
  def succ; C.new(@n+1); end
end
a = []
(C.new(1)..C.new(3)).each {|x| a << x.n }
p(a)`, "[1, 2, 3]\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestRangeStep covers #step: numeric and String stepping, the analytic
// Enumerator size (which must agree with the walk exactly, including a term that
// lands just inside or outside an exclusive boundary), and the zero-step edge.
func TestRangeStep(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"step_int", `(-5..5).step(2) {|x| print x, " "}; puts`, "-5 -3 -1 1 3 5 \n"},
		{"step_float", `(-1.0..1.0).step(0.5) {|x| print x, " "}; puts`, "-1.0 -0.5 0.0 0.5 1.0 \n"},
		{"step_str", `('A'..'G').step(2) {|x| print x}; puts`, "ACEG\n"},
		{"step_returns_self", `r = (1..2); p(r.step {}.equal?(r))`, "true\n"},

		{"size_incl", `p((-5..5).step(2).size)`, "6\n"},
		{"size_excl_boundary", `p((-5...5).step(2).size)`, "5\n"},
		{"size_excl_float", `p((-1.0...1.0).step(0.5).size)`, "4\n"},
		{"size_default_step", `p((-2...2).step.size)`, "4\n"},
		{"size_near_upper", `p((1.0...55.6).step(18.2).size)`, "4\n"},
		{"size_frac", `p((1...10).step(2).size)`, "5\n"},
		{"size_str_nil", `p(('A'..'E').step(2).size)`, "nil\n"},
		{"size_endless_inf", `p((1..).step(2).size)`, "Infinity\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestRangeErrors covers the raising paths: arity, TypeError for un-iterable
// ranges, RangeError for endless/beginless materialisation, and ArgumentError for
// zero-step and beginless #step.
func TestRangeErrors(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"cover_no_arg", `(1..2).cover?`, "ArgumentError"},
		{"cover_two_args", `(1..2).cover?(1, 2)`, "ArgumentError"},
		{"include_no_arg", `(1..2).include?`, "ArgumentError"},
		{"eqq_two_args", `(1..2).send(:===, 1, 2)`, "ArgumentError"},

		{"include_str_beginless", `(..'aa').include?('a')`, "TypeError"},
		{"include_str_endless", `('aa'..).include?('a')`, "TypeError"},
		{"include_nilnil_string", `(nil..nil).include?('a')`, "TypeError"},
		{"include_custom_endless", `
class SE
  def initialize(v); @v=v; end
  attr_reader :v
  def <=>(o); return nil unless o.is_a?(SE); @v <=> o.v; end
  def succ; SE.new(@v+1); end
end
(SE.new(0)..).include?(SE.new(5))`, "TypeError"},

		{"to_a_endless", `eval("(1..)").to_a`, "RangeError"},
		{"to_a_beginless", `(..1).to_a`, "TypeError"},
		{"first_bare_beginless", `(..1).first`, "RangeError"},
		{"first_negative", `(0..2).first(-1)`, "ArgumentError"},
		{"first_bad_arg", `(2..3).first("1")`, "TypeError"},

		{"min_beginless", `(..1).min`, "RangeError"},
		{"min_n_beginless", `(..1).min(2)`, "RangeError"},
		{"min_n_float_endless", `(1.0..).min(2)`, "TypeError"},
		{"min_block_endless", `(1..).min {|a, b| a }`, "RangeError"},
		{"min_n_negative", `(0..2).min(-1)`, "ArgumentError"},

		{"max_endless", `(1..).max`, "RangeError"},
		{"max_n_endless", `(1..).max(2)`, "RangeError"},
		{"max_n_beginless", `(..1.0).max(2)`, "RangeError"},
		{"max_excl_beginless_int", `(...1).max`, "TypeError"},
		{"max_excl_float", `(1.5...3.5).max`, "TypeError"},
		{"max_block_beginless", `(..1).max {|a, b| a }`, "RangeError"},

		{"step_zero_numeric", `(-1..1).step(0)`, "ArgumentError"},
		{"step_zero_float", `(-1..1).step(0.0) {}`, "ArgumentError"},
		{"step_beginless_iter", `(..10).step(1) { break }`, "ArgumentError"},
		{"step_beginless_nonnum", `(..10).step("a")`, "ArgumentError"},
		{"step_nilnil", `Range.new(nil, nil).step(1)`, "ArgumentError"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := runErr(t, tc.src)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("src=%q: got err=%v, want containing %q", tc.src, err, tc.want)
			}
		})
	}
}

// TestRangeStepZeroNonNumeric covers the one non-error zero-step path: a bounded
// non-numeric range with a zero step yields nothing rather than raising.
func TestRangeStepZeroNonNumeric(t *testing.T) {
	src := `
t = Time.at(0)
a = []
(t..(t + 1)).step(0) {|x| a << x }
p(a)`
	if got := eval(t, src); got != "[]\n" {
		t.Errorf("got=%q want=%q", got, "[]\n")
	}
}

// TestRangeMinMaxBlockCmp covers blockCmpSign's reduction of a non-Integer
// comparison result to a sign via #> / #<, so a custom comparison object drives
// #min / #max.
func TestRangeMinMaxBlockCmp(t *testing.T) {
	// The block returns a value whose #<=>-less sign is decided by #> and #< — here
	// a plain wrapper that forwards to its integer, exercising the non-Integer path.
	src := `
class Cmp
  def initialize(n); @n=n; end
  def >(o); @n > o; end
  def <(o); @n < o; end
end
p((1..4).min {|a, b| Cmp.new(a <=> b) })`
	if got := eval(t, src); got != "1\n" {
		t.Errorf("got=%q want=%q", got, "1\n")
	}
}

// TestRangeCoverageEdges exercises branches the primary tests reach only through
// ASCII / Integer inputs: blockCmpSign's Integer-zero and #>/#< result signs, the
// non-ASCII String #succ walk, a custom-#succ element overshooting the end, and
// the size Enumerator's nil paths for non-numeric step/limit. Values checked
// against MRI where the range is well-formed; the malformed ranges (which MRI
// rejects at construction but rbgo tolerates) assert rbgo's own handling.
func TestRangeCoverageEdges(t *testing.T) {
	cmp := `
class Cmp
  def initialize(n); @n = n; end
  def >(o); @n > o; end
  def <(o); @n < o; end
end
`
	cases := []struct{ name, src, want string }{
		// blockCmpSign: Integer sign branches (<0, >0, ==0).
		{"blkcmp_int_zero", `p((1..3).min {|a, b| 0 })`, "1\n"},
		{"blkcmp_int_pos", `p((1..3).min {|a, b| 5 })`, "1\n"},
		{"blkcmp_int_max_pos", `p((1..3).max {|a, b| 5 })`, "3\n"},
		// blockCmpSign: non-Integer result decided by #< (negative) and #> (positive).
		{"blkcmp_obj_neg", cmp + `p((1..3).min {|a, b| Cmp.new(-1) })`, "3\n"},
		{"blkcmp_obj_pos", cmp + `p((1..4).max {|a, b| Cmp.new(a <=> b) })`, "4\n"},
		// blockCmpSign: a result that is neither #>0 nor #<0 is treated as equal (0).
		{"blkcmp_obj_zero", `
class Neu
  def >(o); false; end
  def <(o); false; end
end
p((1..3).min {|a, b| Neu.new })`, "1\n"},

		// strRangeElems non-ASCII #succ walk (and isASCIIStr's non-ASCII return).
		{"nonascii_walk", `p(("\u{3a3}".."\u{3a9}").to_a)`, "[\"Σ\", \"Τ\", \"Υ\", \"Φ\", \"Χ\", \"Ψ\", \"Ω\"]\n"},
		{"nonascii_backward", `p(("\u{3a9}".."\u{3a3}").to_a)`, "[]\n"},
		{"nonascii_len_overshoot", `p(("a\u{3a3}".."b").to_a)`, "[\"aΣ\"]\n"},

		// rangeElemsV #succ path: an element whose #succ overshoots the end.
		{"succ_overshoot", `
class Mul
  include Comparable
  def initialize(n); @n = n; end
  attr_reader :n
  def <=>(o); @n <=> o.n; end
  def succ; Mul.new(@n * 10); end
end
p((Mul.new(1)..Mul.new(50)).to_a.map(&:n))`, "[1, 10]\n"},

		// step Enumerator size: nil for a non-numeric step or a non-numeric limit.
		{"size_nonnumeric_step", `p((1..10).step(Object.new).size)`, "nil\n"},
		{"size_nonnumeric_end", `p((1.."z").step(2).size)`, "nil\n"},
		{"size_nonnumeric_int_step_limit", `p(1.step("z", 2).size)`, "nil\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestRangeCoverageErrors exercises the remaining raising branches: a #to_str that
// returns a non-String (in the String-range membership test) and an endless
// Symbol range (which cannot be materialised).
func TestRangeCoverageErrors(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"to_str_non_string", `
o = Object.new
def o.to_str; 1; end
('a'..'aa').include?(o)`, "TypeError"},
		{"symbol_endless", `(:a..).to_a`, "RangeError"},
		// A zero step with a block reaches the numeric walkers, which raise it (the
		// no-block enum path raises eagerly instead — see TestRangeErrors).
		{"step_zero_block_bounded", `(1..3).step(0) { |x| }`, "ArgumentError"},
		{"step_zero_block_endless_int", `(1..).step(0) { |x| break }`, "ArgumentError"},
		{"step_zero_block_endless_float", `(1.0..).step(0.0) { |x| break }`, "ArgumentError"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := runErr(t, tc.src)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("src=%q: got err=%v, want containing %q", tc.src, err, tc.want)
			}
		})
	}
}

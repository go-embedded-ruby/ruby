// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// TestSpaceshipNumericExact covers Integer/Float #<=> including the exact
// integer-vs-float ordering (no precision loss for values outside the double
// range) and the numeric coercion protocol, asserted against MRI Ruby 3.4.
func TestSpaceshipNumericExact(t *testing.T) {
	cases := []struct{ src, want string }{
		// Integer <=> Integer / Bignum <=> Bignum (exact).
		{`p(3 <=> 5)`, "-1\n"},
		{`p(5 <=> 5)`, "0\n"},
		{`p((2**70) <=> (2**71))`, "-1\n"},
		{`p((2**71) <=> (2**70))`, "1\n"},
		// Bignum <=> Float: exact, so a Bignum just past the double grid still
		// orders correctly against the rounded float value.
		{`p((2**64 + 1) <=> (2**64).to_f)`, "1\n"},
		{`p((2**64) <=> (2**64).to_f)`, "0\n"},
		{`p((2**64 - 1) <=> (2**64).to_f)`, "-1\n"},
		// Float <=> Bignum (self is the Float): sign is inverted.
		{`p((2**64).to_f <=> (2**64 + 1))`, "-1\n"},
		{`p((2**64).to_f <=> (2**64))`, "0\n"},
		// Small Integer <=> Float and Float <=> small Integer.
		{`p(3 <=> 3.5)`, "-1\n"},
		{`p(3.5 <=> 3)`, "1\n"},
		{`p(954 <=> 954.0)`, "0\n"},
		// Infinities.
		{`p((2**64) <=> Float::INFINITY)`, "-1\n"},
		{`p((2**64) <=> (-Float::INFINITY))`, "1\n"},
		{`p((2**64).to_f <=> Float::INFINITY)`, "-1\n"},
		// NaN on either side is incomparable -> nil.
		{`p(3 <=> Float::NAN)`, "nil\n"},
		{`p((2**64) <=> Float::NAN)`, "nil\n"},
		{`p(Float::NAN <=> (2**64))`, "nil\n"},
		// Non-numeric with no #coerce -> nil (no raise).
		{`p(3 <=> "x")`, "nil\n"},
		{`p(3 <=> Object.new)`, "nil\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestIntFloatOrdering covers exact Integer/Float ordering via < <= > >= (the
// intFloatCompare path): large integers order correctly against a rounded float,
// the float-on-the-left sign inversion works, and a NaN operand makes every
// ordering comparison false.
func TestIntFloatOrdering(t *testing.T) {
	cases := []struct{ src, want string }{
		// A Bignum just past the double grid: (2**64+32).to_f rounds down to 2**64,
		// so the integer is strictly greater than its own float.
		{`p((2**64 + 32) <= (2**64 + 32).to_f)`, "false\n"},
		{`p((2**64 + 32) >  (2**64 + 32).to_f)`, "true\n"},
		{`p((2**64) < (2**64).to_f)`, "false\n"},
		{`p((2**64) >= (2**64).to_f)`, "true\n"},
		// Float on the left (sign inverted internally).
		{`p((2**64).to_f < (2**64 + 1))`, "true\n"},
		{`p((2**64).to_f >= (2**64 + 1))`, "false\n"},
		// Small integer vs float still exact.
		{`p(5 < 4.999)`, "false\n"},
		{`p(5 > 4.999)`, "true\n"},
		{`p(5 <= 5.0)`, "true\n"},
		{`p(5 >= 5.0)`, "true\n"},
		// NaN: every ordering comparison is false.
		{`p(5 < Float::NAN)`, "false\n"},
		{`p(5 > Float::NAN)`, "false\n"},
		{`p((2**64) <= Float::NAN)`, "false\n"},
		{`p(Float::NAN < (2**64))`, "false\n"},
		// Infinities.
		{`p((2**64) < Float::INFINITY)`, "true\n"},
		{`p((2**64) > -Float::INFINITY)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestSpaceshipNumericCoerce covers the #<=> coercion protocol: a non-numeric
// argument that answers #coerce is coerced and <=> re-dispatched on the pair; a
// non-Array result yields nil; an exception raised inside #coerce propagates.
func TestSpaceshipNumericCoerce(t *testing.T) {
	cases := []struct{ src, want string }{
		{`class C1; def coerce(o); [o, 10]; end; end; p(3 <=> C1.new)`, "-1\n"},
		{`class C2; def coerce(o); [o, 3]; end; end; p(3 <=> C2.new)`, "0\n"},
		{`class C3; def coerce(o); [o, 1]; end; end; p(3 <=> C3.new)`, "1\n"},
		// #coerce returning something other than a 2-element Array -> nil.
		{`class C4; def coerce(o); nil; end; end; p(3 <=> C4.new)`, "nil\n"},
		{`class C5; def coerce(o); [1]; end; end; p(3 <=> C5.new)`, "nil\n"},
		// An exception inside #coerce is not rescued.
		{`class C6; def coerce(o); raise "boom"; end; end
begin; 3 <=> C6.new; rescue => e; puts e.message; end`, "boom\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestNumericRelopCoerce covers the < <= > >= coercion protocol on built-in
// numbers: a non-numeric operand that answers #coerce is coerced and the
// operator re-dispatched; a missing/invalid #coerce raises ArgumentError; an
// exception inside #coerce propagates unrescued.
func TestNumericRelopCoerce(t *testing.T) {
	coercible := `class Num
  def initialize(v); @v = v; end
  def coerce(o); [Num.new(o), self]; end
  def <(o);  to_i <  o.to_i; end
  def <=(o); to_i <= o.to_i; end
  def >(o);  to_i >  o.to_i; end
  def >=(o); to_i >= o.to_i; end
  def to_i;  @v.to_i; end
end
`
	cases := []struct{ src, want string }{
		{coercible + `p(2 < Num.new(5))`, "true\n"},
		{coercible + `p(2 < Num.new(1))`, "false\n"},
		{coercible + `p(9 > Num.new(5))`, "true\n"},
		{coercible + `p(2 <= Num.new(2))`, "true\n"},
		{coercible + `p(2 >= Num.new(5))`, "false\n"},
		// Bignum receiver dispatches after coercion too.
		{coercible + `p((2**64) < Num.new(2**65))`, "true\n"},
		// Missing #coerce -> ArgumentError.
		{`begin; 5 < "4"; rescue ArgumentError; puts "AE"; end`, "AE\n"},
		{`begin; 5 < Object.new; rescue ArgumentError; puts "AE"; end`, "AE\n"},
		// #coerce returning a non-Array -> ArgumentError.
		{`class Bad; def coerce(o); nil; end; end
begin; 5 < Bad.new; rescue ArgumentError; puts "AE"; end`, "AE\n"},
		// #coerce raising propagates (not rescued as ArgumentError).
		{`class Boom; def coerce(o); raise "x"; end; end
begin; 1 < Boom.new; rescue RuntimeError => e; puts e.message; end`, "x\n"},
		// cmperrOperand rendering: Symbol / nil / true are inspected in the message,
		// other objects are named by class.
		{`begin; 1 < :sym; rescue ArgumentError => e; puts e.message; end`,
			"comparison of Integer with :sym failed\n"},
		{`begin; 1 < nil; rescue ArgumentError => e; puts e.message; end`,
			"comparison of Integer with nil failed\n"},
		{`begin; 1 < true; rescue ArgumentError => e; puts e.message; end`,
			"comparison of Integer with true failed\n"},
		{`begin; 1 < "s"; rescue ArgumentError => e; puts e.message; end`,
			"comparison of Integer with String failed\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestFloatConstants covers the IEEE-754 double-precision limits exposed as
// Float:: constants, asserted against MRI (core/float/constants_spec).
func TestFloatConstants(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p Float::MAX`, "1.7976931348623157e+308\n"},
		{`p Float::MIN`, "2.2250738585072014e-308\n"},
		{`p Float::EPSILON`, "2.220446049250313e-16\n"},
		{`p(Float::MAX == (1 + (1 - (2 ** -52))) * (2.0 ** 1023))`, "true\n"},
		{`p(Float::MIN == 2.0 ** -1022)`, "true\n"},
		{`p(Float::EPSILON == 2.0 ** -52)`, "true\n"},
		{`p Float::DIG`, "15\n"},
		{`p Float::MANT_DIG`, "53\n"},
		{`p Float::MAX_EXP`, "1024\n"},
		{`p Float::MIN_EXP`, "-1021\n"},
		{`p Float::MAX_10_EXP`, "308\n"},
		{`p Float::MIN_10_EXP`, "-307\n"},
		{`p Float::RADIX`, "2\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

package vm_test

import "testing"

// TestNumericHierarchy covers the Numeric class sitting between the numeric
// types and Object (with Comparable mixed into Numeric) and Class#superclass,
// asserted against MRI Ruby 4.0.5.
func TestNumericHierarchy(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p Integer.ancestors`, "[Integer, Numeric, Comparable, Object, Kernel, BasicObject]\n"},
		{`p Float.ancestors`, "[Float, Numeric, Comparable, Object, Kernel, BasicObject]\n"},
		{`p Rational.ancestors`, "[Rational, Numeric, Comparable, Object, Kernel, BasicObject]\n"},
		{`p Numeric.ancestors`, "[Numeric, Comparable, Object, Kernel, BasicObject]\n"},
		{`p [1.is_a?(Numeric), 1.0.is_a?(Numeric), 1.is_a?(Comparable)]`, "[true, true, true]\n"},
		// Comparable still derives from <=> for the numeric types.
		{`p [1 < 2, 2.0 >= 1.0, 3.between?(1, 5), 5.clamp(1, 3)]`, "[true, true, true, 3]\n"},
		// Class#superclass walks the new chain.
		{`p Integer.superclass`, "Numeric\n"},
		{`p Numeric.superclass`, "Object\n"},
		{`p Object.superclass`, "BasicObject\n"},
		{`p BasicObject.superclass`, "nil\n"},
		{`class A; end; class B < A; end; p B.superclass`, "A\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestNumericDefaultMethods covers the Numeric fallbacks — zero?, real, floor,
// ceil, round and truncate — that a subclass defining only #to_f (and #<=>)
// inherits, asserted against MRI Ruby 4.0.6.
func TestNumericDefaultMethods(t *testing.T) {
	pre := "class FNum < Numeric; def initialize(v); @v = v; end; def v; @v; end; " +
		"def <=>(o); @v <=> (o.is_a?(FNum) ? o.v : o); end; def coerce(o); [FNum.new(o), self]; end; " +
		"def to_f; @v.to_f; end; end; "
	cases := []struct{ src, want string }{
		{pre + `p [FNum.new(2.7).truncate, FNum.new(2.7).floor, FNum.new(2.7).ceil, FNum.new(2.7).round]`, "[2, 2, 3, 3]\n"},
		{pre + `p [FNum.new(-2.7).truncate, FNum.new(-2.7).floor, FNum.new(-2.7).ceil, FNum.new(-2.7).round]`, "[-2, -3, -2, -3]\n"},
		{pre + `p FNum.new(2.75).round(1)`, "2.8\n"},
		{pre + `p [FNum.new(0).zero?, FNum.new(5).zero?]`, "[true, false]\n"},
		{pre + `p FNum.new(5).real.v`, "5\n"},
		{pre + `p [FNum.new(0).nonzero?, FNum.new(5).nonzero?.v]`, "[nil, 5]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

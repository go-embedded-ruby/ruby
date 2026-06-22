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

package vm_test

import "testing"

// TestEql covers Object#eql? — equality without numeric coercion, recursive for
// Array/Hash, identity for plain objects, and value-based for built-in subclass
// instances. Asserted against MRI Ruby 4.0.5.
func TestEql(t *testing.T) {
	cases := []struct{ src, want string }{
		// Numerics: never eql? across Integer/Float, even when == would hold.
		{`p [1.eql?(1), 1.eql?(1.0), 1.eql?(2)]`, "[true, false, false]\n"},
		{`p [1.0.eql?(1.0), 1.0.eql?(1), 1.0.eql?(2.0)]`, "[true, false, false]\n"},
		{`p [(10**30).eql?(10**30), (10**30).eql?(1), (10**30).eql?(10**31)]`, "[true, false, false]\n"},
		// String / Symbol.
		{`p ["hi".eql?("hi"), "hi".eql?("ho"), "hi".eql?(:hi)]`, "[true, false, false]\n"},
		{`p [:a.eql?(:a), :a.eql?("a"), :a.eql?(:b)]`, "[true, false, false]\n"},
		// Array: recursive eql? (so [1] is not eql? [1.0]); length and type guards.
		{`p [[1, 2].eql?([1, 2]), [1].eql?([1, 2]), [1].eql?([2]), [1].eql?(1), [1].eql?([1.0])]`, "[true, false, false, false, false]\n"},
		// Hash: same size, keys present, values eql?.
		{`p [{a: 1}.eql?({a: 1}), {a: 1}.eql?({a: 1, b: 2}), {a: 1}.eql?({a: 2}), {a: 1}.eql?({b: 1}), {a: 1}.eql?(1)]`, "[true, false, false, false, false]\n"},
		// Singletons / identity.
		{`p [nil.eql?(nil), nil.eql?(1), true.eql?(true), false.eql?(false)]`, "[true, false, true, true]\n"},
		{`class K; end; k = K.new; p [k.eql?(k), k.eql?(K.new)]`, "[true, false]\n"},
		// Built-in subclass instances compare as the wrapped value, on either side.
		{`class S < String; end; p [S.new("x").eql?("x"), "x".eql?(S.new("x"))]`, "[true, true]\n"},

		// The set operations uniq / & / | / - compare with eql?, so 1 and 1.0 are
		// distinct members...
		{`p [[1, 1.0, 1].uniq, [1, 1.0] & [1.0], [1] | [1.0], [1, 1.0] - [1]]`, "[[1, 1.0], [1.0], [1, 1.0], [1.0]]\n"},
		{`a = [1, 1.0, 1]; a.uniq!; p a`, "[1, 1.0]\n"},
		// ...while membership/search (include?/index) still use ==.
		{`p [[1].include?(1.0), [1, 2, 3].index(2.0)]`, "[true, 1]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

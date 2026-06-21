package vm_test

import (
	"strings"
	"testing"
)

// TestBlocklessIteratorsReturnEnumerator verifies that every iterator method,
// when called without a block, returns an Enumerator (MRI 4.x semantics) rather
// than raising — chaining .to_a (or a real block) recovers the elements.
func TestBlocklessIteratorsReturnEnumerator(t *testing.T) {
	cases := []struct{ src, want string }{
		// Array.
		{`p [1, 2, 3].map.to_a`, "[1, 2, 3]\n"},
		{`p [1, 2, 3].map!.to_a`, "[1, 2, 3]\n"},
		{`p [1, 2, 3].collect.to_a`, "[1, 2, 3]\n"},
		{`p [1, 2, 3].select.to_a`, "[1, 2, 3]\n"},
		{`p [1, 2, 3].select!.to_a`, "[1, 2, 3]\n"},
		{`p [1, 2, 3].filter.to_a`, "[1, 2, 3]\n"},
		{`p [1, 2, 3].filter!.to_a`, "[1, 2, 3]\n"},
		{`p [1, 2, 3].reject.to_a`, "[1, 2, 3]\n"},
		{`p [1, 2, 3].reject!.to_a`, "[1, 2, 3]\n"},
		// take_while/drop_while are predicate-driven, so a degenerate (blockless)
		// materialisation stops at the first element — matching MRI. With a real
		// predicate via with_index they behave fully (covered below).
		{`p [1, 2, 3].take_while.to_a`, "[1]\n"},
		{`p [1, 2, 3].drop_while.to_a`, "[1]\n"},
		{`p [1, 2, 3].take_while.with_index { |x, i| i < 2 }`, "[1, 2]\n"},
		{`p [1, 2, 3].drop_while.with_index { |x, i| i < 1 }`, "[2, 3]\n"},
		{`p [3, 1, 2].sort_by.to_a`, "[3, 1, 2]\n"},
		{`p [3, 1, 2].min_by.to_a`, "[3, 1, 2]\n"},
		{`p [3, 1, 2].max_by.to_a`, "[3, 1, 2]\n"},
		{`p [1, 2, 3].flat_map.to_a`, "[1, 2, 3]\n"},
		{`p [1, 2, 3].find.to_a`, "[1, 2, 3]\n"},
		{`p [1, 2, 3].detect.to_a`, "[1, 2, 3]\n"},
		{`p [1, 2, 3].partition.to_a`, "[1, 2, 3]\n"},
		{`p [1, 2, 3].group_by.to_a`, "[1, 2, 3]\n"},
		{`p [1, 2, 3].filter_map.to_a`, "[1, 2, 3]\n"},
		{`p [1, 2, 3, 4].each_slice(2).to_a`, "[[1, 2], [3, 4]]\n"},
		{`p [1, 2, 3].each_cons(2).to_a`, "[[1, 2], [2, 3]]\n"},
		{`p [1, 2, 3].each_with_object([]).to_a.length`, "3\n"},
		// Hash.
		{`p({a: 1, b: 2}.map.to_a)`, "[[:a, 1], [:b, 2]]\n"},
		{`p({a: 1}.select.to_a)`, "[[:a, 1]]\n"},
		{`p({a: 1}.reject.to_a)`, "[[:a, 1]]\n"},
		{`p({a: 1}.transform_values.to_a)`, "[1]\n"},
		{`p({a: 1}.transform_keys.to_a)`, "[:a]\n"},
		// Range.
		{`p (1..3).map.to_a`, "[1, 2, 3]\n"},
		// Integer.
		{`p 3.times.to_a`, "[0, 1, 2]\n"},
		{`p 1.upto(3).to_a`, "[1, 2, 3]\n"},
		{`p 3.downto(1).to_a`, "[3, 2, 1]\n"},
		// String.
		{`p "ab".each_char.to_a`, "[\"a\", \"b\"]\n"},
		{`p "ab".each_byte.to_a`, "[97, 98]\n"},
		{`p "a\nb".each_line.to_a`, "[\"a\\n\", \"b\"]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	// Methods with a required argument raise ArgumentError when it is missing,
	// before the block check — matching MRI.
	for _, src := range []string{
		`[1, 2, 3].each_slice`,
		`[1, 2, 3].each_cons`,
		`[1, 2, 3].each_with_object`,
		`1.upto`,
		`1.downto`,
	} {
		if err := runErr(t, src); err == nil || !strings.Contains(err.Error(), "ArgumentError") {
			t.Errorf("src=%q got=%v want ArgumentError", src, err)
		}
	}
}

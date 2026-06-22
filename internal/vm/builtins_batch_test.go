package vm_test

import (
	"strings"
	"testing"
)

// TestBuiltinsBatch covers Array.new (all forms), Array#values_at,
// String#casecmp/#casecmp? and Enumerable#to_h. Asserted against MRI Ruby 4.0.5.
func TestBuiltinsBatch(t *testing.T) {
	cases := []struct{ src, want string }{
		// Array.new: empty / size / size+fill / block / array-copy.
		{`p [Array.new, Array.new(3), Array.new(3, 0), Array.new(3) { |i| i * i }]`, "[[], [nil, nil, nil], [0, 0, 0], [0, 1, 4]]\n"},
		{`p Array.new([1, 2, 3])`, "[1, 2, 3]\n"},
		// Array#values_at: positive, negative and out-of-range indices.
		{`p [1, 2, 3, 4, 5].values_at(0, 2, 4)`, "[1, 3, 5]\n"},
		{`p [1, 2, 3].values_at(-1, 5)`, "[3, nil]\n"},
		// String#casecmp / #casecmp?.
		{`p ["Hello".casecmp("hello"), "Hello".casecmp("world"), "a".casecmp("B")]`, "[0, -1, -1]\n"},
		{`p ["Hello".casecmp?("HELLO"), "a".casecmp?("b"), "x".casecmp?(5)]`, "[true, false, nil]\n"},
		{`p "a".casecmp(5)`, "nil\n"}, // non-String operand -> nil, like <=>
		// Enumerable#to_h: block form, pair form, over a multi-value enumerator.
		{`p (1..3).to_h { |x| [x, x * x] }`, "{1 => 1, 2 => 4, 3 => 9}\n"},
		{`p [[1, 2], [3, 4]].each.to_h`, "{1 => 2, 3 => 4}\n"},
		{`p (1..3).each_with_index.to_h`, "{1 => 0, 2 => 1, 3 => 2}\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	errs := []struct{ src, want string }{
		{`Array.new(-1)`, "negative array size"},
		{`[1, 2, 3].each.to_h`, "wrong element type Integer (expected array)"},
		{`[[1, 2, 3]].each.to_h`, "wrong array length (expected 2, was 3)"},
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q err=%v, want substring %q", c.src, err, c.want)
		}
	}
}

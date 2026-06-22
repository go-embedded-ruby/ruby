package vm_test

import "testing"

// TestEnumerableExtraMethods covers Enumerable methods added in the prelude so
// every Enumerable (Range, Hash, Struct, Enumerator) gains them, not just Array:
// find_index, find_all, take_while, drop_while, each_slice, each_cons,
// chunk_while and slice_when. Asserted against MRI Ruby 4.0.5.
func TestEnumerableExtraMethods(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p (1..5).find_index { |x| x > 3 }`, "3\n"},
		{`p (1..5).find_index(3)`, "2\n"},
		{`p (1..5).find_index { |x| x > 99 }`, "nil\n"},
		{`p (1..5).find_all(&:even?)`, "[2, 4]\n"},
		{`p (1..5).take_while { |x| x < 3 }`, "[1, 2]\n"},
		{`p (1..5).drop_while { |x| x < 3 }`, "[3, 4, 5]\n"},
		{`p (1..5).each_slice(2).to_a`, "[[1, 2], [3, 4], [5]]\n"},
		{`r = []; (1..5).each_slice(2) { |s| r << s }; p r`, "[[1, 2], [3, 4], [5]]\n"},
		{`p (1..7).each_cons(3).to_a`, "[[1, 2, 3], [2, 3, 4], [3, 4, 5], [4, 5, 6], [5, 6, 7]]\n"},
		{`p (1..10).chunk_while { |a, b| b - a == 1 }.to_a`, "[[1, 2, 3, 4, 5, 6, 7, 8, 9, 10]]\n"},
		{`p [1, 2, 4, 5, 7].chunk_while { |a, b| b - a == 1 }.to_a`, "[[1, 2], [4, 5], [7]]\n"},
		{`p (1..5).slice_when { |a, b| b.even? }.to_a`, "[[1], [2, 3], [4, 5]]\n"},
		{`p [].chunk_while { |a, b| true }.to_a`, "[]\n"},
		// find_index / find_all are not native on Array, so the prelude serves it too.
		{`p [1, 2, 3].find_index(2)`, "1\n"},
		{`p [3, 1, 4, 1, 5].find_all { |x| x > 2 }`, "[3, 4, 5]\n"},
		// Works over a multi-value enumerator (element + index) via __each_packed.
		{`p [10, 20, 30].each_with_index.find_index { |x, i| i == 2 }`, "2\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

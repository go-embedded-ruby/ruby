package vm_test

import "testing"

// TestEnumerableMultiValueYield covers the fix for chaining an Enumerable method
// over an Enumerator that yields multiple values per step (the canonical case is
// each_with_index, which yields element + index). Enumerable now iterates through
// __each_packed, which packs a multi-value yield into an Array so a downstream
// multi-parameter block (`map { |x, i| }`) auto-splats it exactly as MRI does.
// Single-value sources and Hash (whose #each yields one [k, v] pair) are
// unaffected. Asserted against MRI Ruby 4.0.5.
func TestEnumerableMultiValueYield(t *testing.T) {
	cases := []struct{ src, want string }{
		// Multi-value enumerator (each_with_index) chained through Enumerable.
		{`p [1, 2, 3].each_with_index.map { |x, i| x * i }`, "[0, 2, 6]\n"},
		{`p [10, 20, 30].each_with_index.select { |x, i| i.even? }`, "[[10, 0], [30, 2]]\n"},
		{`p [1, 2, 3].each_with_index.reject { |x, i| i.even? }`, "[[2, 1]]\n"},
		{`p [1, 2, 3].each_with_index.find { |x, i| i == 1 }`, "[2, 1]\n"},
		{`p [1, 2, 3].each_with_index.flat_map { |x, i| [x, i] }`, "[1, 0, 2, 1, 3, 2]\n"},
		{`p [1, 2, 3].each_with_index.count { |x, i| i > 0 }`, "2\n"},
		{`p [5, 6, 7].each_with_index.to_a`, "[[5, 0], [6, 1], [7, 2]]\n"},
		{`p [1, 2, 3].each_with_index.partition { |x, i| i.even? }`, "[[[1, 0], [3, 2]], [[2, 1]]]\n"},
		{`p [1, 2, 3].each_with_index.each_with_object([]) { |(x, i), a| a << x * i }`, "[0, 2, 6]\n"},
		// Hash#each yields a [k, v] pair: still auto-splats to a 2-param block.
		{`p({a: 1, b: 2}.map { |k, v| [k, v * 2] })`, "[[:a, 2], [:b, 4]]\n"},
		{`p({a: 1, b: 2}.count { |k, v| v > 1 })`, "1\n"},
		{`p({a: 1, b: 2}.find { |k, v| v == 2 })`, "[:b, 2]\n"},
		{`p({a: 1, b: 2}.to_a)`, "[[:a, 1], [:b, 2]]\n"},
		{`p({a: 1, b: 2}.any? { |k, v| v > 1 })`, "true\n"},
		// Single-value sources (Enumerator over scalars, Range, Struct) unchanged.
		{`p [1, 2, 3].each.map { |x| x * 10 }`, "[10, 20, 30]\n"},
		{`p (1..5).map { |x| x * 2 }`, "[2, 4, 6, 8, 10]\n"},
		{`S = Struct.new(:a, :b); p S.new(1, 2).map { |x| x * 10 }`, "[10, 20]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

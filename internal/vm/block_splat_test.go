package vm_test

import "testing"

// TestBlockSplatParams covers top-level rest parameters in block signatures
// (|*rest|, |a, *rest|), end to end through the compiler and the splat-aware
// block-argument binding. Asserted against MRI Ruby 4.0.5.
func TestBlockSplatParams(t *testing.T) {
	cases := []struct{ src, want string }{
		// |*rest| collects all yielded values.
		{`r = []; [1, 2, 3].each { |*a| r << a }; p r`, "[[1], [2], [3]]\n"},
		{`p [1, 2, 3].map { |*a| a }`, "[[1], [2], [3]]\n"},
		// |head, *rest| splits.
		{`p [[1, 2], [3, 4]].map { |a, *b| [a, b] }`, "[[1, [2]], [3, [4]]]\n"},
		{`r = []; [[1, 2, 3]].each { |a, *b| r << [a, b] }; p r`, "[[1, [2, 3]]]\n"},
		// A single Array argument: |*a| does not spread it (block arity is -1).
		{`p [[1, 2]].map { |*a| a }`, "[[[1, 2]]]\n"},
		// Procs / lambdas with splat, including too-few-args padding.
		{`p proc { |*a| a }.call(1, 2, 3)`, "[1, 2, 3]\n"},
		{`p proc { |a, *b| [a, b] }.call`, "[nil, []]\n"}, // 0 args -> pad required
		{`p proc { |a, *b| [a, b] }.call(1)`, "[1, []]\n"},
		{`p lambda { |*a| a }.call(5)`, "[5]\n"},
		// Regressions: non-splat blocks and Hash auto-splat must be unchanged.
		{`p [1, 2, 3].map { |x| x * 2 }`, "[2, 4, 6]\n"},
		{`p({a: 1, b: 2}.map { |k, v| [k, v] })`, "[[:a, 1], [:b, 2]]\n"},
		{`p [1, 2, 3].each_with_object([]) { |x, acc| acc << x }`, "[1, 2, 3]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

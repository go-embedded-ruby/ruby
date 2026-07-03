package vm_test

import "testing"

// TestArraySelectNative covers the native Array#select fast path: the block form
// (kept/dropped elements, first-seen order), the empty result, and the no-block
// form that returns an Enumerator. #filter delegates to #select through the
// prelude, so it exercises the same native path.
func TestArraySelectNative(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"even", `p [1, 2, 3, 4, 5, 6].select { |x| x.even? }`, "[2, 4, 6]\n"},
		{"none", `p [1, 3, 5].select { |x| x.even? }`, "[]\n"},
		{"all", `p [2, 4].select { |x| x.even? }`, "[2, 4]\n"},
		{"filter_alias", `p [1, 2, 3].filter { |x| x > 1 }`, "[2, 3]\n"},
		{"no_block_enum", `p [1, 2, 3].select.class`, "Enumerator\n"},
		{"no_block_resume", `p [1, 2, 3, 4].select.each { |x| x.odd? }`, "[1, 3]\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestArrayReduceNative covers native Array#reduce / #inject across every
// argument form the prelude supported: bare block, block with init, the symbol
// form reduce(:op), the (init, sym) form, the two-argument form whose operator is
// a non-Symbol (deferred to send), the nil result of an empty fold, and the
// single-element short-circuit that returns the element without yielding.
func TestArrayReduceNative(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"block", `p [1, 2, 3, 4].reduce { |a, b| a + b }`, "10\n"},
		{"block_init", `p [1, 2, 3].reduce(10) { |a, b| a + b }`, "16\n"},
		{"sym", `p [1, 2, 3, 4].reduce(:+)`, "10\n"},
		{"init_sym", `p [1, 2, 3].reduce(100, :+)`, "106\n"},
		{"init_sym_mul", `p [1, 2, 3, 4].reduce(1, :*)`, "24\n"},
		{"nonsym_op_string", `p [1, 2, 3].reduce(0, "+")`, "6\n"},
		{"empty_nil", `p [].reduce { |a, b| a + b }`, "nil\n"},
		{"empty_init", `p [].reduce(7) { |a, b| a + b }`, "7\n"},
		{"single", `p [42].reduce { |a, b| a + b }`, "42\n"},
		{"inject_alias", `p [1, 2, 3].inject(:+)`, "6\n"},
		{"inject_block", `p [2, 3, 4].inject { |a, b| a * b }`, "24\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestArrayReduceNoBlock covers the "no block given" yield error: a bare reduce
// with two or more elements reaches a yield step and raises LocalJumpError,
// exactly as the interpreted Enumerable#reduce did.
func TestArrayReduceNoBlock(t *testing.T) {
	class, msg := evalErr(t, `[1, 2, 3].reduce`)
	if class != "LocalJumpError" || msg != "no block given (yield)" {
		t.Fatalf("got %s %q", class, msg)
	}
}

// TestArrayContainerPipeline covers the full map/select/reduce pipeline used by
// the array benchmark, confirming the native kernels compose to the same result.
func TestArrayContainerPipeline(t *testing.T) {
	got := eval(t, `p (1..10).to_a.map { |x| x * 2 }.select { |x| x % 3 == 0 }.reduce(0) { |a, b| a + b }`)
	if got != "36\n" { // 6 + 12 + 18 = 36
		t.Fatalf("pipeline got %q", got)
	}
}
